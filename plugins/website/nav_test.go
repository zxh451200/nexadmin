package website

import (
	"encoding/json"
	"strings"
	"testing"

	"nexadmin/pkg/idgen"
)

// 顶层 1、二级 2、三级 3 都合法；第四级必须拒绝。
func TestNavDepthLimits(t *testing.T) {
	parents := map[uint64]uint64{1: 0, 2: 1, 3: 2}
	cases := []struct {
		name     string
		id, pid  uint64
		want     int
		wantFail bool
	}{
		{"top level", 9, 0, 1, false},
		{"under top", 9, 1, 2, false},
		{"under level 2", 9, 2, 3, false},
		{"under level 3", 9, 3, 0, true},
		{"missing parent", 9, 99, 0, true},
		{"self parent", 2, 2, 0, true},
		{"descendant parent", 1, 3, 0, true},
	}
	for _, c := range cases {
		got, err := navDepth(parents, c.id, c.pid)
		if c.wantFail {
			if err == nil {
				t.Errorf("%s: want error, got depth %d", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: depth = %d, want %d", c.name, got, c.want)
		}
	}
}

// 把二级节点挂到三级节点下：自身层级合法，但现有子树整体越界，必须拒绝。
func TestNavSubtreeHeight(t *testing.T) {
	parents := map[uint64]uint64{1: 0, 2: 1, 3: 2}
	if got := navSubtreeHeight(parents, 1); got != 3 {
		t.Errorf("height(1) = %d, want 3", got)
	}
	if got := navSubtreeHeight(parents, 3); got != 1 {
		t.Errorf("height(3) = %d, want 1", got)
	}
}

func TestValidateNavParents(t *testing.T) {
	if err := validateNavParents(map[uint64]uint64{1: 0, 2: 1, 3: 2}); err != nil {
		t.Errorf("valid tree rejected: %v", err)
	}
	if err := validateNavParents(map[uint64]uint64{1: 2, 2: 1}); err == nil {
		t.Error("cycle should be rejected")
	}
	if err := validateNavParents(map[uint64]uint64{1: 0, 2: 1, 3: 2, 4: 3}); err == nil {
		t.Error("four levels should be rejected")
	}
	if err := validateNavParents(map[uint64]uint64{1: 0, 2: 9}); err == nil {
		t.Error("missing parent should be rejected")
	}
}

func TestNavSubtreeIDs(t *testing.T) {
	parents := map[uint64]uint64{1: 0, 2: 1, 3: 2, 4: 0}
	got := navSubtreeIDs(parents, 1)
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("navSubtreeIDs = %v, want [1 2 3]", got)
	}
	if ids := navSubtreeIDs(parents, 99); len(ids) != 0 {
		t.Errorf("missing root = %v, want empty", ids)
	}
}

func TestBuildNavTree(t *testing.T) {
	rows := []Nav{
		{ParentID: 0, Label: "a"},
		{ParentID: 0, Label: "a2"},
		{ParentID: 1, Label: "b"},
		{ParentID: 3, Label: "c"},
		{ParentID: 42, Label: "orphan"},
	}
	rows[0].ID = idgen.ID(1)
	rows[1].ID = idgen.ID(2)
	rows[2].ID = idgen.ID(3)
	rows[3].ID = idgen.ID(4)
	rows[4].ID = idgen.ID(5)
	tree := buildNavTree(rows)
	if len(tree) != 2 || tree[0].Label != "a" || tree[1].Label != "a2" {
		t.Fatalf("root = %+v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].Label != "b" {
		t.Fatalf("level 2 = %+v", tree[0].Children)
	}
	if len(tree[0].Children[0].Children) != 1 || tree[0].Children[0].Children[0].Label != "c" {
		t.Fatalf("level 3 = %+v", tree[0].Children[0].Children)
	}
}

// parentId 在 JSON 里必须是字符串，否则雪花 ID 在浏览器会丢精度；输入兼容字符串与数字。
func TestNavParentIDJSON(t *testing.T) {
	b, err := json.Marshal(Nav{ParentID: NavParentID(123456789012345678)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"parentId":"123456789012345678"`) {
		t.Errorf("marshal = %s", b)
	}
	var f ItemForm
	if err := json.Unmarshal([]byte(`{"label":"x","href":"/x","parentId":0}`), &f); err != nil || f.ParentID != 0 {
		t.Errorf("unmarshal number = %v, %v", f.ParentID, err)
	}
	if err := json.Unmarshal([]byte(`{"label":"x","href":"/x","parentId":"123456789012345678"}`), &f); err != nil ||
		uint64(f.ParentID) != 123456789012345678 {
		t.Errorf("unmarshal string = %v, %v", f.ParentID, err)
	}
	if err := json.Unmarshal([]byte(`{"parentId":"bad"}`), &f); err == nil {
		t.Error("invalid parentId should fail")
	}
}

// 三级导航必须渲染出一级下拉、二级嵌套与移动端缩进列表。
func TestNavTreeTemplateRendering(t *testing.T) {
	v := testView()
	grand := Nav{Label: "孙项", Href: "/p/about"}
	grand.ID = idgen.ID(8)
	child := Nav{Label: "子项", Href: "/news", Children: []Nav{grand}}
	child.ID = idgen.ID(7)
	root := Nav{Label: "根项", Href: "/", Enabled: true, Children: []Nav{child}}
	root.ID = idgen.ID(6)
	v.Navs = []Nav{root}

	var b strings.Builder
	if err := pageTmpl["home"].ExecuteTemplate(&b, "layout", v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`class="nav-item has-sub"`,
		`data-nav-toggle`,
		`aria-label="展开「根项」的子导航"`,
		`class="nav-sub"`,
		`>子项</a>`,
		`>孙项</a>`,
		`class="mobile-sub"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("layout missing %q", want)
		}
	}
}
