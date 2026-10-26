// Package ws 是框架通用 WebSocket hub：鉴权握手、按用户订阅主题、心跳保活、
// 断线自动清理。所有插件的实时推送（IM 消息、告警、工作流待办、通知）共用一条
// WS 连接——前端只建一次连接，订阅多个主题。
//
// 协议（JSON 帧）：
//
//	客户端 → 服务端  {"type":"subscribe","topic":"im:CONV123"}     订阅主题
//	客户端 → 服务端  {"type":"unsubscribe","topic":"im:CONV123"}   取消订阅
//	客户端 → 服务端  {"type":"ping"}                                应用层心跳
//	服务端 → 客户端  {"type":"pong"}                                心跳回应
//	服务端 → 客户端  {"type":"message","topic":"im:CONV123","data":{...}}  主题推送
//	服务端 → 客户端  {"type":"error","data":{"message":"..."}}     错误通知
//
// 鉴权：握手 URL 带 ?token=<accessToken>（浏览器 WebSocket API 不能设 Header，
// 与 EventSource 同样的约束）；JWT 解析失败返回 401 并拒绝升级。
// 多实例：Publish 经 cluster.Bus 广播，每个实例只推给本地持有的连接。
package ws

import (
	"sync"
	"time"
)

// Conn 是一个已升级的 WebSocket 连接（用户级，非主题级）。
type Conn struct {
	ID     string     // 连接 ID（随机，用于日志）
	UserID string     // 挂载后的用户 ID
	Send   chan Frame // 出站队列（满则丢弃该帧，不阻塞其它连接）
	closed chan struct{}
	once   sync.Once
}

// Close 关闭连接（幂等）。
func (c *Conn) Close() {
	c.once.Do(func() { close(c.closed) })
}

// IsClosed 报告连接是否已关闭。
func (c *Conn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// Hub 管理全部活跃连接与主题订阅。
type Hub struct {
	mu      sync.RWMutex
	conns   map[string]*Conn           // connID → Conn
	topics  map[string]map[string]bool // topic → set(connID)
	subsOf  map[string]map[string]bool // connID → set(topic)
	onClose func(connID string, topics []string)
	sendBuf int
}

// NewHub 创建 hub；sendBuf 是每个连接的出站队列深度（默认 64）。
func NewHub(sendBuf int) *Hub {
	if sendBuf <= 0 {
		sendBuf = 64
	}
	return &Hub{
		conns:   map[string]*Conn{},
		topics:  map[string]map[string]bool{},
		subsOf:  map[string]map[string]bool{},
		sendBuf: sendBuf,
	}
}

// Register 注册一个新连接（握手成功后调用）。onClose 在连接移除时回调（含其订阅主题，
// 供调用方清理外部状态；可为 nil）。
func (h *Hub) Register(connID, userID string, onClose func(connID string, topics []string)) *Conn {
	c := &Conn{
		ID: connID, UserID: userID,
		Send:   make(chan Frame, h.sendBuf),
		closed: make(chan struct{}),
	}
	h.mu.Lock()
	h.conns[connID] = c
	h.subsOf[connID] = map[string]bool{}
	h.mu.Unlock()

	// 包装 onClose：移除时自动清主题。
	go func() {
		<-c.closed
		h.mu.Lock()
		topics := make([]string, 0, len(h.subsOf[connID]))
		for t := range h.subsOf[connID] {
			topics = append(topics, t)
			delete(h.topics[t], connID)
			if len(h.topics[t]) == 0 {
				delete(h.topics, t)
			}
		}
		delete(h.subsOf, connID)
		delete(h.conns, connID)
		h.mu.Unlock()
		if onClose != nil {
			onClose(connID, topics)
		}
	}()
	return c
}

// Subscribe 把连接加入主题。
func (h *Hub) Subscribe(connID, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.conns[connID]; !ok {
		return
	}
	if h.topics[topic] == nil {
		h.topics[topic] = map[string]bool{}
	}
	h.topics[topic][connID] = true
	h.subsOf[connID][topic] = true
}

// Unsubscribe 把连接从主题移除。
func (h *Hub) Unsubscribe(connID, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.topics[topic], connID)
	if len(h.topics[topic]) == 0 {
		delete(h.topics, topic)
	}
	delete(h.subsOf[connID], topic)
}

// Publish 向主题的全部本地订阅者投递；无订阅者的主题静默丢弃（多实例下由
// cluster.Bus 保证其它实例的订阅者也能收到）。
func (h *Hub) Publish(topic string, data any) {
	h.mu.RLock()
	connIDs := make([]string, 0, len(h.topics[topic]))
	for id := range h.topics[topic] {
		connIDs = append(connIDs, id)
	}
	h.mu.RUnlock()

	frame := Frame{Type: "message", Topic: topic, Data: data}
	for _, id := range connIDs {
		c, ok := h.conns[id]
		if !ok || c.IsClosed() {
			continue
		}
		select {
		case c.Send <- frame:
		default: // 队列满：丢帧不阻塞（前端靠 HTTP 拉取兜底）
		}
	}
}

// PublishToUser 向指定用户的全部活跃连接投递（通知类场景）。
func (h *Hub) PublishToUser(userID string, data any) {
	h.mu.RLock()
	var connIDs []string
	for id, c := range h.conns {
		if c.UserID == userID {
			connIDs = append(connIDs, id)
		}
	}
	h.mu.RUnlock()

	for _, id := range connIDs {
		c := h.conns[id]
		if c == nil || c.IsClosed() {
			continue
		}
		frame := Frame{Type: "message", Topic: "user:" + userID, Data: data}
		select {
		case c.Send <- frame:
		default:
		}
	}
}

// ConnCount 返回活跃连接数（监控用）。
func (h *Hub) ConnCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// TopicCount 返回主题数（监控用）。
func (h *Hub) TopicCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics)
}

// Frame 是 WS 上流转的一条消息。
type Frame struct {
	Type  string `json:"type"`            // subscribe | unsubscribe | ping | pong | message | error
	Topic string `json:"topic,omitempty"` // 订阅/消息的主题
	Data  any    `json:"data,omitempty"`
}

// Default 是进程级默认 hub（server.go 在启动时 Set）。
var defaultHub *Hub

// SetDefault 设置进程级 hub。
func SetDefault(h *Hub) { defaultHub = h }

// Default 返回进程级 hub（未初始化时 nil）。
func Default() *Hub { return defaultHub }

// Publish 是 Default().Publish 的快捷方式（hub 未初始化时静默丢弃）。
func Publish(topic string, data any) {
	if defaultHub != nil {
		defaultHub.Publish(topic, data)
	}
}

// PublishToUser 是 Default().PublishToUser 的快捷方式。
func PublishToUser(userID string, data any) {
	if defaultHub != nil {
		defaultHub.PublishToUser(userID, data)
	}
}

// 帧类型常量。
const (
	TypeSubscribe   = "subscribe"
	TypeUnsubscribe = "unsubscribe"
	TypePing        = "ping"
	TypePong        = "pong"
	TypeMessage     = "message"
	TypeError       = "error"
)

// 心跳间隔与写超时。
const (
	PingInterval = 30 * time.Second
	WriteWait    = 10 * time.Second
)
