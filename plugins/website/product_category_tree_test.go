package website

import (
	"testing"

	"nexadmin/pkg/idgen"
)

// 空类目整枝不进前台导航，但父级的 HasChildren 不能只看「有没有子类目记录」——
// 否则侧栏就是「带 › 箭头、悬停/展开是空白」。
func TestProductCategoryLinksHasChildrenOnlyWhenChildRendered(t *testing.T) {
	rows := []ProductCategory{
		{Title: "能源管理", Slug: "energy-management", ProductCount: 4},
		// 子类目一个商品都没有：会被 walk 跳过，不该让父级长出箭头。
		{Title: "EMS 云平台", Slug: "ems-cloud", ParentID: 1, ProductCount: 0},
		{Title: "电池储能系统", Slug: "battery-ess", ProductCount: 3},
		{Title: "工商业储能", Slug: "commercial-ess", ParentID: 3, ProductCount: 7},
	}
	for i := range rows {
		rows[i].ID = idgen.ID(i + 1)
	}

	flat := ProductCategoryLinks(rows, "")
	got := make(map[string]productCatLink, len(flat))
	for _, l := range flat {
		got[l.Slug] = l
	}

	if _, ok := got["ems-cloud"]; ok {
		t.Fatalf("空类目不该出现在前台列表: %+v", flat)
	}
	if c := got["energy-management"]; c.HasChildren {
		t.Fatalf("子类目全空时父级不该标 HasChildren: %+v", c)
	}
	if c := got["battery-ess"]; !c.HasChildren {
		t.Fatalf("有露出子类目时父级应标 HasChildren: %+v", c)
	}

	for _, root := range NestProductCategories(flat) {
		if root.HasChildren && len(root.Children) == 0 {
			t.Fatalf("树里出现「有箭头无子节点」的节点: %+v", root)
		}
	}
}
