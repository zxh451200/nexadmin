package user

import (
	"context"

	"nexadmin/core"
	"nexadmin/pkg/idgen"
)

type userProvisioner struct{ svc *service }

// provisionActor 是 SCIM 等外部供号通道的操作者：数据范围同超管，但 guardTarget 不豁免，
// 因此超管与持有内置角色的账号既不能被改资料，也不能被停用。
var provisionActor = actor{Super: true, Provision: true}

func (p userProvisioner) ProvisionUser(ctx context.Context, in core.ProvisionUserInput) (core.ProvisionedUser, error) {
	if in.Username == "" {
		return core.ProvisionedUser{}, bad("username is required")
	}
	status := in.Status
	if status == "" {
		status = statusOn
	}
	out, err := p.svc.create(ctx, userInput{
		ExternalID: in.ExternalID, Username: in.Username, Nickname: in.Nickname, Email: in.Email, Phone: in.Phone, Status: status, NoPassword: true,
	}, provisionActor)
	if err != nil {
		return core.ProvisionedUser{}, err
	}
	return core.ProvisionedUser{ID: out.ID, TenantID: tenantOf(ctx), Username: out.Username, Nickname: out.Nickname, Email: out.Email, Phone: out.Phone, Status: out.Status, ExternalID: out.ExternalID}, nil
}

func (p userProvisioner) FindProvisionedUser(ctx context.Context, key string) (core.ProvisionedUser, error) {
	var u User
	q := p.svc.db.WithContext(ctx)
	if err := q.Where("external_id = ? OR username = ?", key, key).First(&u).Error; err != nil {
		return core.ProvisionedUser{}, err
	}
	return toProvisioned(u), nil
}

func (p userProvisioner) UpdateProvisionedUser(ctx context.Context, id idgen.ID, in core.ProvisionUserInput) (core.ProvisionedUser, error) {
	u, err := p.svc.find(ctx, id)
	if err != nil {
		return core.ProvisionedUser{}, err
	}
	status := in.Status
	if status == "" {
		status = u.Status
	}
	// update 接收完整输入；回填 SCIM 未管理的本地字段及关联，避免被零值覆盖。
	roles, err := p.svc.roleIDsOf(ctx, id)
	if err != nil {
		return core.ProvisionedUser{}, err
	}
	var posts []userPost
	if err := p.svc.db.WithContext(ctx).Where("user_id = ?", int64(id)).Find(&posts).Error; err != nil {
		return core.ProvisionedUser{}, err
	}
	input := userInput{
		ExternalIDSet: in.ExternalIDSet,
		ExternalID:    in.ExternalID, Username: in.Username, Nickname: in.Nickname, Email: in.Email, Phone: in.Phone, Status: status,
		Avatar: u.Avatar, Gender: u.Gender, Remark: u.Remark,
		Region: regionDTO{Province: u.Province, City: u.City, District: u.District},
	}
	if u.DeptID != 0 {
		input.DeptID = u.DeptID.String()
	}
	for _, role := range roles {
		input.RoleIDs = append(input.RoleIDs, role.String())
	}
	for _, post := range posts {
		input.PostIDs = append(input.PostIDs, post.PostID.String())
	}
	out, err := p.svc.update(ctx, id, input, provisionActor)
	if err != nil {
		return core.ProvisionedUser{}, err
	}
	return core.ProvisionedUser{ID: out.ID, TenantID: tenantOf(ctx), Username: out.Username, Nickname: out.Nickname, Email: out.Email, Phone: out.Phone, Status: out.Status, ExternalID: out.ExternalID}, nil
}

func (p userProvisioner) DisableProvisionedUser(ctx context.Context, id idgen.ID) error {
	return p.svc.setStatus(ctx, id, statusOff, provisionActor)
}

func toProvisioned(u User) core.ProvisionedUser {
	return core.ProvisionedUser{ID: u.ID, TenantID: u.TenantID, ExternalID: u.ExternalID, Username: u.Username, Nickname: u.Nickname, Email: u.Email, Phone: u.Phone, Status: u.Status}
}

var _ core.UserProvisioner = userProvisioner{}
