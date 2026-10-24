// Package session 记录“在线会话”，供 online（在线用户/强退）、profile（登录设备）、message 共用。
//
// core 的登录流程没有给模块留钩子，所以会话是“被动登记”的：已登录用户访问挂了 Middleware 的接口时，
// 按 用户+IP+UA 生成稳定的会话 ID 写进 core/cache。强退时把该会话最近一次见到的 access / refresh
// 令牌吊销（写入 core/auth 使用的吊销 key）。
// 存储布局（按租户）：每个会话一个 key（nex:t<租户>:online:s:<sid>，JSON，带 TTL）+ 一个有序集合索引
// （nex:t<租户>:online:idx，member=sid，score=最近活跃的毫秒时间戳）。登记 / 强退只对单个会话 key 和集合做原子操作，
// 不再读-改-写整份索引，多实例并发不会互相覆盖。同一会话（同用户 + IP + UA）被两个实例同时登记时后写者胜，只影响展示字段。
// 缓存不支持有序集合（外部实现的 Cache）时退回进程内的内存存储，行为同单实例。
//
// 残余窗口（cache 现有能力做不到完全消除，刻意不引入更复杂的方案）：
//   - 同一会话 key 的 Touch 是 Get 后 Set（读-改-写），两个实例同时登记同一会话时后写者胜，
//     只会丢 LastActiveAt / 令牌 jti 这类展示与强退用字段，下一次 Touch（最长 30 秒节流）即纠正；
//   - all() 发现会话 key 已过期后才 ZRem 索引项，若恰好有并发 Touch 在两步之间重建了该会话，
//     索引项会被误删，列表里短暂缺失它，直到该会话的下一次 Touch 重新 ZAdd；
//   - 强退（Kick）先吊销令牌再删会话；并发 Touch 可能在删除后立即重建展示记录，但被吊销的令牌已无法通过鉴权。
//   - 缓存不支持有序集合时索引与会话都退回进程内存，多实例下彼此不可见。
package session

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

