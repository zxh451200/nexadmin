package plugins_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
