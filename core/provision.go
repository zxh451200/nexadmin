package core

import (
	"context"
	"errors"
	"sync/atomic"

	"nexadmin/pkg/idgen"
)

// UserProvisioner 是外部身份同步插件使用的受控用户生命周期接口。
// 实现方必须复用用户模块已有的租户隔离、额度、角色和审计规则。
type UserProvisioner interface {
	ProvisionUser(context.Context, ProvisionUserInput) (ProvisionedUser, error)
	UpdateProvisionedUser(context.Context, idgen.ID, ProvisionUserInput) (ProvisionedUser, error)
	DisableProvisionedUser(context.Context, idgen.ID) error
	FindProvisionedUser(context.Context, string) (ProvisionedUser, error)
}

type ProvisionUserInput struct {
	// ExternalIDSet 为 true 时更新 ExternalID，即使值为空；否则空值保留原值。
	ExternalIDSet bool
	ExternalID    string
	Username      string
	Nickname      string
	Email         string
	Phone         string
	Status        string
	RoleCodes     []string
}

type ProvisionedUser struct {
	ID         idgen.ID
	TenantID   idgen.ID
	ExternalID string
	Username   string
	Nickname   string
	Email      string
	Phone      string
	Status     string
}

var provisioner atomic.Value

func SetUserProvisioner(p UserProvisioner) {
	if p == nil {
		provisioner.Store((*userProvisionerHolder)(nil))
		return
	}
	provisioner.Store(&userProvisionerHolder{p: p})
}

func CurrentUserProvisioner() (UserProvisioner, bool) {
	v := provisioner.Load()
	if v == nil {
		return nil, false
	}
	h, ok := v.(*userProvisionerHolder)
	if !ok || h == nil || h.p == nil {
		return nil, false
	}
	return h.p, true
}

type userProvisionerHolder struct{ p UserProvisioner }

var ErrUserProvisionerUnavailable = errors.New("core: user provisioner unavailable")
