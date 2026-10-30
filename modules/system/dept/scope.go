package dept

import (
	"context"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// 部门写操作的数据范围校验：超管/范围“全部”不受影响；受限操作者只能在自己范围内的部门上操作，
// 范围外与不存在的部门得到同样的 403（防枚举）。读取（/depts/tree、/depts/:id）默认不裁剪：
// 用户表单、用户筛选与角色“自定义数据范围”的部门下拉都依赖完整的部门树；只有部门管理页带 scene=manage 时才裁剪
// （见 TreeScoped/GetScoped）。

func forbidden(ctx context.Context, n int) error {
	return datascope.Forbidden(ctx, "datascope.resource.dept", n)
}

// subtreeIDs 返回 id 及其全部下级部门；id 不存在时返回 nil。
func (s *Service) subtreeIDs(ctx context.Context, id idgen.ID) ([]int64, error) {
	db := s.db.WithContext(ctx)
	d, err := get(db, id)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []int64
	if err := db.Model(&Dept{}).Where("path LIKE ?", d.Path+"%").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// parentOK 新建/移动时目标父部门必须在范围内；受限操作者不能建/移到根。
func parentOK(sc *datascope.Scope, parent *idgen.ID) bool {
	return parent != nil && sc.HasDept(int64(*parent))
}

// CheckCreate 校验在 parent 下新建部门。
func (s *Service) CheckCreate(ctx context.Context, sc *datascope.Scope, in Input) error {
	if !sc.Limited() {
		return nil
	}
	if !parentOK(sc, nz(in.ParentID)) {
		return forbidden(ctx, 1)
	}
	return nil
}

// CheckUpdate 校验修改 id；上级变化（移动）时，新上级必须在范围内，且被移动的整棵子树都在范围内。
func (s *Service) CheckUpdate(ctx context.Context, sc *datascope.Scope, id idgen.ID, in Input) error {
	if !sc.Limited() {
		return nil
	}
	if !sc.HasDept(int64(id)) {
		return forbidden(ctx, 1)
	}
	d, err := get(s.db.WithContext(ctx), id)
	if err != nil {
		if isNotFoundErr(err) {
			return forbidden(ctx, 1)
		}
		return err
	}
	parent := nz(in.ParentID)
	if samePtr(d.ParentID, parent) {
		return nil
	}
	if !parentOK(sc, parent) {
		return forbidden(ctx, 1)
	}
	return s.checkSubtree(ctx, sc, id)
}

// CheckDelete 校验删除 id：级联删除的整棵子树必须全部在范围内，否则整体拒绝。
func (s *Service) CheckDelete(ctx context.Context, sc *datascope.Scope, id idgen.ID) error {
	if !sc.Limited() {
		return nil
	}
	return s.checkSubtree(ctx, sc, id)
}

func (s *Service) checkSubtree(ctx context.Context, sc *datascope.Scope, id idgen.ID) error {
	ids, err := s.subtreeIDs(ctx, id)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return forbidden(ctx, 1)
	}
	denied := 0
	for _, v := range ids {
		if !sc.HasDept(v) {
			denied++
		}
	}
	if denied > 0 {
		return forbidden(ctx, denied)
	}
	return nil
}

// CheckSort 校验排序：父节点与涉及的全部 id 都在范围内。
func (s *Service) CheckSort(ctx context.Context, sc *datascope.Scope, in SortInput) error {
	if !sc.Limited() {
		return nil
	}
	denied := 0
	if !parentOK(sc, nz(in.ParentID)) {
		denied++
	}
	for _, id := range in.IDs {
		if !sc.HasDept(int64(id)) {
			denied++
		}
	}
	if denied > 0 {
		return forbidden(ctx, denied)
	}
	return nil
}

// CheckMove 校验移动：目标父级在范围内，且被移动的整棵子树都在范围内。
func (s *Service) CheckMove(ctx context.Context, sc *datascope.Scope, id idgen.ID, in MoveInput) error {
	if !sc.Limited() {
		return nil
	}
	parent := nz(in.ParentID)
	if parent != nil && !sc.HasDept(int64(*parent)) {
		return forbidden(ctx, 1)
	}
	return s.checkSubtree(ctx, sc, id)
}
