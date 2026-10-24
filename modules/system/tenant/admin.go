package tenant

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/core/response"
	ctenant "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 租户管理员角色沿用出厂内置角色的约定（见 role 种子里的 admin）：
// 内置、数据范围“全部”、不可被下级分配；每个租户各有一份，编码固定为 admin。
const (
	adminRoleCode = "admin"
	adminRoleName = "租户管理员"
)

var (
	adminUsernameRe = regexp.MustCompile(`^[A-Za-z0-9_.@-]{2,64}$`)
	adminEmailRe    = regexp.MustCompile(`^[^@\s]+@[^@\s]+$`)
)

// adminPlan 是校验通过、已哈希的租户管理员信息（不含明文密码）。
type adminPlan struct {
	username string
	email    string
	hash     string
}

// newAdminPlan 校验并哈希初始密码。明文密码不进入返回值、错误信息与日志。
func newAdminPlan(in TenantInput) (*adminPlan, error) {
	name := strings.TrimSpace(in.AdminUsername)
	email := strings.TrimSpace(in.AdminEmail)
	if !adminUsernameRe.MatchString(name) || in.AdminPassword == "" || len(in.AdminPassword) > 72 ||
		(email != "" && (len(email) > 100 || !adminEmailRe.MatchString(email))) {
		return nil, response.BadRequest("tenant.admin_invalid")
	}
	hash, err := auth.HashPassword(in.AdminPassword)
	if err != nil {
		return nil, errors.New("tenant: hash admin password failed")
	}
	return &adminPlan{username: name, email: email, hash: hash}, nil
}

// 以下是只含建号所需列的表映射，避免 import 用户/角色模块。显式写 tenant_id，创建时不依赖请求上下文。

type adminRole struct {
	database.PlatformModel
	TenantID   idgen.ID
	Name       string
	Code       string
	Sort       int
	Status     string
	DataScope  string
	Builtin    bool
	Assignable bool
	Remark     string
}

func (adminRole) TableName() string { return "sys_role" }

type adminUser struct {
	database.PlatformModel
	TenantID idgen.ID
	Username string
	Password string
	Nickname string
	Avatar   string
	Phone    string
	Email    string
	Gender   string
	DeptID   idgen.ID
	Status   string
	Province string
	City     string
	District string
	Remark   string
	IsSuper  bool
}

func (adminUser) TableName() string { return "sys_user" }

type adminUserRole struct {
	UserID idgen.ID
	RoleID idgen.ID
}

func (adminUserRole) TableName() string { return "sys_user_role" }

// createTenantAdmin 在 tx（外层事务）里为新租户建「租户管理员」角色（已存在则复用）和管理员账号并关联。
// 任何一步出错都返回错误，由外层事务整体回滚。
func createTenantAdmin(ctx context.Context, tx *gorm.DB, tid idgen.ID, p *adminPlan) error {
	tx = tx.WithContext(ctenant.Skip(ctx)) // 显式按 tenant_id 操作，不受请求所在租户的隔离条件影响

	var role adminRole
	err := tx.Where("tenant_id = ? AND code = ?", int64(tid), adminRoleCode).Take(&role).Error
	switch {
	case err == nil:
	case errors.Is(err, gorm.ErrRecordNotFound):
		role = adminRole{TenantID: tid, Name: adminRoleName, Code: adminRoleCode, Sort: 10, Status: "1",
			DataScope: "all", Builtin: true, Assignable: false, Remark: "拥有本租户全部权限，不可删除"}
		if err := tx.Create(&role).Error; err != nil {
			return err
		}
	default:
		return err
	}

	u := adminUser{TenantID: tid, Username: p.username, Password: p.hash, Nickname: p.username, Email: p.email,
		Gender: "0", Status: "1"}
	if err := tx.Create(&u).Error; err != nil {
		return err
	}
	return tx.Create(&adminUserRole{UserID: u.ID, RoleID: role.ID}).Error
}
