package role

import (
	"context"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 出厂角色，内容与前端 role.mock.ts 一致（含出厂 ID）。
// “角色-菜单”授权与 mock 一样默认留空：超管角色靠 builtin 直接拥有全部权限，其余角色由管理员在后台分配。

var factoryRoles = []struct {
	id         idgen.ID
	name       string
	code       string
	sort       int
	status     string
	dataScope  string
	deptIDs    []idgen.ID
	remark     string
	builtin    bool
	assignable bool
	date       string
}{
	{2100000000000000001, "超级管理员", "admin", 10, "1", scopeAll, nil, "拥有系统全部权限，不可删除", true, false, "2026-01-05"},
	{2100000000000000002, "运营", "operator", 20, "1", scopeDeptAndChild, nil, "负责内容与公告运营", false, true, "2026-01-12"},
	{2100000000000000003, "审计", "auditor", 30, "1", scopeCustom, []idgen.ID{1860000000000000008, 1860000000000000004}, "只读查看日志与在线用户", false, true, "2026-01-20"},
	{2100000000000000005, "内容编辑", "editor", 40, "0", scopeSelf, nil, "", false, true, "2026-02-08"},
	{2100000000000000004, "访客", "guest", 50, "1", scopeSelf, nil, "仅可浏览工作台", false, true, "2026-02-15"},
	{2100000000000000006, "安全管理员（不可分配）", "secadmin", 60, "1", scopeAll, nil, "演示：不允许被下级管理员分配的角色", false, false, "2026-02-20"},
}

// legacyRoleNames 早期出厂角色名带英文后缀（审计 Auditor、内容编辑 Editor）：角色名是数据不是
// UI 文案，中文界面里两套语言挤在一行既难看也难检索。按「默认租户 + 角色编码 + 旧名」就地改名，
// 管理员改过名的行不匹配，不会被覆盖。
var legacyRoleNames = map[string]string{
	"auditor": "审计 Auditor",
	"editor":  "内容编辑 Editor",
}

// SeedOnStartup 实现 setup.StartupSeeder：每次启动把早期双语角色名就地改名。
func (module) SeedOnStartup(db *gorm.DB) error {
	db = db.WithContext(tenant.Skip(context.Background())) // 启动期没有请求上下文，租户条件显式写
	for code, old := range legacyRoleNames {
		var name string
		for _, r := range factoryRoles {
			if r.code == code {
				name = r.name
			}
		}
		if name == "" {
			continue
		}
		if err := db.Model(&Role{}).Where("tenant_id = ? AND code = ? AND name = ?", int64(tenant.DefaultID), code, old).Update("name", name).Error; err != nil {
			return err
		}
	}
	return nil
}

// seedRoles 幂等种入出厂角色：按（默认租户, code）判断，已存在一律不动。
func seedRoles(db *gorm.DB) error {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	db = db.WithContext(tenant.Skip(ctx)) // 种子数据显式写 tenant_id，不依赖请求上下文
	return db.Transaction(func(tx *gorm.DB) error {
		for _, s := range factoryRoles {
			var n int64
			if err := tx.Model(&Role{}).Where("tenant_id = ? AND code = ?", int64(tenant.DefaultID), s.code).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			at, _ := time.Parse("2006-01-02", s.date)
			at = at.Add(8*time.Hour + 30*time.Minute)
			r := &Role{Name: s.name, Code: s.code, Sort: s.sort, Status: s.status, DataScope: s.dataScope, Builtin: s.builtin, Assignable: s.assignable, Remark: s.remark}
			r.ID, r.TenantID, r.CreatedAt, r.UpdatedAt = s.id, tenant.DefaultID, at, at
			if err := tx.Create(r).Error; err != nil {
				return err
			}
			if err := replaceDepts(tx, r.ID, s.deptIDs); err != nil {
				return err
			}
		}
		return nil
	})
}
