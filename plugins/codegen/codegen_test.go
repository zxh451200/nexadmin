package codegen

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
)

type fakeReader struct {
	tables []string
	cols   map[string][]RawColumn
}

func (f *fakeReader) Tables(context.Context) ([]string, error) { return f.tables, nil }
func (f *fakeReader) Columns(_ context.Context, t string) ([]RawColumn, error) {
	return f.cols[t], nil
}

const evilComment = "标题\n}\nfunc Evil() { os.Exit(1) }\n// `x` \u2028 \u202e */"

func richColumns() []RawColumn {
	return []RawColumn{
		{Name: "id", TypeName: "bigint", PrimaryKey: true},
		{Name: "tenant_id", TypeName: "bigint"},
		{Name: "title", TypeName: "varchar", Length: 100, Comment: evilComment},
		{Name: "summary", TypeName: "text", Nullable: true},
		{Name: "qty", TypeName: "int"},
		{Name: "price", TypeName: "decimal"},
		{Name: "is_hot", TypeName: "tinyint", FullType: "tinyint(1)"},
		{Name: "owner_id", TypeName: "bigint"},
		{Name: "published_at", TypeName: "datetime", Nullable: true},
		{Name: "birthday", TypeName: "date", Nullable: true},
		{Name: "status", TypeName: "varchar", Length: 20, HasDefault: true},
		{Name: "created_at", TypeName: "datetime"},
		{Name: "updated_at", TypeName: "datetime"},
		{Name: "attachment", TypeName: "blob", Nullable: true},
		{Name: "Bad-Col", TypeName: "varchar"},
	}
}

func newFake() *fakeReader {
	return &fakeReader{
		tables: []string{"demo_item", "plain_note", "dict_tag", "tenant_note", "sys_user", "plg_cron_job", "sys_migration",
			"schema_migrations", "Bad Table", "x;drop table y", "no_pk"},
		cols: map[string][]RawColumn{
			"demo_item": richColumns(),
			"plain_note": {
				{Name: "id", TypeName: "bigint", PrimaryKey: true},
				{Name: "name", TypeName: "varchar", Length: 50},
				{Name: "deleted_at", TypeName: "datetime", Nullable: true},
			},
			"dict_tag": {
				{Name: "id", TypeName: "int8", PrimaryKey: true},
				{Name: "label", TypeName: "varchar"},
				{Name: "created_at", TypeName: "timestamptz"},
				{Name: "updated_at", TypeName: "timestamptz"},
			},
			"tenant_note": {
				{Name: "id", TypeName: "bigint", PrimaryKey: true},
				{Name: "tenant_id", TypeName: "bigint"},
				{Name: "content", TypeName: "text"},
				{Name: "level", TypeName: "smallint"},
			},
			"no_pk": {{Name: "a", TypeName: "int"}},
		},
	}
}

