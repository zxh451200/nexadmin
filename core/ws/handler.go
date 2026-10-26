package ws

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
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
	CheckOrigin: func(r *http.Request) bool {
		return true // P0 同源部署；跨域场景由反向代理管 CORS
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

// Handle 处理 GET /ws 升级请求：?token=<accessToken> 鉴权 → 升级 → 注册连接 →
// 进入读写循环。读循环处理 subscribe / unsubscribe / ping；写循环消费出站队列。
func (h *Handler) Handle(c *gin.Context) {
	// WebSocket API 不能设 Authorization 头，token 走查询参数（与 EventSource 流票同约束）。
	token := c.Query("token")
	if token == "" {
		token = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	}
	if h.auth != nil && token != "" {
		if _, err := h.auth.ParseAccess(c.Request.Context(), token); err != nil {
			response.Fail(c, response.Unauthorized("error.unauthorized"))
			return
		}
	}
	// token 为空时仍允许连接（公开主题，如访客 IM），订阅控制由业务侧在 topic 层做。

	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// upgrader 已写 400 响应
		return
	}
	connID := randomID()
	userID := ""
	if h.auth != nil && token != "" {
		if cl, perr := h.auth.ParseAccess(c.Request.Context(), token); perr == nil {
			userID = cl.UserID.String()
		}
	}
	conn := h.hub.Register(connID, userID, nil)
	defer func() {
		conn.Close()
		_ = wsConn.Close()
	}()

	// 写循环：消费出站队列 + 应用层 pong + 协议层 ping。
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
				_ = wsConn.SetWriteDeadline(time.Now().Add(WriteWait))
				if err := wsConn.WriteJSON(frame); err != nil {
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
			if f.Topic != "" {
				h.hub.Subscribe(connID, f.Topic)
			}
		case TypeUnsubscribe:
			if f.Topic != "" {
				h.hub.Unsubscribe(connID, f.Topic)
			}
		case TypePing:
			_ = wsConn.SetWriteDeadline(time.Now().Add(WriteWait))
			_ = wsConn.WriteJSON(Frame{Type: TypePong})
		}
	}
	<-writeDone
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ws_" + hex.EncodeToString(b)
}
