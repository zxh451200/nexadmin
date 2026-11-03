package codegen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 树形表与主子表是生成器的两个结构化模板：检测、推荐配置与产物形态都要钉住，
// 编译由 TestGeneratedCodeCompiles 的 treedemo / subdemo 用例兜底。

func TestTreeDetectionAndRecommend(t *testing.T) {
	a, err := analyze("demo_tree", newFake().cols["demo_tree"])
	if err != nil {
		t.Fatal(err)
	}
	if !a.Tree {
		t.Fatal("demo_tree must be detected as a tree table")
	}
	for _, c := range a.Cols {
		if c.Name != "parent_id" {
			continue
		}
		if !c.Config.Form {
			t.Error("parent_id must be in the form by default")
		}
		if c.Config.List || c.Config.Query {
			t.Errorf("parent_id should not be listed/queried by default, got %+v", c.Config)
		}
	}
	sc := a.schema()
	if !sc.Tree {
		t.Error("schema().tree must be true")
	}
}

func TestSubSpecBuild(t *testing.T) {
	a, err := analyze("demo_reply", newFake().cols["demo_reply"])
	if err != nil {
		t.Fatal(err)
	}
	sub, err := buildSubSpec(a, "article_id", nil, "Demo")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Pascal != "Reply" || sub.JSONPlural != "replies" {
		t.Errorf("sub names = %s / %s, want Reply / replies", sub.Pascal, sub.JSONPlural)
	}
	for _, f := range sub.Fields {
		if f.Name == "article_id" {
			t.Error("foreign key must be excluded from editable child fields")
		}
	}
	if len(sub.FormFields()) == 0 {
		t.Error("child fields must default to form-editable")
	}

	for _, tc := range []struct {
		name string
		fk   string
	}{
		{"外键不存在", "item_id"},
		{"外键不是 BIGINT", "content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildSubSpec(a, tc.fk, nil, "Demo"); err == nil {
				t.Errorf("buildSubSpec accepted %s", tc.name)
			}
		})
	}
}

func TestRenderTreeAndSubOutput(t *testing.T) {
	ctx := context.Background()
	treeOut, err := svc().Generate(ctx, GenRequest{Table: "demo_tree", Module: "treedemo"})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range treeOut.Files {
		files[f.Path] = f.Content
	}
	page := files["web/src/pages/treedemo/TreedemoPage.tsx"]
	for _, s := range []string{
		"import { TreeTable } from '@/framework/components/TreeTable'",
		"function buildTree(",
		`<TreeTable`,
	} {
		if !strings.Contains(page, s) {
			t.Errorf("tree page missing %q", s)
		}
	}
	if !strings.Contains(files["web/src/pages/treedemo/TreedemoFormDialog.tsx"], "parentOptions") {
		t.Error("tree form missing parent options select")
	}
	var zh map[string]map[string]any
	if err := readJSON(files["web/src/pages/treedemo/locales/zh-CN.json"], &zh); err != nil {
		t.Fatal(err)
	}
	if _, ok := zh["treedemo"]["treeRoot"]; !ok {
		t.Error("tree locales missing treeRoot")
	}

	subOut, err := svc().Generate(ctx, GenRequest{
		Table: "demo_item", Module: "subdemo",
		Sub: &SubTableSpec{Table: "demo_reply", ForeignKey: "article_id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	subFiles := map[string]string{}
	for _, f := range subOut.Files {
		subFiles[f.Path] = f.Content
	}
	model := subFiles["internal/modules/subdemo/model.go"]
	// gofmt 会把结构体字段的类型列对齐，断言只钉到类型本身。
	for _, s := range []string{"type Reply struct", "type ReplyView struct", "type ReplyForm struct", "[]ReplyView", "[]ReplyForm"} {
		if !strings.Contains(model, s) {
			t.Errorf("sub model missing %q", s)
		}
	}
	service := subFiles["internal/modules/subdemo/service.go"]
	for _, s := range []string{".Transaction(func(tx *gorm.DB) error", `Where("article_id = ?"`, "f.Children[i].apply(&kid)"} {
		if !strings.Contains(service, s) {
			t.Errorf("sub service missing %q", s)
		}
	}
	form := subFiles["web/src/pages/subdemo/SubdemoFormDialog.tsx"]
	for _, s := range []string{"type KidDraft", "replies: kids.map"} {
		if !strings.Contains(form, s) {
			t.Errorf("sub form missing %q", s)
		}
	}
	var zhSub map[string]map[string]any
	if err := readJSON(subFiles["web/src/pages/subdemo/locales/zh-CN.json"], &zhSub); err != nil {
		t.Fatal(err)
	}
	subNode, ok := zhSub["subdemo"]["sub"].(map[string]any)
	if !ok {
		t.Fatal("sub locales missing sub section")
	}
	if _, ok := subNode["field"].(map[string]any)["content"]; !ok {
		t.Error("sub locales missing child field labels")
	}
}

func readJSON(raw string, v any) error {
	return json.Unmarshal([]byte(raw), v)
}
