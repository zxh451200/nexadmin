package role

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/roleassign"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func tenantOf(ctx context.Context) idgen.ID {
	id, _ := tenant.From(ctx)
	return id
}

type service struct{ db *gorm.DB }

var codeRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

func bad(msg string) *response.Error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func notFound() *response.Error { return response.NotFound("error.not_found") }

// escapeLike 转义 LIKE 通配符，避免关键字里的 % _ 被当成通配。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

type listQuery struct {
	Page, PageSize int
	Keyword        string
	Status         string // 逗号分隔，可多选
}

// deptIDsOf 批量读取角色的自定义数据范围部门。
func (s *service) deptIDsOf(ctx context.Context, roleIDs []idgen.ID) (map[idgen.ID][]idgen.ID, error) {
	out := map[idgen.ID][]idgen.ID{}
	if len(roleIDs) == 0 {
		return out, nil
	}
	var rows []roleDept
	if err := s.db.WithContext(ctx).Where("role_id IN ?", roleIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RoleID] = append(out[r.RoleID], r.DeptID)
	}
	return out, nil
}

func (s *service) list(ctx context.Context, q listQuery) ([]*roleDTO, int64, error) {
	tx := s.db.WithContext(ctx).Model(&Role{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + escapeLike(strings.ToLower(kw)) + "%"
		tx = tx.Where("(LOWER(name) LIKE ? OR LOWER(code) LIKE ?)", like, like)
	}
	if q.Status != "" {
		tx = tx.Where("status IN ?", strings.Split(q.Status, ","))
	}
	tx = tx.Session(&gorm.Session{}) // 让查询可重复使用：先 Count 再 Find
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Role
	if err := tx.Order("sort ASC, id ASC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]idgen.ID, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	depts, err := s.deptIDsOf(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*roleDTO, len(rows))
	for i := range rows {
		out[i] = toDTO(&rows[i], depts[rows[i].ID])
	}
	return out, total, nil
}

// options 返回启用角色的下拉选项。op 为 nil 或超管时返回全部；否则只返回该操作者能分配的角色（三道校验，见 roleassign）。
func (s *service) options(ctx context.Context, op *roleassign.Operator) ([]option, error) {
	var rows []Role
	if err := s.db.WithContext(ctx).Where("status = ?", statusOn).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	var ok map[idgen.ID]bool
	if op != nil && !op.Super {
		loaded, err := roleassign.Load(ctx, s.db, tenantOf(ctx), nil)
		if err != nil {
			return nil, err
		}
		ok = make(map[idgen.ID]bool, len(loaded))
		for _, r := range loaded {
			ok[r.ID] = op.Check(r) == ""
		}
	}
	out := make([]option, 0, len(rows))
	for i := range rows {
		if ok != nil && !ok[rows[i].ID] {
			continue
		}
		out = append(out, option{ID: rows[i].ID, Name: rows[i].Name})
	}
	return out, nil
}

// find 按 ID 读取当前租户的角色。
func (s *service) find(ctx context.Context, id idgen.ID) (*Role, error) {
	var r Role
	if err := s.db.WithContext(ctx).First(&r, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	return &r, nil
}

func (s *service) get(ctx context.Context, id idgen.ID) (*roleDTO, error) {
	r, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	depts, err := s.deptIDsOf(ctx, []idgen.ID{id})
	if err != nil {
		return nil, err
	}
	return toDTO(r, depts[id]), nil
}

// check 校验并规整请求体，返回自定义数据范围的部门 ID（仅 dataScope=custom 时有值）。
func (in *roleInput) check() ([]idgen.ID, error) {
	in.Name, in.Code = strings.TrimSpace(in.Name), strings.TrimSpace(in.Code)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 64 {
		return nil, bad("角色名称不能为空且不超过 64 个字符")
	}
	if len(in.Code) < 1 || len(in.Code) > 64 || !codeRe.MatchString(in.Code) {
		return nil, bad("角色编码只能包含字母、数字、下划线、点、冒号和连字符，且不超过 64 位")
	}
	if utf8.RuneCountInString(in.Remark) > 500 {
		return nil, bad("备注不超过 500 个字符")
	}
	switch in.Status {
	case "":
		in.Status = statusOn
	case statusOn, statusOff:
	default:
		return nil, bad("状态不正确")
	}
	switch in.DataScope {
	case "":
		in.DataScope = scopeSelf
	case scopeAll, scopeCustom, scopeDept, scopeDeptAndChild, scopeSelf:
	default:
		return nil, bad("数据范围不正确")
	}
	if in.DataScope != scopeCustom {
		return nil, nil
	}
	seen := map[idgen.ID]bool{}
	var depts []idgen.ID
	for _, raw := range in.DeptIDs {
		id, err := idgen.Parse(raw)
		if err != nil {
			return nil, bad("部门 ID 不正确")
		}
		if !seen[id] {
			seen[id] = true
			depts = append(depts, id)
		}
	}
	return depts, nil
}

// codeTaken 判断当前租户内编码是否已被其他角色占用。
func (s *service) codeTaken(ctx context.Context, code string, exclude idgen.ID) (bool, error) {
	var n int64
	tx := s.db.WithContext(ctx).Model(&Role{}).Where("code = ?", code)
	if exclude != 0 {
		tx = tx.Where("id <> ?", int64(exclude))
	}
	err := tx.Count(&n).Error
	return n > 0, err
}

// replaceDepts 整体替换角色的自定义数据范围部门。
func replaceDepts(tx *gorm.DB, roleID idgen.ID, depts []idgen.ID) error {
	if err := tx.Where("role_id = ?", int64(roleID)).Delete(&roleDept{}).Error; err != nil {
		return err
	}
	if len(depts) == 0 {
		return nil
	}
	rows := make([]roleDept, len(depts))
	for i, d := range depts {
		rows[i] = roleDept{RoleID: roleID, DeptID: d}
	}
	return tx.Create(&rows).Error
}

// ensureRoleQuota 在新增 1 个角色前检查套餐角色数上限。没有额度限制（0 = 不限、默认租户、平台级操作）时不加锁也不统计；
// 有限制时必须在 tx 事务内调用：先对租户行 SELECT ... FOR UPDATE 再统计，使同一租户的并发创建被串行化、不会超出 maxRoles。
func ensureRoleQuota(ctx context.Context, tx *gorm.DB) error {
	info, err := tenant.QuotaInfo(ctx)
	if err != nil || info == nil || info.MaxRoles <= 0 {
		return err
	}
	if err := database.LockTenantRow(ctx, tx); err != nil {
		return err
	}
	var n int64
	if err := tx.WithContext(ctx).Model(&Role{}).Count(&n).Error; err != nil {
		return err
	}
	return tenant.CheckRoleQuota(ctx, n, 1)
}

func (s *service) create(ctx context.Context, in roleInput) (*roleDTO, error) {
	depts, err := in.check()
	if err != nil {
		return nil, err
	}
	if taken, err := s.codeTaken(ctx, in.Code, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, bad("角色编码已存在")
	}
	r := &Role{Name: in.Name, Code: in.Code, Sort: in.Sort, Status: in.Status, DataScope: in.DataScope, Remark: in.Remark,
		Assignable: in.Assignable == nil || *in.Assignable}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureRoleQuota(ctx, tx); err != nil {
			return err
		}
		if err := tx.Create(r).Error; err != nil {
			return err
		}
		return replaceDepts(tx, r.ID, depts)
	})
	if err != nil {
		return nil, err
	}
	event.Publish(ctx, event.RoleCreated, roleEvent(r))
	return toDTO(r, depts), nil
}

func (s *service) update(ctx context.Context, id idgen.ID, in roleInput) (*roleDTO, error) {
	depts, err := in.check()
	if err != nil {
		return nil, err
	}
	r, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Builtin && (in.Code != r.Code || in.Status != statusOn) {
		return nil, bad("内置角色不可修改编码或停用")
	}
	if taken, err := s.codeTaken(ctx, in.Code, id); err != nil {
		return nil, err
	} else if taken {
		return nil, bad("角色编码已存在")
	}
	r.Name, r.Code, r.Sort, r.Status, r.DataScope, r.Remark = in.Name, in.Code, in.Sort, in.Status, in.DataScope, in.Remark
	if in.Assignable != nil {
		r.Assignable = *in.Assignable
	}
	if r.Builtin {
		r.Assignable = false // 内置超管角色恒不可分配
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(r).Error; err != nil {
			return err
		}
		return replaceDepts(tx, id, depts)
	})
	if err != nil {
		return nil, err
	}
	event.Publish(ctx, event.RoleUpdated, roleEvent(r))
	return toDTO(r, depts), nil
}

