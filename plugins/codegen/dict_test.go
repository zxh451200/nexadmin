package codegen

import (
	"context"
	"strings"
	"testing"
)

// 字典列是对标成熟 CMS 生成器的核心补齐：存量与新建两条路径都要钉住——
// 校验放行什么、推荐配置长什么样、生成的 TSX 里真的有 DictTag / DictSelect。

func dictCols() []ColumnConfig {
	return []ColumnConfig{{Name: "status", List: true, Query: true, Form: true, Control: "dict", Dict: "sys_status"}}
}

func TestDictControlValidation(t *testing.T) {
	a, err := analyze("demo_item", richColumns())
	if err != nil {
		t.Fatal(err)
	}

	s, err := buildSpec(a, "", "demo", dictCols())
	if err != nil {
		t.Fatal(err)
	}
	var f *Field
	for i := range s.Fields {
		if s.Fields[i].Name == "status" {
			f = &s.Fields[i]
		}
	}
	if f == nil {
		t.Fatal("status field missing from spec")
	}
	if !f.IsDict() || f.Dict != "sys_status" {
		t.Errorf("status field = %+v, want dict sys_status", f)
	}
	// 其他列不受影响：没配 dict 的保持原控件。
	for i := range s.Fields {
		if s.Fields[i].Name != "status" && s.Fields[i].IsDict() {
			t.Errorf("column %q unexpectedly dict", s.Fields[i].Name)
		}
	}

	for _, tc := range []struct {
		name string
		cfg  ColumnConfig
	}{
		{"缺字典编码", ColumnConfig{Name: "status", Form: true, Control: "dict"}},
		{"编码不合法", ColumnConfig{Name: "status", Form: true, Control: "dict", Dict: "SYS-STATUS"}},
		{"int 列不能配字典", ColumnConfig{Name: "qty", Form: true, Control: "dict", Dict: "sys_status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildSpec(a, "", "demo", []ColumnConfig{tc.cfg}); err == nil {
				t.Errorf("buildSpec accepted %s", tc.name)
			}
		})
	}
}

func TestDefineDictField(t *testing.T) {
	def := TableDef{Name: "product", Tenant: true, Migration: "001", Fields: []FieldDef{
		{Name: "title", Kind: KindString, Length: 100},
		{Name: "status", Kind: KindString, Dict: "sys_status"},
	}}
	td, err := def.normalize()
	if err != nil {
		t.Fatal(err)
	}
	var status defField
	for _, f := range td.fields {
		if f.name == "status" {
			status = f
		}
	}
	if status.dict != "sys_status" || status.length != 64 {
		t.Errorf("status defField = %+v, want dict sys_status length 64", status)
	}
	for _, raw := range td.rawColumns() {
		if raw.Name == "status" && (raw.Length != 64 || !raw.HasDefault || raw.Nullable || raw.Dict == "") {
			t.Errorf("status raw column = %+v, want length 64 with default and dict code", raw)
		}
	}
	for _, m := range td.migrations() {
		if !strings.Contains(m.Content, "VARCHAR(64) NOT NULL DEFAULT ''") {
			t.Errorf("%s migration missing dict column DDL", m.Path)
		}
	}

	// 推荐配置直接落到字典下拉，向导一进来就能看到。
	sc, err := analyze(td.table, td.rawColumns())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range sc.Cols {
		if c.Name == "status" && (c.Config.Control != "dict" || c.Config.Dict != "sys_status") {
			t.Errorf("recommended config = %+v, want dict sys_status", c.Config)
		}
	}

	for _, tc := range []struct {
		name string
		def  TableDef
	}{
		{"int 字段不能带字典", TableDef{Name: "p", Fields: []FieldDef{{Name: "qty", Kind: KindInt, Dict: "sys_status"}}}},
		{"编码不合法", TableDef{Name: "p", Fields: []FieldDef{{Name: "status", Kind: KindString, Dict: "1bad"}}}},
		{"字典列长度不可自定义", TableDef{Name: "p", Fields: []FieldDef{{Name: "status", Kind: KindString, Length: 32, Dict: "sys_status"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.def.normalize(); err == nil {
				t.Errorf("normalize accepted %s", tc.name)
			}
		})
	}
}

func TestRenderDictOutput(t *testing.T) {
	out, err := svc().Generate(context.Background(), GenRequest{Table: "demo_item", Module: "demo", Columns: dictCols()})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = f.Content
	}
	page := files["web/src/pages/demo/DemoPage.tsx"]
	for _, s := range []string{
		`import { DictTag } from '@/framework/components/DictTag'`,
		`import { DictSelect } from '@/framework/components/DictSelect'`,
		`<DictTag dict="sys_status" value={r.status} />`,
		`<DictSelect`,
		`dict="sys_status"`,
	} {
		if !strings.Contains(page, s) {
			t.Errorf("page.tsx missing %q", s)
		}
	}
	form := files["web/src/pages/demo/DemoFormDialog.tsx"]
	if !strings.Contains(form, `<DictSelect dict="sys_status"`) {
		t.Errorf("form.tsx missing DictSelect, got:\n%s", form)
	}
	mock := files["web/src/pages/demo/demo.mock.ts"]
	if !strings.Contains(mock, "'active'") {
		t.Error("mock missing dict placeholder value")
	}
}
