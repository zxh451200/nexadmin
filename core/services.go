package core

import (
	"context"

	coreai "nexadmin/core/ai"
	"nexadmin/pkg/idgen"
)

// UnreadCount 是站内消息与公告的未读数。
type UnreadCount struct {
	Messages int64 `json:"messages"`
	Notices  int64 `json:"notices"`
}

// MessageCenter 由内置消息模块实现，插件通过 core 获取，避免 import modules/*。
type MessageCenter interface {
	Unread(ctx context.Context, uid idgen.ID) (UnreadCount, error)
}

var (
	aiGatewayFactory     func(*Deps) *coreai.Gateway
	messageCenterFactory func(*Deps) MessageCenter
)

// SetAIGatewayFactory 由 modules/ai 注册统一装配（含限流、租户策略与配额）的网关工厂。
func SetAIGatewayFactory(f func(*Deps) *coreai.Gateway) { aiGatewayFactory = f }

// AIGateway 返回已按当前配置装配的网关；未注册工厂时返回 nil。
func AIGateway(d *Deps) *coreai.Gateway {
	if aiGatewayFactory == nil {
		return nil
	}
	return aiGatewayFactory(d)
}

// SetMessageCenterFactory 由 modules/message 注册。
func SetMessageCenterFactory(f func(*Deps) MessageCenter) { messageCenterFactory = f }

// MessageCenterOf 返回站内消息服务；未注册时返回 nil（调用方需判空）。
func MessageCenterOf(d *Deps) MessageCenter {
	if messageCenterFactory == nil {
		return nil
	}
	return messageCenterFactory(d)
}