// remove 删除角色并清理它的菜单授权、数据范围和用户关联；内置角色不可删除。
func (s *service) remove(ctx context.Context, id idgen.ID) error {
	r, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if r.Builtin {
		return bad("内置角色不可删除")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 仍有（未删除的）用户持有该角色时拒绝；sys_user_role 无租户列，角色已在上面按租户校验过归属。
		var n int64
		if err := tx.Table("sys_user_role AS ur").
			Joins("JOIN sys_user u ON u.id = ur.user_id AND u.deleted_at IS NULL").
			Where("ur.role_id = ?", int64(id)).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return response.Conflict("role.in_use", n)
		}
		if err := tx.Delete(&Role{}, "id = ?", int64(id)).Error; err != nil {
			return err
		}
		for _, table := range []string{"sys_role_menu", "sys_role_dept", "sys_user_role"} {
			if err := tx.Exec("DELETE FROM "+table+" WHERE role_id = ?", int64(id)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	event.Publish(ctx, event.RoleDeleted, roleEvent(r))
	return nil
}

// sort 按传入顺序重排角色（sort = 10, 20, 30 ...）。
func (s *service) sort(ctx context.Context, ids []string) error {
	parsed := make([]idgen.ID, 0, len(ids))
	for _, raw := range ids {
		id, err := idgen.Parse(raw)
		if err != nil {
			return bad("角色 ID 不正确")
		}
		parsed = append(parsed, id)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, id := range parsed {
			if err := tx.Model(&Role{}).Where("id = ?", int64(id)).Update("sort", (i+1)*10).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// menus 返回角色已分配的菜单/按钮 ID。
func (s *service) menus(ctx context.Context, id idgen.ID) ([]idgen.ID, error) {
	if _, err := s.find(ctx, id); err != nil {
		return nil, err
	}
	var raw []int64
	if err := s.db.WithContext(ctx).Model(&roleMenu{}).Where("role_id = ?", int64(id)).Pluck("menu_id", &raw).Error; err != nil {
		return nil, err
	}
	out := make([]idgen.ID, len(raw))
	for i, v := range raw {
		out[i] = idgen.ID(v)
	}
	return out, nil
}

// assignMenus 整体替换角色的菜单/按钮授权；不存在的菜单 ID 直接忽略。
func (s *service) assignMenus(ctx context.Context, id idgen.ID, menuIDs []string) error {
	if _, err := s.find(ctx, id); err != nil {
		return err
	}
	seen := map[int64]bool{}
	var want []int64
	for _, raw := range menuIDs {
		m, err := idgen.Parse(raw)
		if err != nil {
			return bad("菜单 ID 不正确")
		}
		if !seen[int64(m)] {
			seen[int64(m)] = true
			want = append(want, int64(m))
		}
	}
	var exist []int64
	if len(want) > 0 {
		if err := s.db.WithContext(ctx).Table("sys_menu").Where("id IN ?", want).Pluck("id", &exist).Error; err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", int64(id)).Delete(&roleMenu{}).Error; err != nil {
			return err
		}
		if len(exist) == 0 {
			return nil
		}
		rows := make([]roleMenu, len(exist))
		for i, m := range exist {
			rows[i] = roleMenu{RoleID: id, MenuID: idgen.ID(m)}
		}
		return tx.Create(&rows).Error
	})
}
