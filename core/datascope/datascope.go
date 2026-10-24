// Package datascope 解析当前操作者的数据范围（全部/本部门/本部门及以下/仅本人/自定义部门，多角色取并集），
// 供部门、公告、日志等按部门归属的模块做读写裁剪。用户模块有自己的 SQL 条件版本（scopeCond），语义保持一致。
package datascope

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Scope 是操作者可见的范围。nil 的 *Scope 表示“不裁剪”（只给内部调用方用，HTTP 入口必须先 Resolve）。
type Scope struct {
	All    bool     // 超管或任一启用角色的范围是“全部”
	UserID idgen.ID // 操作者自己，永远在范围内
	Depts  []int64  // 非 All 时可见的部门
	// Preset 是预设范围里最宽的一档（""/"self"/"dept"/"deptAndChild"，本部门类要求操作者有部门）；Custom 表示持有自定义范围角色。
	// 角色可分配校验用它们判断“预设范围的角色是否比操作者宽”，Depts 是它们合并后的具体部门集合。
	Preset string
	Custom bool
}

// Limited 报告是否需要裁剪。
func (s *Scope) Limited() bool { return s != nil && !s.All }

// HasDept 判断部门是否在范围内；All 恒为 true。
func (s *Scope) HasDept(id int64) bool {
	if !s.Limited() {
		return true
	}
	for _, d := range s.Depts {
		if d == id {
			return true
		}
	}
	return false
}

// Resolve 读取操作者的启用角色并合成范围。超管直接返回 All，不产生查询。
// “本部门及以下”依赖 sys_dept.path 物化路径；取不到时退化为“本部门”。
func Resolve(ctx context.Context, db *gorm.DB, userID idgen.ID, super bool) (*Scope, error) {
	if super {
		return &Scope{All: true, UserID: userID}, nil
	}
	db = db.WithContext(ctx)
	var roles []struct {
		ID        idgen.ID
		DataScope string
	}
	err := db.Table("sys_user_role AS ur").Select("r.id AS id, r.data_scope AS data_scope").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Where("ur.user_id = ?", int64(userID)).Scan(&roles).Error
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.DataScope == "all" {
			return &Scope{All: true, UserID: userID}, nil
		}
	}
	var self struct{ DeptID idgen.ID }
	if err := db.Table("sys_user").Select("dept_id").Where("id = ? AND deleted_at IS NULL", int64(userID)).Scan(&self).Error; err != nil {
		return nil, err
	}
	sc := &Scope{UserID: userID}
	seen := map[int64]bool{}
	add := func(ids ...int64) {
		for _, id := range ids {
			if id != 0 && !seen[id] {
				seen[id] = true
				sc.Depts = append(sc.Depts, id)
			}
		}
	}
	var custom []int64
	for _, r := range roles {
		switch r.DataScope {
		case "self":
			sc.Preset = widerPreset(sc.Preset, "self")
		case "dept":
			if self.DeptID != 0 {
				sc.Preset = widerPreset(sc.Preset, "dept")
			}
			add(int64(self.DeptID))
		case "deptAndChild":
			if self.DeptID == 0 {
				continue
			}
			sc.Preset = widerPreset(sc.Preset, "deptAndChild")
			var path string
			var sub []int64
			if e := db.Table("sys_dept").Select("path").Where("id = ?", int64(self.DeptID)).Scan(&path).Error; e == nil && path != "" {
				like := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(path) + "%"
				if e := db.Table("sys_dept").Where("path LIKE ?", like).Pluck("id", &sub).Error; e == nil {
					add(sub...)
					continue
				}
			}
			add(int64(self.DeptID))
		case "custom":
			custom = append(custom, int64(r.ID))
			sc.Custom = true
		}
	}
	if len(custom) > 0 {
		var ds []int64
		if err := db.Table("sys_role_dept").Where("role_id IN ?", custom).Pluck("dept_id", &ds).Error; err != nil {
			return nil, err
		}
		add(ds...)
	}
	return sc, nil
}

func presetRank(p string) int {
	switch p {
	case "deptAndChild":
		return 3
	case "dept":
		return 2
	case "self":
		return 1
	}
	return 0
}

func widerPreset(a, b string) string {
	if presetRank(b) > presetRank(a) {
		return b
	}
	return a
}

// UserIDCond 生成“按用户 id 列归属”的条件，如创建者列 created_by。无需裁剪时返回 ("", nil, false)。
func (s *Scope) UserIDCond(col string) (string, []any, bool) {
	if !s.Limited() {
		return "", nil, false
	}
	if len(s.Depts) == 0 {
		return col + " = ?", []any{int64(s.UserID)}, true
	}
	return "(" + col + " = ? OR " + col + " IN (SELECT id FROM sys_user WHERE dept_id IN ?))", []any{int64(s.UserID), s.Depts}, true
}

// UsernameCond 生成“按账号列归属”的条件（日志表只记账号，没有用户 id），如 operator、username。
// 账号在租户内唯一；已删除用户或改名后的历史记录不再归属于任何部门，对受限操作者不可见。
func (s *Scope) UsernameCond(col string) (string, []any, bool) {
	if !s.Limited() {
		return "", nil, false
	}
	if len(s.Depts) == 0 {
		return col + " IN (SELECT username FROM sys_user WHERE id = ? AND deleted_at IS NULL)", []any{int64(s.UserID)}, true
	}
	return col + " IN (SELECT username FROM sys_user WHERE deleted_at IS NULL AND (id = ? OR dept_id IN ?))", []any{int64(s.UserID), s.Depts}, true
}

// Forbidden 是统一的越权错误：受限操作者对范围外与不存在的目标得到同样的 403，防止枚举。
// n>1 时只报数量，不指明具体 id。
func Forbidden(what string, n int) *response.Error {
	msg := "无权操作该" + what + "（超出数据范围）"
	if n > 1 {
		msg = fmt.Sprintf("所选%s中有 %d 个超出数据范围或不存在，已整批拒绝", what, n)
	}
	return response.Forbidden("error.forbidden").WithMsg(msg)
}

// FromClaims 按令牌解析范围。无令牌 -> 401；开放平台“应用”主体没有用户身份，权限已由其授权的权限点限定，按全部处理；
// 个人访问令牌（PAT）按其所属用户解析。
func FromClaims(ctx context.Context, db *gorm.DB, cl *auth.Claims) (*Scope, error) {
	if cl == nil {
		return nil, response.Unauthorized("error.unauthorized")
	}
	if cl.Principal == auth.PrincipalApp {
		return &Scope{All: true}, nil
	}
	return Resolve(ctx, db, cl.UserID, cl.Super)
}
