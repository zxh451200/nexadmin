package main

import (
	"bytes"
	"encoding/json"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const demoFields = "title:string,status:dict(sys_status),sort:int,cover:image,published_at:datetime,owner_id:ref(user)"

func TestParseFields(t *testing.T) {
	fs, err := parseFields(demoFields + ",tags:file, ok : bool ,intro:text,born:date,price:float,kind:enum(article_kind)")
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{"title", kString, ""}, {"status", kDict, "sys_status"}, {"sort", kInt, ""}, {"cover", kImage, ""},
		{"published_at", kDateTime, ""}, {"owner_id", kRef, "user"}, {"tags", kFile, ""}, {"ok", kBool, ""},
		{"intro", kText, ""}, {"born", kDate, ""}, {"price", kFloat, ""}, {"kind", kDict, "article_kind"},
	}
	if len(fs) != len(want) {
		t.Fatalf("got %d fields: %+v", len(fs), fs)
	}
	for i := range want {
		if fs[i] != want[i] {
			t.Errorf("field %d: got %+v want %+v", i, fs[i], want[i])
		}
	}
	if fs, err := parseFields("  "); err != nil || fs != nil {
		t.Errorf("空串应返回空: %v %v", fs, err)
	}
	bad := map[string]string{
		"title":                          "缺少类型",
		"Title:string":                   "大写字段名",
		"id:int":                         "系统列",
		"order:int":                      "SQL 保留字",
		"a:string,a:int":                 "重复",
		"a:blob":                         "未知类型",
		"a:dict":                         "dict 缺参数",
		"a:ref()":                        "ref 空参数",
		"a:string(3)":                    "string 不接受参数",
		"a:dict(Sys-Status)":             "非法字典编码",
		"a:string,,b:int":                "空项",
		"a:dict(x":                       "括号不匹配",
		"tenant_id:int":                  "系统列",
		strings.Repeat("a", 41) + ":int": "名称过长",
	}
	for in, why := range bad {
		if _, err := parseFields(in); err == nil {
			t.Errorf("%q 应报错（%s）", in, why)
		}
	}
}

