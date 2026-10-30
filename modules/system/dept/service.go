package dept

import (
	"context"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 封装部门的数据访问与业务规则。
type Service struct{ db *gorm.DB }

// NewService 创建服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func bad(key string, args ...any) error { return response.BadRequest(key, args...) }
func notFound() error                   { return response.NotFound("error.not_found") }
func isNotFound(err error) bool         { return errors.Is(err, gorm.ErrRecordNotFound) }

// nz 把 0 值 ID 视为未填（前端可能提交 "" 或 null）。
func nz(p *idgen.ID) *idgen.ID {
	if p == nil || *p == 0 {
		return nil
	}
	return p
}

func samePtr(a, b *idgen.ID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// rebasePath 把子孙节点的 path 从旧前缀换成新前缀（移动节点时重写子树）。
func rebasePath(path, oldPrefix, newPrefix string) string {
	return newPrefix + strings.TrimPrefix(path, oldPrefix)
}

func get(tx *gorm.DB, id idgen.ID) (*Dept, error) {
	var d Dept
	if err := tx.Where("id = ?", int64(id)).First(&d).Error; err != nil {
		if isNotFound(err) {
			return nil, notFound()
		}
		return nil, err
	}
	return &d, nil
}

// Get 读取单个部门。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*Dept, error) {
	return get(s.db.WithContext(ctx), id)
}

// Tree 返回完整部门树，同级按 sort、id 排序。
func (s *Service) Tree(ctx context.Context) ([]*Dept, error) {
	var list []*Dept
	if err := s.db.WithContext(ctx).Order("sort, id").Find(&list).Error; err != nil {
		return nil, err
	}
	return buildTree(list), nil
}

// buildTree 把已排序的平铺列表组装成树；列表里缺失父节点的节点不会出现在结果中。
func buildTree(list []*Dept) []*Dept {
	byParent := map[idgen.ID][]*Dept{}
	for _, d := range list {
		var pid idgen.ID
		if d.ParentID != nil {
			pid = *d.ParentID
		}
		byParent[pid] = append(byParent[pid], d)
	}
	var fill func(pid idgen.ID) []*Dept
	fill = func(pid idgen.ID) []*Dept {
		nodes := byParent[pid]
		for _, n := range nodes {
			n.Children = fill(n.ID)
		}
		return nodes
	}
	out := fill(0)
	if out == nil {
		out = []*Dept{}
	}
	return out
}

// Create 新增部门，path = 上级 path + 自身 ID + "/"。
func (s *Service) Create(ctx context.Context, in Input) (*Dept, error) {
	db := s.db.WithContext(ctx)
	parentPath := "/"
	parent := nz(in.ParentID)
	if parent != nil {
		p, err := get(db, *parent)
		if err != nil {
			if isNotFoundErr(err) {
				return nil, bad("dept.parent_not_found")
			}
			return nil, err
		}
		parentPath = p.Path
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	d := &Dept{
		Name: in.Name, ParentID: parent, Path: parentPath + id.String() + "/", Sort: in.Sort,
		LeaderID: nz(in.LeaderID), Phone: in.Phone, Email: in.Email, Status: statusOr(in.Status),
	}
	d.ID = id
	if err := db.Create(d).Error; err != nil {
		return nil, err
	}
	event.Publish(ctx, event.DeptCreated, deptEvent(d))
	return d, nil
}

func isNotFoundErr(err error) bool {
	var e *response.Error
	return errors.As(err, &e) && e.Status == 404
}

func statusOr(s string) string {
	if s == "" {
		return "1"
	}
	return s
}

// Update 修改部门；上级变化时在同一事务里重写整棵子树的 path。
func (s *Service) Update(ctx context.Context, id idgen.ID, in Input) (*Dept, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := get(tx, id)
		if err != nil {
			return err
		}
		parent := nz(in.ParentID)
		if !samePtr(d.ParentID, parent) {
			parentPath := "/"
			if parent != nil {
				if *parent == id {
					return bad("dept.parent_self_or_child")
				}
				p, err := get(tx, *parent)
				if err != nil {
					if isNotFoundErr(err) {
						return bad("dept.parent_not_found")
					}
					return err
				}
				if strings.HasPrefix(p.Path, d.Path) { // 新上级在自己的子树里
					return bad("dept.parent_self_or_child")
				}
				parentPath = p.Path
			}
			newPath := parentPath + id.String() + "/"
			var subs []Dept
			if err := tx.Where("path LIKE ? AND id <> ?", d.Path+"%", int64(id)).Find(&subs).Error; err != nil {
				return err
			}
			for _, sub := range subs {
				np := rebasePath(sub.Path, d.Path, newPath)
				if err := tx.Model(&Dept{}).Where("id = ?", int64(sub.ID)).Update("path", np).Error; err != nil {
					return err
				}
			}
			d.Path = newPath
		}
		leader := nz(in.LeaderID)
		leaderName := d.LeaderName
		if !samePtr(d.LeaderID, leader) {
			leaderName = "" // 负责人变更后旧名称作废
		}
		return tx.Model(&Dept{}).Where("id = ?", int64(id)).Updates(map[string]any{
			"name": in.Name, "parent_id": parent, "path": d.Path, "sort": in.Sort,
			"leader_id": leader, "leader_name": leaderName, "phone": in.Phone, "email": in.Email,
			"status": statusOr(in.Status),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	event.Publish(ctx, event.DeptUpdated, deptEvent(d))
	return d, nil
}

// Delete 级联删除本部门及全部下级。
func (s *Service) Delete(ctx context.Context, id idgen.ID) error {
	var gone []Dept // 被级联删除的整棵子树，用于逐个发 dept.deleted
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := get(tx, id)
		if err != nil {
			return err
		}
		// 级联删除整棵子树（设计文档 §12），但子树里任何一个部门仍有（未删除的）用户时整体拒绝，
		// 避免一次删除让用户悄悄失去部门归属。
		var users int64
		sub := tx.Model(&Dept{}).Select("id").Where("path LIKE ?", d.Path+"%")
		if err := tx.Table("sys_user").Where("deleted_at IS NULL AND dept_id IN (?)", sub).Count(&users).Error; err != nil {
			return err
		}
		if users > 0 {
			return response.Conflict("dept.has_users", users)
		}
		if err := tx.Where("path LIKE ?", d.Path+"%").Find(&gone).Error; err != nil {
			return err
		}
		return tx.Where("path LIKE ?", d.Path+"%").Delete(&Dept{}).Error
	})
	if err != nil {
		return err
	}
	for i := range gone {
		event.Publish(ctx, event.DeptDeleted, deptEvent(&gone[i]))
	}
	return nil
}

// Sort 调整同级顺序：ids 必须恰好是该父节点下的全部子部门，否则 409。
func (s *Service) Sort(ctx context.Context, in SortInput) error {
	parent := nz(in.ParentID)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&Dept{})
		if parent == nil {
			q = q.Where("parent_id IS NULL")
		} else {
			q = q.Where("parent_id = ?", int64(*parent))
		}
		var have []int64
		if err := q.Pluck("id", &have).Error; err != nil {
			return err
		}
		set := make(map[int64]bool, len(have))
		for _, h := range have {
			set[h] = true
		}
		seen := map[int64]bool{}
		for _, id := range in.IDs {
			if !set[int64(id)] || seen[int64(id)] {
				return response.Conflict("error.stale_list")
			}
			seen[int64(id)] = true
		}
		if len(seen) != len(have) {
			return response.Conflict("error.stale_list")
		}
		for i, id := range in.IDs {
			if err := tx.Model(&Dept{}).Where("id = ?", int64(id)).Update("sort", (i+1)*10).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Move 把部门移到另一个上级下（拖拽跨层级），并按 afterId 落到目标同级的精确位置。
// 上级变化时在同一事务里重写整棵子树的 path；插入点用整组重编号，避免连续拖拽后 sort 重复。
func (s *Service) Move(ctx context.Context, id idgen.ID, in MoveInput) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		d, err := get(tx, id)
		if err != nil {
			return err
		}
		parent := nz(in.ParentID)
		parentPath := "/"
		if parent != nil {
			if *parent == id {
				return bad("dept.parent_self_or_child")
			}
			p, err := get(tx, *parent)
			if err != nil {
				if isNotFoundErr(err) {
					return bad("dept.parent_not_found")
				}
				return err
			}
			if strings.HasPrefix(p.Path, d.Path) { // 新上级在自己的子树里
				return bad("dept.parent_self_or_child")
			}
			parentPath = p.Path
		}
		newPath := parentPath + id.String() + "/"
		if newPath != d.Path {
			var subs []Dept
			if err := tx.Where("path LIKE ? AND id <> ?", d.Path+"%", int64(id)).Find(&subs).Error; err != nil {
				return err
			}
			for _, sub := range subs {
				np := rebasePath(sub.Path, d.Path, newPath)
				if err := tx.Model(&Dept{}).Where("id = ?", int64(sub.ID)).Update("path", np).Error; err != nil {
					return err
				}
			}
		}
		sibQuery := func() *gorm.DB {
			q := tx.Model(&Dept{}).Where("id <> ?", int64(id))
			if parent == nil {
				return q.Where("parent_id IS NULL")
			}
			return q.Where("parent_id = ?", int64(*parent))
		}
		var sibs []Dept
		if err := sibQuery().Find(&sibs).Error; err != nil {
			return err
		}
		sort.Slice(sibs, func(i, j int) bool {
			if sibs[i].Sort != sibs[j].Sort {
				return sibs[i].Sort < sibs[j].Sort
			}
			return sibs[i].ID < sibs[j].ID
		})
		moved := map[string]any{"path": newPath}
		if parent == nil {
			moved["parent_id"] = nil
		} else {
			moved["parent_id"] = int64(*parent)
		}
		if in.AfterID == nil {
			maxSort := 0
			for _, s := range sibs {
				if s.Sort > maxSort {
					maxSort = s.Sort
				}
			}
			moved["sort"] = maxSort + 10
			return tx.Model(&Dept{}).Where("id = ?", int64(id)).Updates(moved).Error
		}
		for i, sid := range placeAfter(sibs, id, strings.TrimSpace(*in.AfterID)) {
			upd := map[string]any{"sort": (i + 1) * 10}
			if sid == id {
				for k, v := range moved {
					upd[k] = v
				}
			}
			if err := tx.Model(&Dept{}).Where("id = ?", int64(sid)).Updates(upd).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	event.Publish(ctx, event.DeptUpdated, deptEvent(d))
	return nil
}

// placeAfter 把 id 插到 afterID 之后（afterID 为空=插到最前；找不到该同级=追加到末尾），
// 返回目标上级下的完整顺序（含 id）。siblings 不含 id，且已按 sort 升序。
func placeAfter(siblings []Dept, id idgen.ID, afterID string) []idgen.ID {
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
