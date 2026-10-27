package pay

// 宿主侧进程内 API。业务模块（如商城 trade / 酒店房账）import 本包，直接调用
// 下面的包级函数，不走 HTTP；支付结果通过 event.Subscribe 订阅 pay.* 事件获得，
// 并以 QueryByBiz 兜底。落账幂等规范：账本行唯一键
// (tenant_id, ledger_id, biz_date, item_type, sub_key)，sub_key = paymentNo / refundNo。

import (
	"context"

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
	return response.Unavailable("pay.plugin_inactive")
}

// Create 发起（或幂等取回）一笔支付，返回订单视图与拉起支付的参数。
func Create(ctx context.Context, in CreateInput) (*OrderView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	o, params, err := s.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	v := orderView(o, params)
	return &v, nil
}

// QueryByBiz 按业务幂等键查询支付单（pending 时顺带向渠道补查一次）。
// 不存在时返回 404 语义的 *response.Error。
func QueryByBiz(ctx context.Context, bizType, bizNo string) (*OrderView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	o, err := s.QueryByBiz(ctx, bizType, bizNo)
	if err != nil {
		return nil, err
	}
	v := orderView(o, nil)
	return &v, nil
}

// CreateRefund 发起退款（in.BizType/BizNo 是退款幂等键）；渠道拒绝时退款单落
// failed 并返回 409。异步渠道落 pending 并把支付单置 refunding。
func CreateRefund(ctx context.Context, in RefundInput) (*RefundView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	r, err := s.CreateRefund(ctx, in)
	if err != nil {
		return nil, err
	}
	v := refundView(r)
	return &v, nil
}

// CreateAuth 冻结一笔预授权；CaptureAuth 请款（产生收款单并触发
// pay.auth.captured）；VoidAuth 撤销剩余额度。
func CreateAuth(ctx context.Context, in CreateAuthInput) (*AuthView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	a, err := s.CreateAuth(ctx, in)
	if err != nil {
		return nil, err
	}
	v := authView(a)
	return &v, nil
}

// CaptureAuth 请款。amountMinor ≤ 剩余额度；bizType/bizNo 是请款收款单的幂等键。
func CaptureAuth(ctx context.Context, authNo string, in CaptureAuthInput) (*AuthView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	a, err := s.CaptureAuth(ctx, authNo, in)
	if err != nil {
		return nil, err
	}
	v := authView(a)
	return &v, nil
}
