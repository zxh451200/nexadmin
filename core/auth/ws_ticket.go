package auth

// WS 入场票：浏览器 WebSocket 不能设 Authorization 头，而访问令牌又不该出现在握手 URL 里
// （反代访问日志、浏览器 Network 面板、错误上报都会原样记下 query）。
// 这里换成一张短时效、用后即废的随机票：先用正常的带头请求换票，再用票建立连接。
// 与 IM 的 SSE 流票同思路，但存 core/cache，多实例部署下换票和连接可以落在不同节点。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"nexadmin/core/cache"
	"nexadmin/pkg/idgen"
)

// wsTicketTTL 只覆盖「换票 → 建连」这一趟，不承担会话生命周期（var 仅为让单测能缩短）。
var wsTicketTTL = 15 * time.Second

// wsTicketIDLen 是随机票的十六进制长度（16 字节）。
const wsTicketIDLen = 32

type wsTicket struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Username string `json:"un"`
	Super    bool   `json:"su,omitempty"`
}

func wsTicketKey(id string) string { return cache.GlobalKey("auth", "wsticket", id) }

// validWSTicketID 拒绝非十六进制输入，避免把客户端字符串直接拼进缓存键。
func validWSTicketID(id string) bool {
	if len(id) != wsTicketIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// IssueWSTicket 为当前登录会话签发一张一次性入场票，返回票面与剩余可用秒数。
func (s *Service) IssueWSTicket(ctx context.Context, cl *Claims) (string, int, error) {
	if cl == nil || cl.UserID == 0 {
		return "", 0, errors.New("auth: ws ticket requires a logged-in principal")
	}
	buf := make([]byte, wsTicketIDLen/2)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, err
	}
	id := hex.EncodeToString(buf)
	raw, err := json.Marshal(wsTicket{
		UserID:   cl.UserID.String(),
		TenantID: cl.TenantID.String(),
		Username: cl.Username,
		Super:    cl.Super,
	})
	if err != nil {
		return "", 0, err
	}
	if err := s.cache.Set(ctx, wsTicketKey(id), string(raw), wsTicketTTL); err != nil {
		return "", 0, err
	}
	return id, int(wsTicketTTL.Seconds()), nil
}

// RedeemWSTicket 原子消费入场票：过期、伪造或已被用掉都是 ErrInvalidToken。
func (s *Service) RedeemWSTicket(ctx context.Context, id string) (*Claims, error) {
	if !validWSTicketID(id) {
		return nil, ErrInvalidToken
	}
	raw, err := cache.GetDel(s.cache, ctx, wsTicketKey(id))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	var t wsTicket
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, ErrInvalidToken
	}
	uid, err := idgen.Parse(t.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	tid, err := idgen.Parse(t.TenantID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return &Claims{UserID: uid, TenantID: tid, Username: t.Username, Super: t.Super, Kind: kindAccess}, nil
}
