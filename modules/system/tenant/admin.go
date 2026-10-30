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

type bootDept struct {
	database.TenantModel
	Name       string
	ParentID   *idgen.ID
	Path       string
	Sort       int
	LeaderName string
	Phone      string
	Email      string
	Status     string
}

func (bootDept) TableName() string { return "sys_dept" }

type bootPost struct {
	database.TenantModel
	Name   string
	Code   string
	DeptID *idgen.ID
	Sort   int
	Status string
	Remark string
}

func (bootPost) TableName() string { return "sys_post" }

type bootRoleMenu struct {
	RoleID idgen.ID
	MenuID idgen.ID
}

func (bootRoleMenu) TableName() string { return "sys_role_menu" }

// 新租户的基础组织与只读角色；编码固定，重复调用按 code 复用。
const (
	staffRoleCode = "staff"
	staffRoleName = "普通员工"
)

// 普通员工能看到的平台级页面：工作台、个人中心、关于。
var staffPerms = []string{"dashboard:view", "profile:view", "about:view"}

// seedTenantBase 在租户创建事务里初始化根部门、默认岗位与“普通员工”角色。
// 让新租户开箱就有组织骨架，不必先手工建部门/岗位才能建用户。
func seedTenantBase(tx *gorm.DB, tid idgen.ID) error {
	tx = tx.WithContext(ctenant.Skip(tx.Statement.Context))

	var deptCount int64
	if err := tx.Model(&bootDept{}).Where("tenant_id = ?", int64(tid)).Count(&deptCount).Error; err != nil {
		return err
	}
	if deptCount == 0 {
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		d := &bootDept{Name: "总部", Path: "/" + id.String() + "/", Sort: 10, Status: "1"}
		d.TenantID = tid
		d.ID = id
		if err := tx.Create(d).Error; err != nil {
			return err
		}
	}

	var postCount int64
	if err := tx.Model(&bootPost{}).Where("tenant_id = ? AND code = ?", int64(tid), "default").Count(&postCount).Error; err != nil {
		return err
	}
	if postCount == 0 {
		p := &bootPost{Name: "默认岗位", Code: "default", Sort: 10, Status: "1", Remark: "开通租户时自动创建"}
		p.TenantID = tid
		if err := tx.Create(p).Error; err != nil {
			return err
		}
	}

	var staff adminRole
	err := tx.Where("tenant_id = ? AND code = ?", int64(tid), staffRoleCode).Take(&staff).Error
	switch {
	case err == nil:
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Builtin 必须为 false：内置角色会被用户鉴权当成超管角色展开为 "*"。
		staff = adminRole{TenantID: tid, Name: staffRoleName, Code: staffRoleCode, Sort: 20, Status: "1",
			DataScope: "self", Builtin: false, Assignable: true, Remark: "普通员工，默认仅本人数据"}
		if err := tx.Create(&staff).Error; err != nil {
			return err
		}
	default:
		return err
	}

	var menuIDs []int64
	if err := tx.Table("sys_menu").Where("perm IN ? AND status = ?", staffPerms, "1").Pluck("id", &menuIDs).Error; err != nil {
		return err
	}
	rows := make([]bootRoleMenu, 0, len(menuIDs))
	for _, m := range menuIDs {
		rows = append(rows, bootRoleMenu{RoleID: staff.ID, MenuID: idgen.ID(m)})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

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
	if err := tx.Create(&adminUserRole{UserID: u.ID, RoleID: role.ID}).Error; err != nil {
		return err
	}
	return seedTenantBase(tx, tid)
}
