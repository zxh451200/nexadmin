package menu

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/core/plugin"
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

func bad(key string, args ...any) *response.Error { return response.BadRequest(key, args...) }

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

// pluginMenuVisible 报告插件菜单是否该出现在侧边栏。默认与 /plugins/active 同源，测试可替换。
var pluginMenuVisible = func(id string) bool { return plugin.Default().MenuVisible(id) }

// pluginOfComponent 从 "plugins/<id>/XxxPage" 提取插件 id。
func pluginOfComponent(component string) (string, bool) {
	rest, ok := strings.CutPrefix(component, "plugins/")
	if !ok {
		return "", false
	}
	id, _, ok := strings.Cut(rest, "/")
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// mineKeep 是侧边栏的保留条件：启用的目录/菜单、持有权限码、且所属插件当前可挂载。
// 插件菜单按 component 归属判断，目录节点没有 component，由 pruneDir 剔除空目录。
func mineKeep(perms []string) func(*Menu) bool {
	return func(m *Menu) bool {
		if m.Status != statusOn || m.Type == typeButton {
			return false
		}
		if id, ok := pluginOfComponent(m.Component); ok && !pluginMenuVisible(id) {
			return false
		}
		return m.Type == typeDir || m.Perm == "" || auth.HasPerm(perms, m.Perm)
	}
}

// mine 返回侧边栏菜单：只含启用的目录和菜单，菜单需持有其权限码，空目录自动剔除。
func (s *service) mine(ctx context.Context, perms []string) ([]*menuDTO, error) {
	rows, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	return buildTree(rows, mineKeep(perms), true), nil
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
		return 0, bad("menu.type_invalid")
	}
	if in.Title == "" || tooLong(in.Title, 100) {
		return 0, bad("menu.title_invalid")
	}
	if tooLong(in.Icon, 64) || tooLong(in.Perm, 100) || tooLong(in.Path, 255) || tooLong(in.Component, 255) {
		return 0, bad("menu.field_too_long")
	}
	switch in.Status {
	case "":
		in.Status = statusOn
	case "0", "1":
	default:
		return 0, bad("menu.status_invalid")
	}
	if strings.TrimSpace(in.ParentID) == "" {
		return 0, checkButtonParent(in.Type, 0)
	}
	pid, err := idgen.Parse(in.ParentID)
	if err != nil {
		return 0, bad("menu.parent_invalid")
	}
	return pid, nil
}

// checkButtonParent 按钮是权限点，离开父菜单就成了孤儿行：既进不了侧栏，
// 也没法在角色授权树里定位到它属于哪个功能。新增、修改和拖拽共用这条判定。
func checkButtonParent(typ string, pid idgen.ID) error {
	if typ == typeButton && pid == 0 {
		return bad("menu.button_needs_parent")
	}
	return nil
}

func (in *menuInput) apply(m *Menu, pid idgen.ID) {
	m.ParentID, m.Type, m.Title, m.Icon = pid, in.Type, in.Title, in.Icon
	m.Path, m.Component, m.Perm, m.Sort = in.Path, in.Component, in.Perm, in.Sort
	m.KeepAlive, m.Hidden, m.External, m.Status = in.KeepAlive, in.Hidden, in.External, in.Status
}