func TestIdentValidation(t *testing.T) {
	okTables := []string{"a", "article", "my_table_2", strings.Repeat("a", 48)}
	for _, s := range okTables {
		if err := ValidTable(s); err != nil {
			t.Errorf("ValidTable(%q) = %v, want ok", s, err)
		}
	}
	badIdents := []string{"", "1abc", "_abc", "Abc", "a-b", "a b", "a;b", "a'b", "a\"b", "a`b", "a\nb", "a.b", "a/b", "a\\b",
		"../x", "a$(id)", "表", strings.Repeat("a", 49), "a\x00b"}
	for _, s := range badIdents {
		if ValidIdent(s) == nil {
			t.Errorf("ValidIdent(%q) accepted", s)
		}
	}
	for _, s := range []string{"sys_user", "sys_migration", "plg_cron_job", "schema_migrations", "migrations", "x_migrations"} {
		if ValidTable(s) == nil {
			t.Errorf("framework table %q accepted", s)
		}
	}
	for _, s := range []string{"article", "note2"} {
		if err := ValidModule(s); err != nil {
			t.Errorf("ValidModule(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "Article", "my_mod", "sysx", "plgx", "user", "post", "func", "type", "service", "handler", "form", "view",
		"core", "a b", "a-b", "9a", strings.Repeat("a", 32), "go", "a\"; import \"os"} {
		if ValidModule(s) == nil {
			t.Errorf("ValidModule(%q) accepted", s)
		}
	}
	for _, s := range []string{"nexadmin", "github.com/acme/app", "example.com/x-y_z/v2"} {
		if err := ValidGoModule(s); err != nil {
			t.Errorf("ValidGoModule(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"a\"b", "a b", "a/../b", "A/b", "../x", "a\nb", "x\"\n_ \"os", "a//b", "/a"} {
		if ValidGoModule(s) == nil {
			t.Errorf("ValidGoModule(%q) accepted", s)
		}
	}
}

func TestSanitizeComment(t *testing.T) {
	got := sanitizeComment(evilComment)
	for _, r := range got {
		if r == '\n' || r == '\r' || r == '`' || r == '\u2028' || r == '\u202e' {
			t.Fatalf("unsafe rune %q in %q", r, got)
		}
	}
	if long := sanitizeComment(strings.Repeat("长", 200)); len([]rune(long)) != 60 {
		t.Errorf("not truncated: %d", len([]rune(long)))
	}
}

func TestClassifyAndShape(t *testing.T) {
	a, err := analyze("demo_item", richColumns())
	if err != nil {
		t.Fatal(err)
	}
	if a.Shape != ShapeTenant || !a.Tenant {
		t.Errorf("shape = %s tenant=%v", a.Shape, a.Tenant)
	}
	want := map[string]Kind{"title": KindString, "summary": KindText, "qty": KindInt, "price": KindFloat, "is_hot": KindBool,
		"owner_id": KindID, "published_at": KindDateTime, "birthday": KindDate, "attachment": KindNone}
	for _, c := range a.Cols {
		if k, ok := want[c.Name]; ok && c.Kind != k {
			t.Errorf("%s kind = %q, want %q", c.Name, c.Kind, k)
		}
		if c.Name == "Bad-Col" && c.Supported {
			t.Error("invalid column name must be unsupported")
		}
		if (c.Name == "id" || c.Name == "tenant_id" || c.Name == "created_at") && !c.System {
			t.Errorf("%s should be system", c.Name)
		}
	}
	title := a.Cols[2]
	if !title.Config.Required || !title.Config.Query || !title.Config.List || title.Config.Control != "input" {
		t.Errorf("title recommendation = %+v", title.Config)
	}
	if status := a.Cols[10]; status.Config.Required {
		t.Error("column with default must not be required")
	}
	if _, err := analyze("no_pk", []RawColumn{{Name: "a", TypeName: "int"}}); err == nil {
		t.Error("table without id pk accepted")
	}
	if _, err := analyze("t", []RawColumn{{Name: "id", TypeName: "int", PrimaryKey: true}}); err == nil {
		t.Error("INT id accepted")
	}
	dup := []RawColumn{{Name: "id", TypeName: "bigint", PrimaryKey: true}, {Name: "user_id", TypeName: "int"}, {Name: "user_i_d", TypeName: "int"}}
	if _, err := analyze("t", dup); err == nil {
		t.Error("colliding Go names accepted")
	}
}

func svc() *Service { return NewService(newFake()) }

func TestServiceTablesAndRejects(t *testing.T) {
	ctx := context.Background()
	tabs, err := svc().Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range tabs {
		names = append(names, x.Name)
	}
	if got, want := strings.Join(names, ","), "demo_item,dict_tag,no_pk,plain_note,tenant_note"; got != want {
		t.Fatalf("tables = %s, want %s", got, want)
	}
	for _, bad := range []string{"sys_user", "plg_cron_job", "schema_migrations", "Bad Table", "x;drop table y", "ghost", "../etc"} {
		if _, err := svc().Columns(ctx, bad); err == nil {
			t.Errorf("Columns(%q) accepted", bad)
		}
		if _, err := svc().Generate(ctx, GenRequest{Table: bad, Module: "demo"}); err == nil {
			t.Errorf("Generate(%q) accepted", bad)
		}
	}
	if _, err := svc().Generate(ctx, GenRequest{Table: "demo_item", Module: "Bad Module"}); err == nil {
		t.Error("bad module accepted")
	}
	if _, err := svc().Generate(ctx, GenRequest{Table: "demo_item", Module: "demo", GoModule: "x\"\n_ \"os"}); err == nil {
		t.Error("bad go module accepted")
	}
	cfgs := [][]ColumnConfig{
		{{Name: "nope"}},
		{{Name: "title\"; os.Exit(1)//"}},
		{{Name: "title"}, {Name: "title"}},
		{{Name: "id", Form: true}},
		{{Name: "attachment", Form: true}},
		{{Name: "summary", Form: true, Query: true}},
		{{Name: "qty", Form: true, Required: true}},
		{{Name: "title", Form: true, Control: "switch"}},
		{{Name: "title", Required: true}},
	}
	for i, c := range cfgs {
		if _, err := svc().Generate(ctx, GenRequest{Table: "demo_item", Module: "demo", Columns: c}); err == nil {
			t.Errorf("config %d accepted: %+v", i, c)
		}
	}
}

func TestRenderParsesAndFormats(t *testing.T) {
	for _, tc := range []struct{ table, module string }{
		{"demo_item", "demo"}, {"plain_note", "note"}, {"dict_tag", "tag"}, {"tenant_note", "tnote"},
	} {
		out, err := svc().Generate(context.Background(), GenRequest{Table: tc.table, Module: tc.module})
		if err != nil {
			t.Fatalf("%s: %v", tc.table, err)
		}
		if len(out.Files) != 12 {
			t.Errorf("%s: %d files", tc.table, len(out.Files))
		}
		fset := token.NewFileSet()
		for _, f := range out.Files {
			if !validOutPath(f.Path) {
				t.Errorf("bad path %q", f.Path)
			}
			switch f.Lang {
			case "go":
				af, err := parser.ParseFile(fset, f.Path, f.Content, parser.AllErrors|parser.ParseComments)
				if err != nil {
					t.Fatalf("%s: parse: %v\n%s", f.Path, err, f.Content)
				}
				if af.Name.Name != tc.module {
					t.Errorf("%s: package %s", f.Path, af.Name.Name)
				}
				if fmted, err := format.Source([]byte(f.Content)); err != nil || string(fmted) != f.Content {
					t.Errorf("%s: not gofmt-stable (%v)", f.Path, err)
				}
				if tc.table == "demo_item" {
					assertNoInjection(t, f.Path, af)
				}
			case "json":
				var v map[string]map[string]any
				if err := json.Unmarshal([]byte(f.Content), &v); err != nil || v[tc.module] == nil {
					t.Errorf("%s: bad locale json: %v", f.Path, err)
				}
			}
			if strings.Contains(f.Content, "<no value>") || strings.Contains(f.Content, "%!") {
				t.Errorf("%s: unrendered template output", f.Path)
			}
		}
	}
}

// assertNoInjection 确认带换行的恶意列注释没有逃出注释：生成物里不能出现 Evil 函数或 os 导入。
func assertNoInjection(t *testing.T, name string, af *ast.File) {
	t.Helper()
	for _, d := range af.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "Evil" {
			t.Fatalf("%s: injected func Evil", name)
		}
	}
	for _, im := range af.Imports {
		if im.Path.Value == `"os"` {
			t.Fatalf("%s: injected import os", name)
		}
	}
}

func TestRenderContent(t *testing.T) {
	out, err := svc().Generate(context.Background(), GenRequest{Table: "demo_item", Module: "demo", GoModule: "example.com/app"})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range out.Files {
		files[f.Path] = f.Content
	}
	model := files["internal/modules/demo/model.go"]
	for _, s := range []string{"database.TenantModel", `TableName() string { return "demo_item" }`, "OwnerID ", "idgen.ID", "example.com/app/core/database"} {
		if !strings.Contains(model, s) {
			t.Errorf("model.go missing %q", s)
		}
	}
	if strings.Contains(model, "Attachment") || strings.Contains(model, "Bad") {
		t.Error("unsupported columns leaked into model")
	}
	if !strings.Contains(files["internal/modules/demo/module.go"], `core.Perm("demo:edit")`) {
		t.Error("module.go missing core.Perm")
	}
	if !strings.Contains(files["internal/modules/demo/seed.go"], `"demo:list"`) {
		t.Error("seed.go missing menu seed")
	}
	if !strings.Contains(files["web/src/pages/demo/DemoPage.tsx"], "@/framework/components/DataTable") {
		t.Error("page does not use framework DataTable")
	}
	if _, ok := files["web/src/pages/demo/demo.mock.ts"]; !ok {
		t.Error("mock missing")
	}
	var zh map[string]map[string]any
	if err := json.Unmarshal([]byte(files["web/src/pages/demo/locales/zh-CN.json"]), &zh); err != nil {
		t.Fatal(err)
	}
	if got := zh["demo"]["field"].(map[string]any)["title"].(string); strings.ContainsAny(got, "\n`") {
		t.Errorf("zh label not sanitized: %q", got)
	}
}

func TestZipInMemoryAndPaths(t *testing.T) {
	out, err := svc().Generate(context.Background(), GenRequest{Table: "plain_note", Module: "note"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := out.Zip()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != len(out.Files) {
		t.Fatalf("zip has %d entries, want %d", len(zr.File), len(out.Files))
	}
	for _, f := range zr.File {
		if !validOutPath(f.Name) || strings.HasPrefix(f.Name, "/") {
			t.Errorf("bad zip entry %q", f.Name)
		}
	}
	for _, p := range []string{"../x/a.go", "/etc/passwd", "internal/modules/../../x.go", "internal/modules/a/../../b.go", "internal/modules/a/.hidden", "other/a.go", "internal/modules/A/b.go", "internal/modules//a.go", "internal/modules/a/b\\c.go"} {
		if validOutPath(p) {
			t.Errorf("validOutPath(%q) = true", p)
		}
		if _, err := (&Output{Files: []File{{Path: p}}}).Zip(); err == nil {
			t.Errorf("Zip accepted path %q", p)
		}
	}
}

func TestHandlersAndPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &handler{svc: svc()}
	e := gin.New()
	e.GET("/tables", h.tables)
	e.GET("/tables/:name/columns", h.columns)
	e.POST("/schema", h.schema)
	e.POST("/preview", h.preview)
	e.POST("/download", h.download)

	do := func(method, url, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		return w
	}
	if w := do("GET", "/tables", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "demo_item") || strings.Contains(w.Body.String(), "sys_user") {
		t.Errorf("tables: %d %s", w.Code, w.Body.String())
	}
	if w := do("GET", "/tables/demo_item/columns", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"suggestedModule":"demoitem"`) {
		t.Errorf("columns: %d %s", w.Code, w.Body.String())
	}
	if w := do("GET", "/tables/sys_user/columns", ""); w.Code == 200 {
		t.Error("framework table columns served")
	}
	if w := do("POST", "/schema", `{"table":"demo_item"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"shape":"tenant"`) {
		t.Errorf("schema: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/schema", `{"define":{"name":"product","tenant":true,"fields":[{"name":"title","kind":"string","required":true}]}}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"table":"biz_product"`) {
		t.Errorf("defined schema: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/preview", `{"table":"plain_note","module":"note"}`); w.Code != 200 || !strings.Contains(w.Body.String(), "internal/modules/note/model.go") {
		t.Errorf("preview: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/preview", `{"define":{"name":"product","fields":[{"name":"title","kind":"string"}]},"module":"product"}`); w.Code != 200 || !strings.Contains(w.Body.String(), "migrations/business/postgres/") {
		t.Errorf("defined preview: %d %s", w.Code, w.Body.String())
	}
	if w := do("POST", "/preview", `{"define":{"name":"product","fields":[]},"module":"product"}`); w.Code == 200 {
		t.Error("preview accepted a definition without fields")
	}
	if w := do("POST", "/preview", `{"table":"plain_note","module":"x\"y"}`); w.Code == 200 {
		t.Error("preview accepted bad module")
	}
	w := do("POST", "/download", `{"table":"plain_note","module":"note"}`)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" ||
		w.Header().Get("Content-Disposition") != `attachment; filename="note-codegen.zip"` {
		t.Fatalf("download: %d %v", w.Code, w.Header())
	}
	b, _ := io.ReadAll(w.Body)
	if _, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err != nil {
		t.Errorf("download is not a zip: %v", err)
	}

	// 真实注册的路由：未登录一律被 core.Perm 拦下（不是 200）。
	r := gin.New()
	(&module{}).Register(r.Group("/api/v1"), &core.Deps{})
	routes := r.Routes()
	if len(routes) != 5 {
		t.Fatalf("registered %d routes, want 5", len(routes))
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.Method, strings.ReplaceAll(rt.Path, ":name", "demo_item"), strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without auth = %d, want 401/403", rt.Method, rt.Path, rec.Code)
		}
	}
}

// TestGeneratedCodeCompiles 把生成的 Go 文件写到本包下的临时点目录（go 工具链的通配符会忽略它），
// 用真实的 go build / go vet 编译；结束后删除。找不到 go 命令时跳过。
func TestGeneratedCodeCompiles(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not found")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := os.MkdirTemp(".", ".gentest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	// demotwo 打开所有类型的查询条件与必填，覆盖 int / id / bool / date 的解析分支
	allq := []ColumnConfig{{Name: "qty", List: true, Query: true, Form: true, Control: "input"}, {Name: "owner_id", List: true, Query: true, Form: true},
		{Name: "birthday", List: true, Query: true, Form: true, Required: true}, {Name: "is_hot", List: true, Query: true, Form: true},
		{Name: "summary", Form: true, Required: true, Control: "textarea"}, {Name: "published_at", List: true, Form: true, Required: true},
		{Name: "title", List: true, Query: true, Form: true, Required: true}}
	var pkgs []string
	for _, tc := range []struct {
		table, module string
		cols          []ColumnConfig
		def           *TableDef
	}{
		{"demo_item", "demo", nil, nil}, {"plain_note", "note", nil, nil}, {"dict_tag", "tag", nil, nil},
		{"tenant_note", "tnote", nil, nil}, {"demo_item", "demotwo", allq, nil},
		{"", "softprod", nil, &TableDef{Name: "soft_product", Tenant: true, SoftDelete: true, Fields: []FieldDef{
			{Name: "title", Kind: KindString, Length: 80, Required: true},
			{Name: "qty", Kind: KindInt},
			{Name: "published_at", Kind: KindDateTime},
		}}},
	} {
		out, err := svc().Generate(context.Background(), GenRequest{Table: tc.table, Define: tc.def, Module: tc.module, GoModule: "nexadmin", Columns: tc.cols})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range out.Files {
			if f.Lang != "go" {
				continue
			}
			p := filepath.Join(tmp, filepath.FromSlash(f.Path))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		pkgs = append(pkgs, "./"+filepath.ToSlash(filepath.Join("plugins", "codegen", tmp, "internal", "modules", tc.module)))
	}
	for _, verb := range []string{"build", "vet"} {
		cmd := exec.Command(goBin, append([]string{verb}, pkgs...)...)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s generated code failed: %v\n%s", verb, err, b)
		}
	}
}

func generatedFiles(out *Output) map[string]string {
	m := map[string]string{}
	for _, f := range out.Files {
		m[f.Path] = f.Content
	}
	return m
}

// TestDefineTableAndMigrations 覆盖向导新建表：表名加前缀、列推荐、软删、三引擎迁移与提示。
func TestDefineTableAndMigrations(t *testing.T) {
	def := &TableDef{
		Name: "biz_product", Tenant: true, SoftDelete: true, Migration: "7",
		Fields: []FieldDef{
			{Name: "title", Kind: KindString, Length: 80, Required: true, Comment: "商品标题"},
			{Name: "qty", Kind: KindInt},
			{Name: "price", Kind: KindFloat},
			{Name: "on_sale", Kind: KindBool},
			{Name: "published_at", Kind: KindDateTime},
		},
	}
	sc, err := svc().Schema(context.Background(), SchemaRequest{Define: def})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Table != "biz_product" || sc.Shape != ShapeTenant || !sc.Tenant || sc.SuggestedModule != "product" {
		t.Fatalf("schema = %+v", sc)
	}
	var deleted *ColumnInfo
	for i := range sc.Columns {
		if sc.Columns[i].Name == "deleted_at" {
			deleted = &sc.Columns[i]
		}
	}
	if deleted == nil || !deleted.System || deleted.Config.Form || deleted.Config.List {
		t.Fatalf("deleted_at = %+v", deleted)
	}

	out, err := svc().Generate(context.Background(), GenRequest{Define: def, Module: "product"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 15 {
		t.Errorf("files = %d, want 15 (12 module + 3 migrations)", len(out.Files))
	}
	files := generatedFiles(out)
	for _, engine := range migrationEngines {
		p := "migrations/business/" + engine + "/007_biz_product.sql"
		if !strings.Contains(files[p], "CREATE TABLE biz_product") || !strings.Contains(files[p], "tenant_id BIGINT NOT NULL") {
			t.Errorf("%s missing table DDL: %q", p, files[p])
		}
	}
	for _, s := range []string{"VARCHAR(80) NOT NULL", "INT NOT NULL DEFAULT 0", "DOUBLE NOT NULL DEFAULT 0",
		"TINYINT(1) NOT NULL DEFAULT 0", "DATETIME(3) NOT NULL", "deleted_at DATETIME(3) NULL", "idx_biz_product_tenant"} {
		if !strings.Contains(files["migrations/business/mysql/007_biz_product.sql"], s) {
			t.Errorf("mysql migration missing %q", s)
		}
	}
	for _, s := range []string{"TIMESTAMP(3)", "BOOLEAN NOT NULL DEFAULT FALSE", "DOUBLE PRECISION", "INTEGER NOT NULL DEFAULT 0"} {
		if !strings.Contains(files["migrations/business/postgres/007_biz_product.sql"], s) {
			t.Errorf("postgres migration missing %q", s)
		}
	}
	for _, s := range []string{"database.TenantModel", "gorm.DeletedAt", `"gorm.io/gorm"`, `TableName() string { return "biz_product" }`} {
		if !strings.Contains(files["internal/modules/product/model.go"], s) {
			t.Errorf("model missing %q", s)
		}
	}
	if n := out.Notes[len(out.Notes)-1]; n.Code != "migration" || n.Value != "migrations/business/mysql/007_biz_product.sql" {
		t.Errorf("last note = %+v", n)
	}
	for _, f := range out.Files {
		if !validOutPath(f.Path) {
			t.Errorf("unsafe path %q", f.Path)
		}
	}
}

func TestDefineValidation(t *testing.T) {
	valid := FieldDef{Name: "title", Kind: KindString}
	many := make([]FieldDef, 0, maxDefFields+1)
	for i := 0; i <= maxDefFields; i++ {
		many = append(many, FieldDef{Name: fmt.Sprintf("f%d", i), Kind: KindString})
	}
	bad := map[string]*TableDef{
		"no name":         {Fields: []FieldDef{valid}},
		"no fields":       {Name: "product"},
		"reserved field":  {Name: "product", Fields: []FieldDef{{Name: "id", Kind: KindInt}}},
		"duplicate field": {Name: "product", Fields: []FieldDef{valid, valid}},
		"bad kind":        {Name: "product", Fields: []FieldDef{{Name: "title", Kind: "blob"}}},
		"length on int":   {Name: "product", Fields: []FieldDef{{Name: "qty", Kind: KindInt, Length: 4}}},
		"required bool":   {Name: "product", Fields: []FieldDef{{Name: "on_sale", Kind: KindBool, Required: true}}},
		"length too long": {Name: "product", Fields: []FieldDef{{Name: "title", Kind: KindString, Length: maxVarchar + 1}}},
		"too many fields": {Name: "product", Fields: many},
		"bad migration":   {Name: "product", Migration: "abc", Fields: []FieldDef{valid}},
		"zero migration":  {Name: "product", Migration: "0", Fields: []FieldDef{valid}},
		"bad name":        {Name: "Product X", Fields: []FieldDef{valid}},
	}
	for name, def := range bad {
		if _, err := svc().Schema(context.Background(), SchemaRequest{Define: def}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := svc().Generate(context.Background(), GenRequest{Module: "product"}); err == nil {
		t.Error("request without table or define accepted")
	}
	if _, err := svc().Generate(context.Background(), GenRequest{Table: "demo_item", Define: &TableDef{Name: "p", Fields: []FieldDef{valid}}, Module: "product"}); err == nil {
		t.Error("table + define accepted")
	}
}

// TestExistingSoftDeleteAndModuleSuggestion 已有表带 deleted_at 时按软删列处理，推荐模块名去掉 biz_ 前缀。
func TestExistingSoftDeleteAndModuleSuggestion(t *testing.T) {
	sc, err := svc().Columns(context.Background(), "plain_note")
	if err != nil {
		t.Fatal(err)
	}
	if sc.SuggestedModule != "plainnote" {
		t.Errorf("suggestedModule = %q, want plainnote", sc.SuggestedModule)
	}
	var system bool
	for _, c := range sc.Columns {
		if c.Name == "deleted_at" {
			system = c.System && !c.Config.Form && !c.Config.List
		}
	}
	if !system {
		t.Error("deleted_at should be a system column")
	}
	out, err := svc().Generate(context.Background(), GenRequest{Table: "plain_note", Module: "note"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generatedFiles(out)["internal/modules/note/model.go"], "gorm.DeletedAt") {
		t.Error("existing deleted_at should generate gorm.DeletedAt")
	}
	for in, want := range map[string]string{"biz_article": "article", "biz_demo_product": "demoproduct", "biz_user": "bizuser", "demo_item": "demoitem"} {
		if got := suggestModule(in); got != want {
			t.Errorf("suggestModule(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCodegenOwnership(t *testing.T) {
	out, err := svc().Generate(context.Background(), GenRequest{Table: "demo_item", Module: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		if f.Lang == "go" && !strings.HasPrefix(f.Path, "internal/modules/demo/") {
			t.Fatalf("framework output: %s", f.Path)
		}
	}
	if !strings.Contains(out.Notes[0].Value, "/internal/modules/demo") {
		t.Fatal(out.Notes)
	}
	data, err := out.Zip()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if !validOutPath(f.Name) {
			t.Fatal(f.Name)
		}
	}
	for _, p := range []string{"modules/demo/a.go", "/internal/modules/demo/a.go", "internal/modules/../a.go", "internal/modules/demo/../../a.go"} {
		if validOutPath(p) {
			t.Fatal(p)
		}
	}
}
