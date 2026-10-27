package aigateway

// 宿主侧进程内 API。应用（RAG/办公/客服等）import 本包取解密后的模型调用配置，
// 并在调用后回报 token 用量——这是"模型密钥集中 + 按租户计费"的接缝。
// 结果事件 aigateway.usage.recorded 可订阅做准实时计费。

import (
	"context"
	"errors"

	"nexadmin/core/response"
)

// defaultSvc 在插件 Register 时设置；宿主模块的调用都发生在请求期，此时插件已挂载。
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
	return response.Unavailable("aigateway.plugin_inactive")
}

// Resolve 返回某类能力（llm/embedding/rerank/...）默认启用模型的调用配置（密钥已解密）。
// 未配置或未启用返回 ErrDisabled；插件未挂载返回 503 语义错误。
func Resolve(ctx context.Context, kind string) (*Config, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	cfg, err := s.Resolve(ctx, kind)
	if errors.Is(err, ErrDisabled) {
		return nil, err // 语义保留：宿主可 errors.Is 降级
	}
	return cfg, err
}

// RecordUsage 回报一笔 token 用量（幂等性由调用方 bizType+bizNo 关联保证；
// 台账是事实记录，重复回报会被计费层按关联键去重）。永不返回错误。
func RecordUsage(ctx context.Context, in UsageInput) {
	s := current()
	if s == nil {
		return
	}
	s.RecordUsage(ctx, in)
}

// UsageSummary 当前租户按 kind 聚合的 token 用量（对账/计费用）。
func UsageSummary(ctx context.Context) ([]UsageSummaryRow, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.UsageSummary(ctx)
}
