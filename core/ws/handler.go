package ws

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"nexadmin/core/auth"
	"nexadmin/core/response"
)

// Upgrader 协议升级器（允许指定 Origin 的跨域 WS）。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	// Origin 同源校验：非浏览器客户端（无 Origin 头）放行；带 Origin 的必须与 Host 同源。
	// 恒 true 等于任意网页都能对本站发起 WS 握手（跨站 WS 劫持），注释里的「反代管 CORS」
	// 在默认部署里并不存在。
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	},
}

// Handler 是挂到路由上的 WS 端点处理器。
type Handler struct {
	hub  *Hub
	auth *auth.Service
}

// NewHandler 创建 WS 端点处理器。
func NewHandler(h *Hub, a *auth.Service) *Handler {
	return &Handler{hub: h, auth: a}
}

// identify 解析这条连接的身份：浏览器用一次性入场票（?ticket=，由 POST /api/v1/ws/ticket 换取），
// 不能设头的场景之外也可以直接带 Authorization 头。访问令牌不再从查询参数读取——
// 握手 URL 会进反代日志与浏览器网络面板，长寿命令牌出现在那里等于外泄。
func (h *Handler) identify(c *gin.Context) (*auth.Claims, error) {
	if h.auth == nil {
		return nil, nil
	}
	if tk := strings.TrimSpace(c.Query("ticket")); tk != "" {
		cl, err := h.auth.RedeemWSTicket(c.Request.Context(), tk)
		if errors.Is(err, auth.ErrInvalidToken) {
			return nil, response.Unauthorized("error.unauthorized")
		}
		if err != nil {
			// 缓存故障不能伪装成认证失败，否则运维只能看到一个 401。
			return nil, response.Unavailable("error.unavailable").Wrap(err)
		}
		return cl, nil
	}
	if tok := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "); tok != "" {
		cl, err := h.auth.ParseAccess(c.Request.Context(), tok)
		if err != nil {
			return nil, response.Unauthorized("error.unauthorized")
		}
		return cl, nil
	}
	return nil, nil
}

// Handle 处理 GET /ws 升级请求：?token=<accessToken> 鉴权 → 升级 → 注册连接 →
// 进入读写循环。读循环处理 subscribe / unsubscribe / ping；写循环消费出站队列。
func (h *Handler) Handle(c *gin.Context) {
	cl, err := h.identify(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 匿名连接只能订阅 public: 前缀的公开主题；业务主题的归属由 TopicGuard 判定。

	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// upgrader 已写 400 响应
		return
	}
	connID := randomID()
	userID := ""
	if cl != nil {
		userID = cl.UserID.String()
	}
	conn := h.hub.Register(connID, userID, nil)
	defer func() {
		conn.Close()
		_ = wsConn.Close()
	}()

	// 写循环：消费出站队列 + 协议层 ping。gorilla 只允许一个写者：错误帧/应用层 pong
	// 来自读循环，必须与这里的 WriteJSON 互斥（writeMu）；WriteControl 文档明确可并发，不参与。
	var writeMu sync.Mutex
	writeFrame := func(frame Frame) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = wsConn.SetWriteDeadline(time.Now().Add(WriteWait))
		return wsConn.WriteJSON(frame) == nil
	}
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		ticker := time.NewTicker(PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-conn.closed:
				_ = wsConn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(WriteWait))
				return
			case frame := <-conn.Send:
				if !writeFrame(frame) {
					conn.Close()
					return
				}
			case <-ticker.C:
				_ = wsConn.SetWriteDeadline(time.Now().Add(WriteWait))
				if err := wsConn.WriteControl(websocket.PingMessage, nil, time.Now().Add(WriteWait)); err != nil {
					conn.Close()
					return
				}
			}
		}
	}()

	// 读循环：处理入站帧（gin 不包 this handler，gorilla 自己读）。
	for {
		var f Frame
		if err := wsConn.ReadJSON(&f); err != nil {
			conn.Close()
			break
		}
		switch f.Type {
		case TypeSubscribe:
			if f.Topic == "" {
				continue
			}
			if !AllowTopic(cl, f.Topic) {
				writeFrame(Frame{Type: TypeError, Topic: f.Topic, Data: map[string]any{"message": "topic not allowed"}})
				continue
			}
			h.hub.Subscribe(connID, f.Topic)
		case TypeUnsubscribe:
			if f.Topic != "" {
				h.hub.Unsubscribe(connID, f.Topic)
			}
		case TypePing:
			writeFrame(Frame{Type: TypePong})
		}
	}
	<-writeDone
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ws_" + hex.EncodeToString(b)
}
