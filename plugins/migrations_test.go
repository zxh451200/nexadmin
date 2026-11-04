package plugins_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 涓夊紩鎿庤縼绉荤粨鏋勪竴鑷存€ч棬绂侊紙01 鍙锋柟妗堥獙鏀堕棬绂佺 5 鏉＄殑鍙嚜鍔ㄥ寲閮ㄥ垎锛夛細
// 瀵规瘡涓甫 migrations/ 鐨勬彃浠讹紝瑙ｆ瀽 mysql/mariadb/postgres 涓変唤 SQL锛?// 鏂█ 琛ㄩ泦鍚堛€佹瘡寮犺〃鐨勫垪鍚嶉泦鍚堛€佺储寮曞悕鍙婂叾鍒楅泦 瀹屽叏涓€鑷淬€?// 绫诲瀷宸紓锛圱INYINT(1) vs BOOLEAN銆丏ATETIME(3) vs TIMESTAMP锛夋槸鍚堟硶鏂硅█宸紓锛屼笉姣斿銆?// 鐪熷疄涓夊簱鎵ц涓庡苟鍙戦獙璇佷粛闇€閮ㄧ讲渚ц鐩栵紙瑙佷氦鎺ヨ鏄庯級銆?
type tableSchema struct {
	columns map[string]bool
	// indexName 鈫?(columns, unique)
	indexes map[string]indexDef
}

type indexDef struct {
	columns []string
	unique  bool
}

type engineSchema struct {
	tables map[string]*tableSchema
}

var (
	reTableName  = regexp.MustCompile(`(?s)CREATE TABLE\s+(\S+)\s*\((.*?)\)\s*;`)
	reIndexStmt  = regexp.MustCompile(`(?i)CREATE\s+(UNIQUE\s+)?INDEX\s+(\S+)\s+ON\s+(\S+)\s*\(([^)]*)\)`)
	reInlineKeys = regexp.MustCompile(`(?i)^\s*(PRIMARY KEY|UNIQUE KEY|KEY|INDEX|CONSTRAINT)\b(.*)`)
	reIdent      = regexp.MustCompile(`^[\x60"]?([A-Za-z_][A-Za-z0-9_]*)[\x60"]?`)
	uniqueColRe  = regexp.MustCompile(`(?i)\bUNIQUE\b`)
)

func parseSchemaSQL(t *testing.T, sqlText string) *engineSchema {
	t.Helper()
	schema := &engineSchema{tables: map[string]*tableSchema{}}
	for _, m := range reTableName.FindAllStringSubmatch(sqlText, -1) {
		name := strings.Trim(m[1], "`\"")
		ts := &tableSchema{columns: map[string]bool{}, indexes: map[string]indexDef{}}
		body := m[2]
		for _, line := range strings.Split(body, "\n") {
			if km := reInlineKeys.FindStringSubmatch(line); km != nil {
				rest := km[2]
				open := strings.Index(rest, "(")
				close := strings.LastIndex(rest, ")")
				if open < 0 || close <= open {
					continue
				}
				cols := splitColumns(rest[open+1 : close])
				// 唯一性按整行判定：UNIQUE KEY（mysql）与 CONSTRAINT ... UNIQUE（PG 表约束）都含 UNIQUE。
				unique := strings.Contains(strings.ToUpper(line), "UNIQUE")
				if idxName := firstIdent(strings.TrimSpace(km[2][:open])); idxName != "" {
					ts.indexes[strings.ToLower(idxName)] = indexDef{columns: cols, unique: unique}
				}
				continue
			}
			if cm := reIdent.FindStringSubmatch(strings.TrimSpace(line)); cm != nil {
				col := strings.ToLower(cm[1])
				ts.columns[col] = true
				// PG 方言的列内联 UNIQUE（如 code VARCHAR(16) NOT NULL UNIQUE）视作单列唯一索引。
				if uniqueColRe.MatchString(line) {
					ts.indexes["__col_"+col] = indexDef{columns: []string{col}, unique: true}
				}
			}
		}
		schema.tables[name] = ts
	}
	for _, m := range reIndexStmt.FindAllStringSubmatch(sqlText, -1) {
		table := strings.Trim(m[3], "`\"")
		ts := schema.tables[table]
		if ts == nil {
			t.Fatalf("index %s references unknown table %s", m[2], table)
		}
		ts.indexes[strings.ToLower(strings.Trim(m[2], "`\""))] = indexDef{
			columns: splitColumns(m[4]), unique: strings.TrimSpace(m[1]) != "",
		}
	}
	return schema
}