// checkParent 上级必须存在且不能是按钮；可见的目录/菜单不能挂到菜单下。
// 侧栏只渲染「目录 → 菜单」两层，挂在菜单下的目录/菜单永远不可见（历史上导入中心就被
// 拖进导出中心菜单下、入口凭空消失）。hidden 行例外：出厂把权限点行（sso.list、maps:config）
// 挂在设置菜单下、只藏不入侧栏，这类行要保持可编辑。新增、修改和拖拽共用这条判定。
func (s *service) checkParent(ctx context.Context, childType string, childHidden bool, pid idgen.ID) error {
	if pid == 0 {
		return nil
	}
	p, err := s.get(ctx, pid)
	if err != nil {
		var re *response.Error
		if errors.As(err, &re) {
			return bad("menu.parent_not_found")
		}
		return err
	}
	if p.Type == typeButton {
		return bad("menu.child_under_button")
	}
	if childType != typeButton && p.Type == typeMenu && !childHidden {
		return bad("menu.child_under_menu")
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
	if err := s.checkParent(ctx, in.Type, in.Hidden, pid); err != nil {
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
			return nil, bad("menu.parent_self_or_child")
		}
		if err := s.checkParent(ctx, in.Type, in.Hidden, pid); err != nil {
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

// subtreeUnion 把每个选中节点连同它的全部下级求并集（去重、升序），口径与单条 remove 完全一致：
// 勾一个节点就是删它和它的全部后代。库里不存在的 ID 跳过（单条删除对不存在的菜单本就「视为成功」）；
// 全部不存在时返回空集，调用方据此连 DELETE 都不打——空集合喂给 `id IN ?` 绝不能退化成全表操作。
// 纯函数：不碰数据库，「选中集 = 被删集合」这条不变式可以在这里表驱动测死。
func subtreeUnion(rows []Menu, roots []idgen.ID) []int64 {
	exists := make(map[idgen.ID]bool, len(rows))
	for i := range rows {
		exists[rows[i].ID] = true
	}
	set := make(map[idgen.ID]bool, len(roots))
	for _, id := range roots {
		if !exists[id] {
			continue
		}
		set[id] = true
		for d := range descendants(rows, id) {
			set[d] = true
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, int64(id))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// removeMany 批量删除菜单：每项连同全部下级求并集后删掉，并在同一事务里清掉角色对这些菜单的授权，
// 返回实际删除条数（取 DELETE 的 RowsAffected，而不是选择集长度，确认框与返回值才不骗人）。
// 与单条 remove 共用 mutateTree 的锁序：校验与写入同事务，任一环节失败整体回滚，
// 不会留下「菜单删了半棵、授权还剩孤儿行」的中间态。
func (s *service) removeMany(ctx context.Context, ids []idgen.ID) (int64, error) {
	if len(ids) == 0 {
		return 0, bad("error.bad_request")
	}
	var n int64
	err := s.mutateTree(ctx, func(locked *service) error {
		rows, err := locked.list(ctx)
		if err != nil {
			return err
		}
		list := subtreeUnion(rows, ids)
		if len(list) == 0 {
			return nil
		}
		db := locked.db.WithContext(ctx)
		res := db.Where("id IN ?", list).Delete(&Menu{})
		if res.Error != nil {
			return res.Error
		}
		n = res.RowsAffected
		return db.Exec("DELETE FROM sys_role_menu WHERE menu_id IN ?", list).Error
	})
	if err != nil {
		return 0, err
	}
	return n, nil
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
			return bad("menu.parent_invalid")
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
			return response.Conflict("error.stale_list")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) != len(group) {
		return response.Conflict("error.stale_list")
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
			return bad("menu.parent_invalid")
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
	if err := checkButtonParent(current.Type, pid); err != nil {
		return err
	}
	if pid == id || descendants(rows, id)[pid] {
		return bad("menu.parent_self_or_child")
	}
	if pid != 0 {
		parent := byID[pid]
		if parent == nil {
			return bad("menu.parent_not_found")
		}
		if parent.Type == typeButton {
			return bad("menu.child_under_button")
		}
		// 可见的目录/菜单不能拖进菜单下（判定与 checkParent 同款，见其注释）
		if current.Type != typeButton && parent.Type == typeMenu && !current.Hidden {
			return bad("menu.child_under_menu")
		}
	}
	if in.AfterID != nil {
		// 插到指定同级之后：先算出目标父级的完整顺序，再整体重编号，
		// 否则插入点两侧的 sort 只能靠调值硬挤，连续拖几次就会重复。
		var sibs []Menu
		for _, row := range rows {
			if row.ID != id && row.ParentID == pid {
				sibs = append(sibs, row)
			}
		}
		sort.Slice(sibs, func(i, j int) bool {
			if sibs[i].Sort != sibs[j].Sort {
				return sibs[i].Sort < sibs[j].Sort
			}
			return sibs[i].ID < sibs[j].ID
		})
		order := placeAfter(sibs, id, strings.TrimSpace(*in.AfterID))
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for i, sid := range order {
				upd := map[string]any{"sort": (i + 1) * 10}
				if sid == id {
					upd["parent_id"] = int64(pid)
				}
				if err := tx.Model(&Menu{}).Where("id = ?", int64(sid)).Updates(upd).Error; err != nil {
					return err
				}
			}
			return nil
		})
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

// placeAfter 把 id 插到 afterID 之后（afterID 为空=插到最前；找不到该同级=追加到末尾），
// 返回目标父级下的完整顺序（含 id）。siblings 不含 id，且已按 sort 升序。
func placeAfter(siblings []Menu, id idgen.ID, afterID string) []idgen.ID {
	out := make([]idgen.ID, 0, len(siblings)+1)
	placed := afterID == ""
	if placed {
		out = append(out, id)
	}
	for _, s := range siblings {
		out = append(out, s.ID)
		if s.ID.String() == afterID {
			out = append(out, id)
			placed = true
		}
	}
	if !placed {
		out = append(out, id)
	}
	return out
}
