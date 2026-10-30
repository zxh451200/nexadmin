package dept

import (
	"context"
	"strings"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// sceneManage 是部门管理页的读取场景：对受限操作者按数据范围裁剪。不带 scene 的调用（各下拉）行为不变。
const sceneManage = "manage"

// pathIDs 解析物化路径 "/1/4/" 中的全部 id（含自身）。
func pathIDs(path string) []int64 {
	var out []int64
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		if id, err := idgen.Parse(seg); err == nil {
			out = append(out, int64(id))
		}
	}
	return out
}

// TreeScoped 返回按数据范围裁剪的部门树：范围内的部门，加上它们到根的祖先节点（维持树结构）。
// 范围外的祖先节点标记 Readonly，前端置灰。sc 不受限（nil/超管/范围全部）时与 Tree 完全一致。
func (s *Service) TreeScoped(ctx context.Context, sc *datascope.Scope) ([]*Dept, error) {
	if !sc.Limited() {
		return s.Tree(ctx)
	}
	var list []*Dept
	if err := s.db.WithContext(ctx).Order("sort, id").Find(&list).Error; err != nil {
		return nil, err
	}
	keep := make(map[int64]bool, len(list))
	for _, d := range list {
		if !sc.HasDept(int64(d.ID)) {
			continue
		}
		for _, id := range pathIDs(d.Path) { // 含自身与全部祖先
			keep[id] = true
		}
		keep[int64(d.ID)] = true
	}
	kept := make([]*Dept, 0, len(keep))
	for _, d := range list {
		if keep[int64(d.ID)] {
			d.Readonly = !sc.HasDept(int64(d.ID))
			kept = append(kept, d)
		}
	}
	return buildTree(kept), nil
}

// GetScoped 读取单个部门；受限操作者对范围外（含仅作为祖先保留的只读节点）与不存在的部门得到同样的 403。
func (s *Service) GetScoped(ctx context.Context, sc *datascope.Scope, id idgen.ID) (*Dept, error) {
	if sc.Limited() && !sc.HasDept(int64(id)) {
		return nil, forbidden(ctx, 1)
	}
	d, err := s.Get(ctx, id)
	if err != nil && sc.Limited() && isNotFoundErr(err) {
		return nil, forbidden(ctx, 1)
	}
	return d, err
}
