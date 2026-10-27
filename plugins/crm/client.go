package crm

// 宿主侧进程内 API。hotel/mall 的 member 模块 import 本包，把自己的会员锚到
// 全生态统一的客户 ID；结果通过 event.Subscribe 订阅 crm.* 事件获得。

import (
	"context"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func idgenOf(v int64) idgen.ID { return idgen.ID(v) }

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
	return response.Unavailable("crm.plugin_inactive")
}

// ResolveByExternal 按 (source, externalNo) 反查客户；fallbackName 非空且未命中时
// 自动建档。返回 (customer, created, err)——member 侧以 customer.ID 作为全生态客户 ID。
func ResolveByExternal(ctx context.Context, source, externalNo, fallbackName string) (*Customer, bool, error) {
	s := current()
	if s == nil {
		return nil, false, responseUnavailable()
	}
	return s.ResolveByExternal(ctx, source, externalNo, fallbackName)
}

// BindExternal 把外部身份绑到已有客户上（幂等）。
func BindExternal(ctx context.Context, customerID int64, source, externalNo string) (*Customer, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.BindExternal(ctx, idgenOf(customerID), source, externalNo)
}

// CreateCustomer 创建（或按锚点幂等取回）客户主数据。
func CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.CreateCustomer(ctx, in)
}