// Session 是一个在线会话。
type Session struct {
	ID           string    `json:"id"`
	UserID       idgen.ID  `json:"userId"`
	Username     string    `json:"username"`
	Nickname     string    `json:"nickname"`
	Device       string    `json:"device"`
	Browser      string    `json:"browser"`
	OS           string    `json:"os"`
	IP           string    `json:"ip"`
	LoginAt      time.Time `json:"loginAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
	AccessJTI    string    `json:"accessJti"`
	RefreshJTI   string    `json:"refreshJti"`
}

type touch struct {
	at  time.Time
	jti string
}

// Store 是会话存储。
type Store struct {
	cache   cache.Cache
	zs      cache.SortedSet
	ttl     time.Duration
	mu      sync.Mutex       // 只保护 touched
	touched map[string]touch // 进程内节流：同一会话 30 秒内不重复写缓存
}

var (
	shared *Store
	once   sync.Once
)

// Shared 返回进程内唯一的 Store；ttl 取 refresh token 有效期（超过它的会话必然已失效）。
func Shared(c cache.Cache, ttl time.Duration) *Store {
	once.Do(func() { shared = NewStore(c, ttl) })
	return shared
}

// NewStore 创建一个独立的 Store（测试用；业务代码用 Shared）。
func NewStore(c cache.Cache, ttl time.Duration) *Store {
	zs, ok := cache.As[cache.SortedSet](c)
	if !ok { // 缓存没有有序集合能力：退回进程内存储
		mem, err := cache.New(config.Cache{Driver: "memory"})
		if err != nil {
			panic(err) // memory 实现不会失败
		}
		c = mem
		zs, _ = cache.As[cache.SortedSet](mem)
	}
	return &Store{cache: c, zs: zs, ttl: ttl, touched: map[string]touch{}}
}

// SID 生成稳定的会话 ID（十进制字符串，token 刷新后不变）。
func SID(uid idgen.ID, ip, ua string) string {
	h := sha256.Sum256([]byte(uid.String() + "|" + ip + "|" + ua))
	return strconv.FormatUint(binary.BigEndian.Uint64(h[:8])>>1, 10)
}

func indexKey(ctx context.Context) string               { return cache.Key(ctx, "online", "idx") }
func sessionKey(ctx context.Context, sid string) string { return cache.Key(ctx, "online", "s", sid) }

func score(t time.Time) float64 { return float64(t.UnixMilli()) }

func (s *Store) cutoff() float64 { return score(time.Now().Add(-s.ttl)) }

func (s *Store) forget(sid string) {
	s.mu.Lock()
	delete(s.touched, sid)
	s.mu.Unlock()
}

func (s *Store) get(ctx context.Context, sid string) (Session, bool, error) {
	raw, err := s.cache.Get(ctx, sessionKey(ctx, sid))
	if errors.Is(err, cache.ErrNotFound) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	var ses Session
	if json.Unmarshal([]byte(raw), &ses) != nil {
		return Session{}, false, nil // 内容损坏就当不存在，会话只是展示用
	}
	return ses, true, nil
}

func (s *Store) known(sid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.touched[sid]
	return ok
}

// Touch 登记/刷新当前请求所属的会话。nick 只在首次见到该会话时调用（可能查库）。
func (s *Store) Touch(ctx context.Context, cl *auth.Claims, ip, ua string, nick func() string) error {
	sid := SID(cl.UserID, ip, ua)
	nickname := ""
	if !s.known(sid) {
		nickname = nick()
	}
	s.mu.Lock()
	t, seen := s.touched[sid]
	s.mu.Unlock()
	if seen && t.jti == cl.ID && time.Since(t.at) < 30*time.Second {
		return nil
	}
	cur, ok, err := s.get(ctx, sid)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if !ok {
		device, browser, os := ParseUA(ua)
		cur = Session{ID: sid, UserID: cl.UserID, Username: cl.Username, Nickname: nickname,
			Device: device, Browser: browser, OS: os, IP: ip, LoginAt: now}
	}
	if cur.Nickname == "" {
		cur.Nickname = nickname
	}
	cur.LastActiveAt, cur.AccessJTI, cur.RefreshJTI = now, cl.ID, cl.RID
	b, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	if err := s.cache.Set(ctx, sessionKey(ctx, sid), string(b), s.ttl); err != nil {
		return err
	}
	if err := s.zs.ZAdd(ctx, indexKey(ctx), sid, score(now), s.ttl); err != nil {
		return err
	}
	if !ok { // 新会话时顺手清掉超过 ttl 没活动的索引项
		_ = s.zs.ZRemRangeByScore(ctx, indexKey(ctx), s.cutoff())
	}
	s.mu.Lock()
	s.touched[sid] = touch{at: time.Now(), jti: cl.ID}
	s.mu.Unlock()
	return nil
}

// all 返回索引里仍然存在的会话；会话 key 已过期的索引项顺手删掉。
func (s *Store) all(ctx context.Context) ([]Session, error) {
	_ = s.zs.ZRemRangeByScore(ctx, indexKey(ctx), s.cutoff())
	ids, err := s.zs.ZRange(ctx, indexKey(ctx), s.cutoff())
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(ids))
	var stale []string
	for _, sid := range ids {
		ses, ok, err := s.get(ctx, sid)
		if err != nil {
			return nil, err
		}
		if !ok {
			stale = append(stale, sid)
			continue
		}
		out = append(out, ses)
	}
	if len(stale) > 0 {
		_ = s.zs.ZRem(ctx, indexKey(ctx), stale...)
		for _, sid := range stale {
			s.forget(sid)
		}
	}
	return out, nil
}

// List 返回当前租户的在线会话，最近活跃的在前；userID 非 0 时只返回该用户的。
func (s *Store) List(ctx context.Context, userID idgen.ID) ([]Session, error) {
	all, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, ses := range all {
		if time.Since(ses.LastActiveAt) > s.ttl || (userID != 0 && ses.UserID != userID) {
			continue
		}
		out = append(out, ses)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActiveAt.After(out[j].LastActiveAt) })
	return out, nil
}

// revoke 吊销会话最近一次见到的令牌（key 约定与 core/auth 的吊销 key 一致）。
func (s *Store) revoke(ctx context.Context, ses Session) error {
	for _, jti := range []string{ses.AccessJTI, ses.RefreshJTI} {
		if jti == "" {
			continue
		}
		if err := s.cache.Set(ctx, cache.GlobalKey("auth", "revoked", jti), "1", s.ttl); err != nil {
			return err
		}
	}
	return nil
}

// remove 删除一个会话（会话 key + 索引项）。
func (s *Store) remove(ctx context.Context, sid string) error {
	s.forget(sid)
	if err := s.cache.Del(ctx, sessionKey(ctx, sid)); err != nil {
		return err
	}
	return s.zs.ZRem(ctx, indexKey(ctx), sid)
}

// Kick 强制下线一个会话；会话不存在返回 (nil, false)。userID 非 0 时要求会话属于该用户。
func (s *Store) Kick(ctx context.Context, sid string, userID idgen.ID) (*Session, bool, error) {
	ses, ok, err := s.get(ctx, sid)
	if err != nil {
		return nil, false, err
	}
	if !ok || (userID != 0 && ses.UserID != userID) {
		return nil, false, nil
	}
	if err := s.revoke(ctx, ses); err != nil {
		return nil, true, err
	}
	return &ses, true, s.remove(ctx, sid)
}

// KickUser 下线某用户除 exceptSID 外的全部会话（改密码后使用）。
func (s *Store) KickUser(ctx context.Context, userID idgen.ID, exceptSID string) error {
	all, err := s.all(ctx)
	if err != nil {
		return err
	}
	for _, ses := range all {
		if ses.UserID != userID || ses.ID == exceptSID {
			continue
		}
		if err := s.revoke(ctx, ses); err != nil {
			return err
		}
		if err := s.remove(ctx, ses.ID); err != nil {
			return err
		}
	}
	return nil
}