func TestValidModuleName(t *testing.T) {
	for _, ok := range []string{"article", "a", "order2", "newsletter"} {
		if err := validModuleName(ok); err != nil {
			t.Errorf("%q 应合法: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Article", "my-mod", "1abc", "sys_user", "system", "core", "user", "my_mod", strings.Repeat("a", 32)} {
		if err := validModuleName(bad); err == nil {
			t.Errorf("%q 应非法", bad)
		}
	}
}

func TestNaming(t *testing.T) {
	cases := map[string][3]string{ // 输入 -> pascal, camel, title
		"published_at": {"PublishedAt", "publishedAt", "Published At"},
		"owner_id":     {"OwnerID", "ownerId", "Owner Id"},
		"title":        {"Title", "title", "Title"},
		"cover_url":    {"CoverURL", "coverUrl", "Cover Url"},
	}
	for in, w := range cases {
		if g := [3]string{pascalName(in), camelName(in), titleWords(in)}; g != w {
			t.Errorf("%s: got %v want %v", in, g, w)
		}
	}
	for in, want := range map[string]string{"article": "articles", "category": "categories", "box": "boxes", "day": "days", "news": "newses", "church": "churches"} {
		if g := pluralName(in); g != want {
			t.Errorf("plural(%s)=%s want %s", in, g, want)
		}
	}
}

func TestTypeMapping(t *testing.T) {
	type row struct{ kind, model, view, form, ts, control, my, pg string }
	rows := []row{
		{kString, "string", "string", "string", "string", "Input", "VARCHAR(255) NOT NULL DEFAULT ''", "VARCHAR(255) NOT NULL DEFAULT ''"},
		{kText, "string", "string", "string", "string", "Textarea", "TEXT", "TEXT"},
		{kInt, "int", "int", "int", "number", "Input", "INT NOT NULL DEFAULT 0", "INT NOT NULL DEFAULT 0"},
		{kFloat, "float64", "float64", "float64", "number", "Input", "DOUBLE NOT NULL DEFAULT 0", "DOUBLE PRECISION NOT NULL DEFAULT 0"},
		{kBool, "bool", "bool", "bool", "boolean", "Switch", "TINYINT(1) NOT NULL DEFAULT 0", "BOOLEAN NOT NULL DEFAULT FALSE"},
		{kDate, "*time.Time", "*time.Time", "*string", "string | null", "DatePicker", "DATE NULL", "DATE NULL"},
		{kDateTime, "*time.Time", "*time.Time", "*string", "string | null", "DateTimePicker", "DATETIME(3) NULL", "TIMESTAMP(3) NULL"},
		{kDict, "string", "string", "string", "string", "DictSelect", "VARCHAR(64) NOT NULL DEFAULT ''", "VARCHAR(64) NOT NULL DEFAULT ''"},
		{kRef, "idgen.ID", "idgen.ID", "idgen.ID", "string", "RemoteSelect", "BIGINT NOT NULL DEFAULT 0", "BIGINT NOT NULL DEFAULT 0"},
		{kImage, "string", "json.RawMessage", "json.RawMessage", "UploadedFile[]", "Uploader", "TEXT", "TEXT"},
		{kFile, "string", "json.RawMessage", "json.RawMessage", "UploadedFile[]", "Uploader", "TEXT", "TEXT"},
	}
	for _, r := range rows {
		got := row{r.kind, goModelType(r.kind), goViewType(r.kind), goFormType(r.kind), tsType(r.kind), controlFor(r.kind),
			sqlType(r.kind, "mysql"), sqlType(r.kind, "postgres")}
		if got != r {
			t.Errorf("%s:\n got  %+v\n want %+v", r.kind, got, r)
		}
		if sqlType(r.kind, "mariadb") != r.my {
			t.Errorf("%s: mariadb 应与 mysql 一致", r.kind)
		}
	}
	if refURL("user") != "/users" || refLabelKey("user") != "nickname" || refLabelKey("dept") != "name" {
		t.Error("ref 映射不对")
	}
}

func mustSpec(t *testing.T, fields string, sortable bool) *Spec {
	t.Helper()
	fs, err := parseFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	s, err := buildSpec("example.com/demo", "article", fs, sortable, "001_article.sql")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuildSpecSort(t *testing.T) {
	s := mustSpec(t, "title:string", true)
	last := s.Fields[len(s.Fields)-1]
	if last.Name != "sort" || !last.IsSort || len(s.FormFields()) != 1 {
		t.Errorf("--sortable 应自动补 sort 列且不进表单: %+v", s.Fields)
	}
	s = mustSpec(t, demoFields, true)
	if len(s.Fields) != 6 || len(s.FormFields()) != 5 {
		t.Errorf("已声明 sort 时不应重复添加: %d/%d", len(s.Fields), len(s.FormFields()))
	}
	s = mustSpec(t, demoFields, false)
	if len(s.FormFields()) != 6 {
		t.Error("非 sortable 时 sort 是普通字段")
	}
	if _, err := buildSpec("m", "article", []Field{{"sort", kString, ""}}, true, "001_a.sql"); err == nil {
		t.Error("sortable 时 sort 非 int 应报错")
	}
	if s.Fields[0].Mobile != "title" || s.Fields[1].Mobile != "sub" {
		t.Errorf("手机卡片角色: %q %q", s.Fields[0].Mobile, s.Fields[1].Mobile)
	}
}

// 各种字段组合渲染出的 Go 都能通过 gofmt（且已是 gofmt 稳定形态）；JSON / SQL 合规。
func TestRenderGofmtAndAssets(t *testing.T) {
	for name, tc := range map[string]struct {
		fields   string
		sortable bool
	}{
		"full":     {demoFields + ",tags:file,ok:bool,intro:text,born:date,price:float", true},
		"plain":    {demoFields, false},
		"minimal":  {"", false},
		"noString": {"n:int,flag:bool", true},
	} {
		s := mustSpec(t, tc.fields, tc.sortable)
		files, err := renderModule(s)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		goN := 0
		for rel, data := range files {
			switch {
			case strings.HasSuffix(rel, ".go"):
				goN++
				out, err := format.Source(data)
				if err != nil || !bytes.Equal(out, data) {
					t.Errorf("%s: %s 不是 gofmt 稳定的: %v", name, rel, err)
				}
			case strings.HasSuffix(rel, ".json"):
				var m map[string]any
				if err := json.Unmarshal(data, &m); err != nil || m["article"] == nil {
					t.Errorf("%s: %s 不是合法 JSON 或命名空间不是模块名: %v", name, rel, err)
				}
			case strings.HasSuffix(rel, ".sql"):
				up := strings.ToUpper(string(data))
				for _, bad := range []string{"INSERT", "UPDATE ", "DELETE", "IF NOT EXISTS"} {
					if strings.Contains(up, bad) {
						t.Errorf("%s: %s 含 %s", name, rel, bad)
					}
				}
				if !strings.Contains(up, "CREATE TABLE ARTICLE") {
					t.Errorf("%s: %s 缺少建表", name, rel)
				}
			}
			if strings.HasSuffix(rel, ".tsx") || strings.HasSuffix(rel, ".ts") {
				for _, bad := range []string{" ml-", " mr-", "text-left", "text-right", "bg-blue", "text-red", "#fff", "[["} {
					if strings.Contains(string(data), bad) {
						t.Errorf("%s: %s 含违反前端约定的 %q", name, rel, bad)
					}
				}
			}
		}
		if goN != 7 || len(files) != 7+5+2+3 {
			t.Errorf("%s: 文件数不对 go=%d total=%d", name, goN, len(files))
		}
	}
}

func TestRenderContent(t *testing.T) {
	s := mustSpec(t, demoFields, true)
	files, err := renderModule(s)
	if err != nil {
		t.Fatal(err)
	}
	get := func(rel string) string { return string(files[rel]) }
	mustHave := map[string][]string{
		"internal/modules/article/module.go":          {`core.Perm("article:list")`, `core.Perm("article:edit")`, `core.Perm("article:delete")`, `"/articles/sort"`, "core.RegisterBuiltin", "SeedOnStartup"},
		"internal/modules/article/model.go":           {"database.TenantModel", `gorm:"column:owner_id"`, `gorm:"column:owner_id"`, "*time.Time"},
		"internal/modules/article/service.go":         {"func (s *Service) Sort(", "sort ASC, id ASC", "Conflict", "title LIKE ?"},
		"internal/modules/article/seed.go":            {`"article:list"`, `"article:edit"`, `"article:delete"`, "seed.Menus"},
		"web/src/pages/article/ArticlePage.tsx":       {"FilterBar", "DataTable", "DictSelect", "DictTag", "sortable=", "pagination={false}", "t('article.menu')"},
		"web/src/pages/article/ArticleFormDialog.tsx": {"ResponsiveDialog", "<DictSelect dict=\"sys_status\"", "<RemoteSelect url=\"/users\"", "<DateTimePicker", "<Uploader kind=\"image\""},
		"web/src/pages/article/route.tsx":             {"path: 'article'", "perm: 'article:list'"},
		"web/src/pages/article/api.ts":                {"useSortArticles", "'/articles/sort'"},
		"web/src/pages/article/article.mock.ts":       {"defineMock", "'/articles/sort'"},
	}
	for rel, subs := range mustHave {
		for _, sub := range subs {
			if !strings.Contains(get(rel), sub) {
				t.Errorf("%s 应包含 %q", rel, sub)
			}
		}
	}
	// 非 sortable：不生成排序接口、分页列表
	plain, _ := renderModule(mustSpec(t, "title:string", false))
	if strings.Contains(string(plain["internal/modules/article/module.go"]), "/sort") ||
		!strings.Contains(string(plain["web/src/pages/article/ArticlePage.tsx"]), "pageSize") ||
		strings.Contains(string(plain["web/src/pages/article/ArticlePage.tsx"]), "sortable=") {
		t.Error("非 sortable 不应出现排序接口 / 拖动，应有分页")
	}
	// 迁移 SQL：三份，postgres 类型不同
	my, pg := get("migrations/business/mysql/001_article.sql"), get("migrations/business/postgres/001_article.sql")
	if my != get("migrations/business/mariadb/001_article.sql") || my == pg || !strings.Contains(pg, "TIMESTAMP(3)") || !strings.Contains(my, "DATETIME(3)") {
		t.Error("迁移 SQL 引擎差异不对")
	}
	if !strings.Contains(my, "idx_article_tenant ON article (tenant_id, sort)") {
		t.Errorf("sortable 应建 (tenant_id, sort) 索引:\n%s", my)
	}
}

func TestMigrationNumbering(t *testing.T) {
	root := t.TempDir()
	if n, err := nextMigrationNumber(root); err != nil || n != 1 {
		t.Fatalf("空目录应从 1 开始: %d %v", n, err)
	}
	wfile(t, root, "migrations/business/mysql/001_a.sql", "x")
	wfile(t, root, "migrations/business/mysql/002_b.sql", "x")
	wfile(t, root, "migrations/business/postgres/007_c.sql", "x")
	wfile(t, root, "migrations/business/mariadb/README", "x")
	if n, _ := nextMigrationNumber(root); n != 8 {
		t.Errorf("应取三个目录的最大编号 + 1，得 %d", n)
	}
	if migrationFile(8, "article") != "008_article.sql" {
		t.Error("编号应补零到 3 位")
	}
}

func TestUpdateModulesFileIdempotent(t *testing.T) {
	src := []byte(strings.ReplaceAll(modulesGoTemplate, "^", "`"))
	a, ch, err := updateModulesFile(src, "example.com/demo/internal/modules/b")
	if err != nil || !ch {
		t.Fatal(err, ch)
	}
	a, _, _ = updateModulesFile(a, "example.com/demo/internal/modules/a")
	again, ch, _ := updateModulesFile(a, "example.com/demo/internal/modules/a")
	if ch || !bytes.Equal(again, a) {
		t.Error("重复追加应原样返回")
	}
	s := string(a)
	if strings.Count(s, "internal/modules/a\"") != 1 || strings.Index(s, "modules/a\"") > strings.Index(s, "modules/b\"") {
		t.Errorf("应去重并排序:\n%s", s)
	}
	if out, err := format.Source(a); err != nil || !bytes.Equal(out, a) {
		t.Errorf("结果应是 gofmt 稳定的: %v\n%s", err, s)
	}
	if _, _, err := updateModulesFile([]byte("package modules\n"), "x"); err == nil {
		t.Error("缺少标记行应报错")
	}
	// main.go 导入：幂等
	main := []byte("package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/demo/core\"\n)\n\nfunc main() { fmt.Println(core.X) }\n")
	m1, ch, err := ensureMainImport(main, "example.com/demo/internal/modules")
	if err != nil || !ch || !strings.Contains(string(m1), `_ "example.com/demo/internal/modules"`) {
		t.Fatalf("应加入 main.go 导入: %v\n%s", err, m1)
	}
	if _, ch, _ := ensureMainImport(m1, "example.com/demo/internal/modules"); ch {
		t.Error("main.go 已有导入时不应改动")
	}
}

func makeProject(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	wfile(t, r, "go.mod", "module example.com/demo\n\ngo 1.25.0\n")
	wfile(t, r, "plugins/plugins.go", tplPlugins)
	wfile(t, r, "cmd/server/main.go", "package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() { fmt.Println() }\n")
	return r
}

func TestAddModuleEndToEnd(t *testing.T) {
	root := makeProject(t)
	var out bytes.Buffer
	cwd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd) //nolint:errcheck
	if err := run([]string{"add", "module", "article", "--fields", "title:string,sort:int", "--sortable"}, &out); err != nil {
		t.Fatal(err)
	}
	if o := out.String(); !strings.Contains(o, "重启服务即自动执行迁移") || strings.Contains(o, "手动执行迁移 SQL 并在菜单管理里添加菜单") {
		t.Errorf("提示应说明重启即自动迁移与补菜单，且不再要求手动处理:\n%s", o)
	}
	if err := run([]string{"--fields", "x:int", "add", "module", "tag"}, &out); err == nil {
		t.Error("add 之后才是子命令，前置 flag 不应被接受")
	}
	if err := run([]string{"add", "module", "tag", "--fields", "name:string"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"add", "module", "article"}, &out); err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Errorf("重复生成应拒绝覆盖: %v", err)
	}
	if err := run([]string{"add", "module", "Bad-Name"}, &out); err == nil {
		t.Error("非法模块名应报错")
	}
	mods := readStr(t, root, "internal/modules/modules.go")
	if strings.Count(mods, `_ "example.com/demo/internal/modules/article"`) != 1 || !strings.Contains(mods, `modules/tag"`) {
		t.Errorf("modules.go 导入不对:\n%s", mods)
	}
	if !strings.Contains(readStr(t, root, "cmd/server/main.go"), `_ "example.com/demo/internal/modules"`) {
		t.Error("main.go 应空导入 internal/modules")
	}
	for _, f := range []string{"migrations/business/mysql/001_article.sql", "migrations/business/mariadb/001_article.sql", "migrations/business/postgres/001_article.sql",
		"migrations/business/mysql/002_tag.sql", "web/src/pages/tag/TagPage.tsx", "web/src/pages/article/locales/zh-CN.json"} {
		if !has(root, f) {
			t.Errorf("缺少 %s", f)
		}
	}
	if has(root, "migrations/business/mysql/003_tag.sql") {
		t.Error("编号应递增而不是重复")
	}
	if err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".go") {
			raw, _ := os.ReadFile(p)
			if o, ferr := format.Source(raw); ferr != nil || !bytes.Equal(o, raw) {
				t.Errorf("%s 未通过 gofmt: %v", p, ferr)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
