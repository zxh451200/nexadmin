package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

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

// RoleAssignmentObserver 同步清理外部身份来源的角色记账；回调与用户角色替换共用事务。
type RoleAssignmentObserver func(ctx context.Context, tx *gorm.DB, tenantID, userID idgen.ID, previousRoles, roleIDs []idgen.ID) error

var roleAssignmentObservers = struct {
	sync.RWMutex
	next uint64
	fns  map[uint64]RoleAssignmentObserver
}{fns: make(map[uint64]RoleAssignmentObserver)}

func RegisterRoleAssignmentObserver(observer RoleAssignmentObserver) func() {
	if observer == nil {
		return func() {}
	}
	roleAssignmentObservers.Lock()
	roleAssignmentObservers.next++
	id := roleAssignmentObservers.next
	roleAssignmentObservers.fns[id] = observer
	roleAssignmentObservers.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			roleAssignmentObservers.Lock()
			delete(roleAssignmentObservers.fns, id)
			roleAssignmentObservers.Unlock()
		})
	}
}

func NotifyRoleAssignmentsReplaced(ctx context.Context, tx *gorm.DB, tenantID, userID idgen.ID, previousRoles, roleIDs []idgen.ID) error {
	roleAssignmentObservers.RLock()
	observers := make([]RoleAssignmentObserver, 0, len(roleAssignmentObservers.fns))
	for _, observer := range roleAssignmentObservers.fns {
		observers = append(observers, observer)
	}
	roleAssignmentObservers.RUnlock()
	var err error
	for _, observer := range observers {
		err = errors.Join(err, observer(ctx, tx, tenantID, userID, previousRoles, roleIDs))
	}
	return err
}
