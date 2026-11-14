package im

// 宿主侧进程内 API。业务模块（如商城）import 本包做服务端集成（比如代访客开单、
// 订阅 im.message.created 推通知）；HTTP 接口给前端用。

import (
	"context"

	"nexadmin/core/response"
)

// defaultSvc 在插件 Register 时设置；宿主调用发生在请求期，此时插件已挂载。
var defaultSvc = make(chan *Service, 1)

// SetDefault 由插件装配层调用，注册进程级默认服务。
func SetDefault(s *Service) {
	select {
	case <-defaultSvc: // 已有则替换
	default:
	}
	defaultSvc <- s
}

func current() *Service {
	select {
	case s := <-defaultSvc:
		defaultSvc <- s
		return s
	default:
		return nil
	}
}

func responseUnavailable() error {
	return response.Unavailable("im.plugin_inactive")
}

// Open 打开（或幂等取回）一个会话。
func Open(ctx context.Context, in OpenInput) (*ConversationView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	c, err := s.Open(ctx, in)
	if err != nil {
		return nil, err
	}
	v := convView(c)
	return &v, nil
}

// Messages 增量拉取会话消息（sinceSeq=0 时返回最近的一页）。
func Messages(ctx context.Context, convNo string, sinceSeq, limit int) (*ConversationView, []Message, error) {
	s := current()
	if s == nil {
		return nil, nil, responseUnavailable()
	}
	c, rows, _, err := s.Messages(ctx, convNo, MessageQuery{Since: int64(sinceSeq), Limit: limit})
	if err != nil {
		return nil, nil, err
	}
	v := convView(c)
	return &v, rows, nil
}

// Send 由宿主服务端代发一条消息（如机器人应答、系统提示用 SenderSystem）。
func Send(ctx context.Context, convNo string, in SendInput) (*Message, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.Send(ctx, convNo, in)
}

// LoadSettings 读取插件设置（含 AI 自动回答开关与兜底话术；kb 插件的机器人据此决定是否应答）。
// 名字避开 Settings 类型（settings.go 的同名结构体）。
func LoadSettings(ctx context.Context) (Settings, error) {
	s := current()
	if s == nil {
		return Settings{}, responseUnavailable()
	}
	return s.Settings(ctx), nil
}

// RequestAgent 转人工排队：把未分配的会话交给自动分配（在线坐席直接落人，否则排队）。
// AI 兜底用：检索答不出来时礼貌说明并把它交给人工。
func RequestAgent(ctx context.Context, convNo string) error {
	s := current()
	if s == nil {
		return responseUnavailable()
	}
	return s.RequestAgent(ctx, convNo)
}
