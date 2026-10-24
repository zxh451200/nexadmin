package menu

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

type service struct{ db *gorm.DB }

// Tree mutations share one lock order across instances. Validation and writes
// use the same transaction so concurrent moves cannot create a cycle.
func (s *service) mutateTree(ctx context.Context, fn func(*service) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor Menu
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").First(&anchor).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return fn(&service{db: tx})
	})
}

func bad(msg string) *response.Error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func notFound() *response.Error { return response.NotFound("error.not_found") }

// list 按 sort、id 升序读出全部菜单（菜单量小，树在内存里组装）。
func (s *service) list(ctx context.Context) ([]Menu, error) {
	var rows []Menu
	err := s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// buildTree 组装菜单树。keep 决定节点是否保留；pruneDir 为 true 时丢弃没有任何可见子节点的目录。
func buildTree(rows []Menu, keep func(*Menu) bool, pruneDir bool) []*menuDTO {
	byParent := map[idgen.ID][]*Menu{}
	for i := range rows {
		byParent[rows[i].ParentID] = append(byParent[rows[i].ParentID], &rows[i])
	}
	var walk func(pid idgen.ID) []*menuDTO
	walk = func(pid idgen.ID) []*menuDTO {
		var out []*menuDTO
		for _, m := range byParent[pid] {
			if !keep(m) {
				continue
			}
			d := toDTO(m)
			d.Children = walk(m.ID)
			if pruneDir && m.Type == typeDir && len(d.Children) == 0 {
				continue
			}
			out = append(out, d)
		}
		return out
	}
	out := walk(0)
	if out == nil {
		out = []*menuDTO{}
	}
	return out
}

// tree 返回完整菜单树（含按钮、含停用项），供菜单管理与角色分配使用。
func (s *service) tree(ctx context.Context) ([]*menuDTO, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	return buildTree(rows, func(*Menu) bool { return true }, false), nil
}

// mine 返回侧边栏菜单：只含启用的目录和菜单，菜单需持有其权限码，空目录自动剔除。
func (s *service) mine(ctx context.Context, perms []string) ([]*menuDTO, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	keep := func(m *Menu) bool {
		if m.Status != statusOn || m.Type == typeButton {
			return false
		}
		return m.Type == typeDir || m.Perm == "" || auth.HasPerm(perms, m.Perm)
	}
	return buildTree(rows, keep, true), nil
}

func (s *service) get(ctx context.Context, id idgen.ID) (*Menu, error) {
	var m Menu
	if err := s.db.WithContext(ctx).First(&m, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	return &m, nil
}

// descendants 返回 id 的全部下级 ID（不含自己）。
func descendants(rows []Menu, id idgen.ID) map[idgen.ID]bool {
	out := map[idgen.ID]bool{}
	var walk func(pid idgen.ID)
	walk = func(pid idgen.ID) {
		for i := range rows {
			if rows[i].ParentID == pid && !out[rows[i].ID] {
				out[rows[i].ID] = true
				walk(rows[i].ID)
			}
		}
	}
	walk(id)
	return out
}

func tooLong(s string, n int) bool { return utf8.RuneCountInString(s) > n }

// check 校验并规整请求体，返回解析后的父节点 ID。
func (in *menuInput) check() (idgen.ID, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Type != typeDir && in.Type != typeMenu && in.Type != typeButton {
		return 0, bad("菜单类型不正确")
	}
	if in.Title == "" || tooLong(in.Title, 100) {
		return 0, bad("菜单标题不能为空且不超过 100 个字符")
	}
	if tooLong(in.Icon, 64) || tooLong(in.Perm, 100) || tooLong(in.Path, 255) || tooLong(in.Component, 255) {
		return 0, bad("字段过长")
	}
	switch in.Status {
	case "":
		in.Status = statusOn
	case "0", "1":
	default:
		return 0, bad("状态不正确")
	}
	if strings.TrimSpace(in.ParentID) == "" {
		return 0, nil
	}
	pid, err := idgen.Parse(in.ParentID)
	if err != nil {
		return 0, bad("上级菜单不正确")
	}
	return pid, nil
}

func (in *menuInput) apply(m *Menu, pid idgen.ID) {
	m.ParentID, m.Type, m.Title, m.Icon = pid, in.Type, in.Title, in.Icon
	m.Path, m.Component, m.Perm, m.Sort = in.Path, in.Component, in.Perm, in.Sort
	m.KeepAlive, m.Hidden, m.External, m.Status = in.KeepAlive, in.Hidden, in.External, in.Status
}

// checkParent 上级必须存在且不能是按钮。
func (s *service) checkParent(ctx context.Context, pid idgen.ID) error {
	if pid == 0 {
		return nil
	}
	p, err := s.get(ctx, pid)
	if err != nil {
		var re *response.Error
		if errors.As(err, &re) {
			return bad("上级菜单不存在")
		}
		return err
	}
	if p.Type == typeButton {
		return bad("按钮下不能再建子菜单")
	}
	return nil
}

func (s *service) create(ctx context.Context, in menuInput) (*Menu, error) {
	var row *Menu
	err := s.mutateTree(ctx, func(locked *service) error {
		var err error
		row, err = locked.createLocked(ctx, in)
		return err
	})
	return row, err
}

func (s *service) createLocked(ctx context.Context, in menuInput) (*Menu, error) {
	pid, err := in.check()
	if err != nil {
		return nil, err
	}
	if err := s.checkParent(ctx, pid); err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	m := &Menu{PlatformModel: database.PlatformModel{ID: id}, Code: "custom:" + id.String()}
	in.apply(m, pid)
	if err := s.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

func (s *service) update(ctx context.Context, id idgen.ID, in menuInput) (*Menu, error) {
	var row *Menu
	err := s.mutateTree(ctx, func(locked *service) error {
		var err error
		row, err = locked.updateLocked(ctx, id, in)
		return err
	})
	return row, err
}

func (s *service) updateLocked(ctx context.Context, id idgen.ID, in menuInput) (*Menu, error) {
	pid, err := in.check()
	if err != nil {
		return nil, err
	}
	m, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if pid != 0 {
		rows, err := s.list(ctx)
		if err != nil {
			return nil, err
		}
		if pid == id || descendants(rows, id)[pid] {
			return nil, bad("上级菜单不能是自己或其下级")
		}
		if err := s.checkParent(ctx, pid); err != nil {
			return nil, err
		}
	}
	in.apply(m, pid)
	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

// remove 级联删除菜单及其全部下级，并清掉角色对它们的授权。菜单不存在时视为成功。
func (s *service) remove(ctx context.Context, id idgen.ID) error {
	return s.mutateTree(ctx, func(locked *service) error { return locked.removeLocked(ctx, id) })
}

func (s *service) removeLocked(ctx context.Context, id idgen.ID) error {
	rows, err := s.list(ctx)
	if err != nil {
		return err
	}
	ids := []int64{int64(id)}
	for d := range descendants(rows, id) {
		ids = append(ids, int64(d))
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id IN ?", ids).Delete(&Menu{}).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM sys_role_menu WHERE menu_id IN ?", ids).Error
	})
}

// sort 重排同一父节点下的菜单；传入的 ID 集合必须与当前该层完全一致，否则 409 提示刷新。
func (s *service) sort(ctx context.Context, in sortInput) error {
	return s.mutateTree(ctx, func(locked *service) error { return locked.sortLocked(ctx, in) })
}

func (s *service) sortLocked(ctx context.Context, in sortInput) error {
	var pid idgen.ID
	if strings.TrimSpace(in.ParentID) != "" {
		var err error
		if pid, err = idgen.Parse(in.ParentID); err != nil {
			return bad("上级菜单不正确")
		}
	}
	rows, err := s.list(ctx)
	if err != nil {
		return err
	}
	group := map[idgen.ID]bool{}
	for i := range rows {
		if rows[i].ParentID == pid {
			group[rows[i].ID] = true
		}
	}
	ids := make([]idgen.ID, 0, len(in.IDs))
	seen := map[idgen.ID]bool{}
	for _, raw := range in.IDs {
		id, err := idgen.Parse(raw)
		if err != nil || !group[id] || seen[id] {
			return response.Conflict("error.bad_request").WithMsg("列表已变化，请刷新")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) != len(group) {
		return response.Conflict("error.bad_request").WithMsg("列表已变化，请刷新")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := tx.Model(&Menu{}).Where("id = ?", int64(id)).Update("sort", (i+1)*10).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// move 把菜单移动到另一个父节点下，并追加到目标节点的末尾。
func (s *service) move(ctx context.Context, id idgen.ID, in moveInput) error {
	return s.mutateTree(ctx, func(locked *service) error { return locked.moveLocked(ctx, id, in) })
}

func (s *service) moveLocked(ctx context.Context, id idgen.ID, in moveInput) error {
	var pid idgen.ID
	if strings.TrimSpace(in.ParentID) != "" {
		var err error
		if pid, err = idgen.Parse(in.ParentID); err != nil {
			return bad("上级菜单不正确")
		}
	}
	rows, err := s.list(ctx)
	if err != nil {
		return err
	}
	var current *Menu
	byID := make(map[idgen.ID]*Menu, len(rows))
	for i := range rows {
		byID[rows[i].ID] = &rows[i]
		if rows[i].ID == id {
			current = &rows[i]
		}
	}
	if current == nil {
		return notFound()
	}
	if pid == id || descendants(rows, id)[pid] {
		return bad("上级菜单不能是自己或其下级")
	}
	if pid != 0 {
		parent := byID[pid]
		if parent == nil {
			return bad("上级菜单不存在")
		}
		if parent.Type == typeButton {
			return bad("按钮下不能再建子菜单")
		}
	}
	maxSort := 0
	for _, row := range rows {
		if row.ID != id && row.ParentID == pid && row.Sort > maxSort {
			maxSort = row.Sort
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&Menu{}).Where("id = ?", int64(id)).Updates(map[string]any{"parent_id": int64(pid), "sort": maxSort + 10}).Error
	})
}