func firstIdent(s string) string {
	if m := reIdent.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		return m[1]
	}
	return ""
}

func splitColumns(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if name := firstIdent(part); name != "" {
			out = append(out, strings.ToLower(name))
		}
	}
	sort.Strings(out)
	return out
}

func readEngineSQL(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var buf strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		buf.WriteString("\n")
		buf.Write(raw)
	}
	return buf.String()
}

func TestPluginMigrationsConsistentAcrossEngines(t *testing.T) {
	root := "."
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "plugins.go" {
			continue
		}
		migDir := filepath.Join(root, e.Name(), "migrations")
		if _, err := os.Stat(migDir); err != nil {
			continue
		}
		engines := map[string]*engineSchema{}
		for _, engine := range []string{"mysql", "mariadb", "postgres"} {
			dir := filepath.Join(migDir, engine)
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("plugin %s: missing migration dir %s", e.Name(), engine)
			}
			engines[engine] = parseSchemaSQL(t, readEngineSQL(t, dir))
		}
		compareEngines(t, e.Name(), engines["mysql"], engines["mariadb"])
		compareEngines(t, e.Name(), engines["mysql"], engines["postgres"])
		checked++
	}
	if checked == 0 {
		t.Fatal("no plugin migrations found 鈥?gate ran against nothing")
	}
	t.Logf("migration consistency checked across %d plugins", checked)
}

// indexSignature 用「唯一性 + 列集」做索引的语义指纹：索引名是各引擎的实现细节
// （PG 的列内联 UNIQUE 会自动命名），列集与唯一性才是需要跨引擎一致的契约。
func indexSignature(def indexDef) string {
	return "unique=" + boolLabel(def.unique) + ";cols=" + strings.Join(def.columns, ",")
}

