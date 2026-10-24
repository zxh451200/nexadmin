package oauthflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"nexadmin/core/cache"
	"nexadmin/pkg/oauth"
)

// 待绑定令牌：第三方身份在本地匹配不上账号时，把身份暂存 10 分钟并发一个随机令牌给前端；
// 用户输入已有账号的用户名密码后凭它完成绑定。令牌一次性，累计输错 5 次作废；身份本身从不回传给前端。

type bindRec struct {
	Identity oauth.Identity `json:"id"`
	Tries    int            `json:"n"` // 已弃用：尝试次数改存独立计数 key，保留字段以兼容旧数据
	Exp      int64          `json:"e"`
}

func bindKey(token string) string { return stateKey("bind:" + token) }

// NewBindToken 暂存身份并返回令牌。
func (f *Flow) NewBindToken(ctx context.Context, id *oauth.Identity) (string, error) {
	tok, err := oauth.RandomToken(24)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(bindRec{Identity: *id, Exp: time.Now().Add(BindTokenTTL).Unix()})
	if err := f.cache.Set(ctx, bindKey(tok), string(raw), BindTokenTTL); err != nil {
		return "", err
	}
	return tok, nil
}

// bindTriesKey 是令牌的尝试计数 key（独立于令牌本身，用原子 Incr 计数）。
func bindTriesKey(token string) string { return stateKey("bindtries:" + token) }

// LookupBindToken 读取令牌对应的身份并记一次尝试；超过 MaxBindTries 次即作废。
// 尝试次数用 cache.Incr 原子计数（首次创建时按令牌剩余有效期设 TTL），并发重放不会丢计数；
// 超限时用 Del 作废令牌。无论成败，调用方都应在绑定成功后调用 DropBindToken / ConsumeBindToken。
func (f *Flow) LookupBindToken(ctx context.Context, token string) (*oauth.Identity, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrBindToken
	}
	raw, err := f.cache.Get(ctx, bindKey(token))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, ErrBindToken
	}
	if err != nil {
		return nil, err
	}
	var rec bindRec
	if json.Unmarshal([]byte(raw), &rec) != nil {
		return nil, ErrBindToken
	}
	remain := time.Until(time.Unix(rec.Exp, 0))
	if remain <= 0 {
		_ = f.cache.Del(ctx, bindKey(token)) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
		return nil, ErrBindToken
	}
	n, err := cache.Incr(f.cache, ctx, bindTriesKey(token), remain)
	if err != nil {
		return nil, err
	}
	if n > MaxBindTries {
		_ = f.cache.Del(ctx, bindKey(token)) // 计数 key 不删：留给 TTL，避免已通过读取的并发尝试把计数重置为 0
		return nil, ErrBindToken
	}
	id := rec.Identity
	return &id, nil
}

// DropBindToken 作废令牌。
func (f *Flow) DropBindToken(ctx context.Context, token string) error {
	return f.cache.Del(ctx, bindKey(token))
}

// ConsumeBindToken 原子地取出并作废令牌（cache.GetDel），返回令牌里的身份。
// 多实例下并发的两次绑定只有一个能消费成功，另一个得到 ErrBindToken；应在账号密码校验通过后、写入绑定前调用。
func (f *Flow) ConsumeBindToken(ctx context.Context, token string) (*oauth.Identity, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrBindToken
	}
	raw, err := cache.GetDel(f.cache, ctx, bindKey(token))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, ErrBindToken
	}
	if err != nil {
		return nil, err
	}
	var rec bindRec
	if json.Unmarshal([]byte(raw), &rec) != nil || time.Now().Unix() >= rec.Exp {
		return nil, ErrBindToken
	}
	id := rec.Identity
	return &id, nil
}
