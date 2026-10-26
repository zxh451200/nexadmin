package workflow

// 宿主侧进程内 API：业务模块（如商城的大额订单审批）import 本包发起/查询/推进流程，
// 不走 HTTP。审批结果通过 event.Subscribe 订阅 workflow.* 事件获得。

import (
	"context"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
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
	return response.Unavailable("workflow.plugin_inactive")
}

// StartInstance 发起（或幂等取回）一个流程实例；bizType/businessKey 挂业务对象。
func StartInstance(ctx context.Context, in StartInput, actor string) (*InstView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.InstStart(ctx, in, actor)
}

// Get 按 ID 取实例详情（含状态快照与流转历史）。
func Get(ctx context.Context, id idgen.ID) (*InstView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.InstGet(ctx, id)
}

// GetRunningByBiz 取某业务键当前运行中的实例；没有返回 404 语义。
func GetRunningByBiz(ctx context.Context, bizType, businessKey string) (*InstView, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	list, _, err := s.InstList(ctx, InstQuery{BizType: bizType, Status: StatusRunning, Page: 1, PageSize: 50}, "")
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].BusinessKey == businessKey {
			return &list[i], nil
		}
	}
	return nil, response.NotFound("workflow.inst_not_found")
}