func boolLabel(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func indexSignatures(ts *tableSchema) map[string]bool {
	out := map[string]bool{}
	for _, def := range ts.indexes {
		out[indexSignature(def)] = true
	}
	return out
}

func compareEngines(t *testing.T, plugin string, a, b *engineSchema) {
	t.Helper()
	label := func(tag string, name string) string {
		return plugin + ": " + tag + " " + name
	}
	for name, ta := range a.tables {
		tb := b.tables[name]
		if tb == nil {
			t.Fatalf("%s", label("table missing in second engine", name))
		}
		if len(ta.columns) != len(tb.columns) {
			t.Fatalf("%s", label("column count mismatch on table", name))
		}
		for col := range ta.columns {
			if !tb.columns[col] {
				t.Fatalf("%s", label("column missing in second engine on table "+name, col))
			}
		}
		sa, sb := indexSignatures(ta), indexSignatures(tb)
		if len(sa) != len(sb) {
			t.Fatalf("%s", label("index count mismatch on table", name))
		}
		for sig := range sa {
			if !sb[sig] {
				t.Fatalf("%s", label("index signature missing in second engine on table "+name, sig))
			}
		}
	}
	for name := range b.tables {
		if _, ok := a.tables[name]; !ok {
			t.Fatalf("%s", label("table only exists in second engine", name))
		}
	}
}

// 模型基座与 DDL 的列门禁：嵌入 database.PlatformModel 的模型必然写 id / created_at /
// updated_at 三列（TenantModel 再多一列 tenant_id），三个引擎的建表语句里少任何一列都会在
// 运行期炸（MySQL 1054 Unknown column、PG 42703 undefined column）。
//
// 上面那条「三引擎互相比对」的闸照不出这种错：plg_devhub_alarm_event 三份 SQL 里都缺
// updated_at，比对结果完全一致，而 Go 模型每插一行都带着这一列——只有对着 Go 模型比才露馅
// （2026-11-04 由 MySQL 集成测试跑出来）。
func TestPluginModelBaseColumnsExistInDDL(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		migDir := filepath.Join(e.Name(), "migrations")
		if _, err := os.Stat(migDir); err != nil {
			continue
		}
		models, err := baseModels(filepath.Join(e.Name()))
		if err != nil {
			t.Fatalf("plugin %s: %v", e.Name(), err)
		}
		if len(models) == 0 {
			continue
		}
		engines := map[string]*engineSchema{}
		sqlByText := map[string]string{}
		for _, engine := range []string{"mysql", "mariadb", "postgres"} {
			sqlByText[engine] = readEngineSQL(t, filepath.Join(migDir, engine))
			engines[engine] = parseSchemaSQL(t, sqlByText[engine])
			applyAlterColumns(engines[engine], sqlByText[engine])
		}
		for _, m := range models {
			if strings.HasPrefix(m.table, "sys_") {
				continue // 框架表：由 migrations/framework/ 建，插件迁移里没有它的 CREATE
			}
			want := []string{"id", "created_at", "updated_at"}
			if m.tenant {
				want = append(want, "tenant_id")
			}
			for _, engine := range []string{"mysql", "mariadb", "postgres"} {
				ts := engines[engine].tables[m.table]
				if ts == nil {
					t.Errorf("plugin %s: 模型 %s 的表 %s 在 %s 迁移里没有建表语句", e.Name(), m.goType, m.table, engine)
					continue
				}
				for _, col := range want {
					if !ts.columns[col] {
						t.Errorf("plugin %s: %s 嵌入 %s，但 %s 里表 %s 缺列 %s（插入即报 Unknown column）",
							e.Name(), m.goType, m.base, engine, m.table, col)
					}
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("没有模型被检查——闸对空跑")
	}
	t.Logf("base columns checked for %d plugin models", checked)
}

// applyAlterColumns 把 ALTER TABLE ... ADD/DROP COLUMN 叠到 CREATE 的结果上：
// 迁移是按编号累加的（ticket 的 tenant_id 来自 017），只看 CREATE 会把已有列的表误判成缺列。
// 不处理 RENAME/CHANGE（基座列 id / created_at / updated_at / tenant_id 不会被改名）。
var (
	reAlterStmt = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+([\x60"]?[A-Za-z_][A-Za-z0-9_]*[\x60"]?)\s+(.*?);`)
	reAlterAdd  = regexp.MustCompile(`(?is)\bADD\s+(?:COLUMN\s+)?[\x60"]?([A-Za-z_][A-Za-z0-9_]*)[\x60"]?`)
	reAlterDrop = regexp.MustCompile(`(?is)\bDROP\s+(?:COLUMN\s+)?[\x60"]?([A-Za-z_][A-Za-z0-9_]*)[\x60"]?`)
	// ADD/DROP 后面跟着这些词时改的不是列，是约束或索引。
	reAlterNotColumn = map[string]bool{
		"constraint": true, "unique": true, "primary": true, "index": true,
		"key": true, "fulltext": true, "spatial": true, "if": true, "exists": true,
	}
)

func applyAlterColumns(schema *engineSchema, sqlText string) {
	clean := func(s string) string {
		s = regexp.MustCompile(`(?i)\bIF\s+NOT\s+EXISTS\b`).ReplaceAllString(s, " ")
		return regexp.MustCompile(`(?i)\bIF\s+EXISTS\b`).ReplaceAllString(s, " ")
	}
	for _, m := range reAlterStmt.FindAllStringSubmatch(sqlText, -1) {
		table := strings.ToLower(strings.Trim(m[1], "`\""))
		ts := schema.tables[table]
		if ts == nil {
			continue // 不是本插件建的表（框架表由 migrations/framework 负责）
		}
		for _, clause := range strings.Split(m[2], ",") {
			clause = clean(clause)
			re := reAlterAdd
			drop := regexp.MustCompile(`(?i)\bDROP\b`).MatchString(clause)
			if drop {
				re = reAlterDrop
			}
			for _, cm := range re.FindAllStringSubmatch(clause, -1) {
				col := strings.ToLower(cm[1])
				if reAlterNotColumn[col] {
					continue
				}
				if drop {
					delete(ts.columns, col)
					continue
				}
				ts.columns[col] = true
			}
		}
	}
}

type baseModel struct {
	goType string
	base   string
	table  string
	tenant bool
}

// baseModels 在一个插件目录下找「嵌入了数据基座的模型 + 它的 TableName」。
// 靠 ast 而不是编译产物：插件可能没被当前构建标签带上。
func baseModels(dir string) ([]baseModel, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	bases := map[string]string{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.TYPE {
					continue
				}
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, fld := range st.Fields.List {
						if len(fld.Names) > 0 || fld.Tag != nil {
							continue // 具名字段不是基座
						}
						name := embeddedIdent(fld.Type)
						if name == "PlatformModel" || name == "TenantModel" {
							bases[ts.Name.Name] = name
						}
					}
				}
			}
		}
	}
	if len(bases) == 0 {
		return nil, nil
	}
	out := make([]baseModel, 0, len(bases))
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Name.Name != "TableName" {
					continue
				}
				recv := strings.TrimLeft(embeddedIdent(fd.Recv.List[0].Type), "*")
				base, ok := bases[recv]
				if !ok {
					continue
				}
				lit, ok := fd.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(lit.Results) != 1 {
					continue
				}
				bl, ok := lit.Results[0].(*ast.BasicLit)
				if !ok {
					continue
				}
				table, err := strconv.Unquote(bl.Value)
				if err != nil {
					continue
				}
				out = append(out, baseModel{goType: recv, base: base, table: table, tenant: base == "TenantModel"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].table < out[j].table })
	return out, nil
}

func embeddedIdent(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return embeddedIdent(v.X)
	}
	return ""
}

// migrationNumberExemptions 是「已知撞号、但改名代价更大」的显式清单：数据，不是静默跳过。
// 键是 <插件>/<引擎>/<编号>，值必须写清为什么这两份文件不能改名。
//
// website 的 019、020 各有两份，且都已经在存量库里执行过：runner 把版本号记在
// 文件名上（core/migrate/discover.go:43「Name 文件名，同时是版本号」，
// core/migrate/store.go:118 的 PRIMARY KEY (scope, version)），所以把
// 019_post_metrics.sql 改成 022_... 会让那条已执行的 SQL 重新变成「待执行」，
// 在存量库上再跑一遍。宁可留着撞号，也不能改名。
var migrationNumberExemptions = map[string]string{
	"website/mysql/019":    "019_head_code 与 019_post_metrics 都已入库；版本按文件名记账，改名＝已执行 SQL 在存量库重跑",
	"website/mariadb/019":  "同 mysql：两份 019 都已入库，改名会让已执行 SQL 重新变成待执行",
	"website/postgres/019": "同 mysql：两份 019 都已入库，改名会让已执行 SQL 重新变成待执行",
	"website/mysql/020":    "020_post_tags 与 020_title_suffix 都已入库；版本按文件名记账，改名＝已执行 SQL 在存量库重跑",
	"website/mariadb/020":  "同 mysql：两份 020 都已入库，改名会让已执行 SQL 重新变成待执行",
	"website/postgres/020": "同 mysql：两份 020 都已入库，改名会让已执行 SQL 重新变成待执行",
}

// reMigrationVersion 取迁移文件名第一个 `_` 之前的数字段，即这条 SQL 的版本号。
var reMigrationVersion = regexp.MustCompile(`^(\d+)_`)

// TestPluginMigrationFileNumbersAreUnique 把「迁移编号只增不改」从人记得住的约定变成闸：
// 对每个 plugins/<id>/migrations/<engine>/ 目录，同一编号被两个及以上 .sql 共用就失败，
// 除非该 <插件>/<引擎>/<编号> 在 migrationNumberExemptions 里显式登记了理由。
//
// 撞号不该被当成排版问题：三份已存在的结构闸都不看编号
// （TestPluginMigrationsConsistentAcrossEngines 比的是表/列/索引，
// scripts/ci/migration_parity.py 比的是内容），而撞号时 runner 的执行顺序只由
// 文件名排序决定，「019_head_code 先还是 019_post_metrics 先」没人写死过。
//
// 豁免条目也不许烂在列表里：登记的撞号如果已经不存在，同样报出来。
func TestPluginMigrationFileNumbersAreUnique(t *testing.T) {
	plugins, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	usedExemptions := map[string]bool{}
	for _, p := range plugins {
		if !p.IsDir() {
			continue
		}
		migDir := filepath.Join(".", p.Name(), "migrations")
		engines, err := os.ReadDir(migDir)
		if err != nil {
			continue // 没有 migrations/ 的插件不归这条闸管
		}
		for _, engine := range engines {
			if !engine.IsDir() {
				continue
			}
			dir := filepath.Join(migDir, engine.Name())
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("plugin %s: 读 %s: %v", p.Name(), dir, err)
			}
			byVersion := map[string][]string{}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".sql") {
					continue
				}
				m := reMigrationVersion.FindStringSubmatch(f.Name())
				if m == nil {
					continue // 没有数字前缀：内容交给「三引擎结构一致」那条闸看
				}
				byVersion[m[1]] = append(byVersion[m[1]], f.Name())
			}
			checked++

			versions := make([]string, 0, len(byVersion))
			for v := range byVersion {
				versions = append(versions, v)
			}
			sort.Strings(versions)
			for _, v := range versions {
				names := byVersion[v]
				if len(names) < 2 {
					continue
				}
				sort.Strings(names)
				key := p.Name() + "/" + engine.Name() + "/" + v
				if reason, exempt := migrationNumberExemptions[key]; exempt {
					usedExemptions[key] = true
					t.Logf("放行已登记的撞号 %s（%s）：%s", key, strings.Join(names, "、"), reason)
					continue
				}
				t.Errorf("plugin %s 的 %s 迁移目录里编号 %s 被 %d 个文件共用（%s）："+
					"撞号时执行顺序只由文件名排序决定，而改名会让已执行的 SQL 在存量库重跑"+
					"（版本记在文件名上，core/migrate/store.go:118 PRIMARY KEY (scope, version)）；"+
					"新迁移请续到最大编号之后，确实要留撞号的必须进 migrationNumberExemptions 写清理由",
					p.Name(), engine.Name(), v, len(names), strings.Join(names, "、"))
			}
		}
	}
	if checked == 0 {
		t.Fatal("一个 plugins/<id>/migrations/<engine>/ 目录都没扫到——这条编号闸对空跑，不算闸")
	}
	for key := range migrationNumberExemptions {
		if !usedExemptions[key] {
			t.Errorf("migrationNumberExemptions 里登记的 %s 已经不再对应任何撞号，请删掉这条豁免", key)
		}
	}
	t.Logf("迁移编号查重闸检查了 %d 个插件/引擎迁移目录，显式豁免 %d 处历史撞号",
		checked, len(migrationNumberExemptions))
}
