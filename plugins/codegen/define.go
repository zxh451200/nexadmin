package codegen

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// TableDef 是向导里定义的新表。生成物附带三引擎迁移，插件本身不建表、不写磁盘。
type TableDef struct {
	Name       string     `json:"name"`
	Tenant     bool       `json:"tenant"`
	SoftDelete bool       `json:"softDelete"`
	Migration  string     `json:"migration"`
	Fields     []FieldDef `json:"fields"`
}

// FieldDef 是新表的一列。Length 只对 string 生效；Required 只对可判空的类型生效。
type FieldDef struct {
	Name     string `json:"name"`
	Kind     Kind   `json:"kind"`
	Length   int    `json:"length"`
	Required bool   `json:"required"`
	Comment  string `json:"comment"`
}

const (
	bizPrefix      = "biz_"
	maxDefFields   = 60
	defaultVarchar = 255
	maxVarchar     = 4000
)

var (
	migrationRe = regexp.MustCompile(`^[0-9]{1,4}$`)
	defKinds    = map[Kind]bool{
		KindString: true, KindText: true, KindInt: true, KindFloat: true,
		KindBool: true, KindDate: true, KindDateTime: true,
	}
	systemFieldNames = map[string]bool{
		"id": true, "tenant_id": true, "created_at": true, "updated_at": true, "deleted_at": true,
	}
	migrationEngines = []string{"mysql", "mariadb", "postgres"}
)

// defField 是校验后的字段，模板、迁移 SQL 和 RawColumn 都从这里派生。
type defField struct {
	name     string
	kind     Kind
	length   int
	required bool
	comment  string
}

// tableDef 是校验后的表定义。
type tableDef struct {
	table      string
	tenant     bool
	softDelete bool
	number     string
	fields     []defField
}

// normalize 校验表定义；表名统一加 biz_ 前缀，字段名走标识符白名单。
func (d TableDef) normalize() (*tableDef, error) {
	name := strings.TrimSpace(d.Name)
	name = strings.TrimPrefix(name, bizPrefix)
	if err := ValidIdent(name); err != nil {
		return nil, fmt.Errorf("invalid table name %q: %w", d.Name, err)
	}
	table := bizPrefix + name
	if err := ValidTable(table); err != nil {
		return nil, err
	}
	number := strings.TrimSpace(d.Migration)
	if number == "" {
		number = "001"
	}
	if !migrationRe.MatchString(number) {
		return nil, fmt.Errorf("invalid migration number %q", d.Migration)
	}
	n, _ := strconv.Atoi(number)
	if n == 0 {
		return nil, fmt.Errorf("migration number must be greater than zero")
	}
	if len(d.Fields) == 0 {
		return nil, fmt.Errorf("table %q has no fields", table)
	}
	if len(d.Fields) > maxDefFields {
		return nil, fmt.Errorf("table %q has too many fields (%d > %d)", table, len(d.Fields), maxDefFields)
	}
	td := &tableDef{table: table, tenant: d.Tenant, softDelete: d.SoftDelete, number: fmt.Sprintf("%03d", n)}
	seen := map[string]bool{}
	for _, f := range d.Fields {
		fn := strings.TrimSpace(f.Name)
		if err := ValidIdent(fn); err != nil {
			return nil, fmt.Errorf("invalid field name %q: %w", f.Name, err)
		}
		if systemFieldNames[fn] {
			return nil, fmt.Errorf("field %q is a system column", fn)
		}
		if seen[fn] {
			return nil, fmt.Errorf("field %q defined twice", fn)
		}
		seen[fn] = true
		if !defKinds[f.Kind] {
			return nil, fmt.Errorf("field %q has unsupported kind %q", fn, f.Kind)
		}
		length := 0
		switch f.Kind {
		case KindString:
			length = f.Length
			if length == 0 {
				length = defaultVarchar
			}
			if length < 1 || length > maxVarchar {
				return nil, fmt.Errorf("field %q length must be 1-%d", fn, maxVarchar)
			}
		default:
			if f.Length != 0 {
				return nil, fmt.Errorf("field %q: length is only valid for string", fn)
			}
		}
		if f.Required && !requirable(f.Kind) {
			return nil, fmt.Errorf("field %q (%s) cannot be required", fn, f.Kind)
		}
		td.fields = append(td.fields, defField{name: fn, kind: f.Kind, length: length, required: f.Required, comment: sanitizeComment(f.Comment)})
	}
	return td, nil
}

// rawColumns 合成与真实库读取同形的列信息，后续分析与推荐配置完全复用。
func (d *tableDef) rawColumns() []RawColumn {
	out := make([]RawColumn, 0, len(d.fields)+5)
	out = append(out, RawColumn{Name: "id", TypeName: "bigint", PrimaryKey: true})
	if d.tenant {
		out = append(out, RawColumn{Name: "tenant_id", TypeName: "bigint"})
	}
	for _, f := range d.fields {
		c := RawColumn{Name: f.name, TypeName: rawTypeName(f.kind), Comment: f.comment, Length: int64(f.length)}
		switch f.kind {
		case KindString, KindText, KindDate, KindDateTime:
			c.Nullable = !f.required
		default:
			c.HasDefault = true
		}
		out = append(out, c)
	}
	out = append(out, RawColumn{Name: "created_at", TypeName: "datetime"}, RawColumn{Name: "updated_at", TypeName: "datetime"})
	if d.softDelete {
		out = append(out, RawColumn{Name: "deleted_at", TypeName: "datetime", Nullable: true})
	}
	return out
}

func rawTypeName(k Kind) string {
	switch k {
	case KindString:
		return "varchar"
	case KindText:
		return "text"
	case KindInt:
		return "int"
	case KindFloat:
		return "double"
	case KindBool:
		return "bool"
	case KindDate:
		return "date"
	case KindDateTime:
		return "datetime"
	}
	return ""
}

// migrations 生成 mysql / mariadb / postgres 三份结构相同的建表迁移。
func (d *tableDef) migrations() []File {
	out := make([]File, 0, len(migrationEngines))
	for _, engine := range migrationEngines {
		out = append(out, File{
			Path:    path.Join("migrations", "business", engine, d.number+"_"+d.table+".sql"),
			Lang:    "sql",
			Content: d.migrationSQL(engine),
		})
	}
	return out
}

func (d *tableDef) migrationSQL(engine string) string {
	pg := engine == "postgres"
	ts := "DATETIME(3)"
	if pg {
		ts = "TIMESTAMP(3)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "-- 业务表 %s（codegen 生成）；迁移里只建表，不写数据。\n", d.table)
	fmt.Fprintf(&b, "CREATE TABLE %s (\n  id BIGINT NOT NULL,\n", d.table)
	if d.tenant {
		b.WriteString("  tenant_id BIGINT NOT NULL,\n")
	}
	for _, f := range d.fields {
		fmt.Fprintf(&b, "  %s %s,\n", f.name, f.sqlType(engine))
	}
	fmt.Fprintf(&b, "  created_at %s NOT NULL,\n  updated_at %s NOT NULL,\n", ts, ts)
	if d.softDelete {
		fmt.Fprintf(&b, "  deleted_at %s NULL,\n", ts)
	}
	b.WriteString("  PRIMARY KEY (id)\n);\n")
	if d.tenant {
		fmt.Fprintf(&b, "CREATE INDEX idx_%s_tenant ON %s (tenant_id);\n", d.table, d.table)
	}
	return b.String()
}

func (f defField) sqlType(engine string) string {
	pg := engine == "postgres"
	switch f.kind {
	case KindString:
		if f.required {
			return fmt.Sprintf("VARCHAR(%d) NOT NULL", f.length)
		}
		return fmt.Sprintf("VARCHAR(%d) NULL", f.length)
	case KindText:
		if f.required {
			return "TEXT NOT NULL"
		}
		return "TEXT NULL"
	case KindInt:
		if pg {
			return "INTEGER NOT NULL DEFAULT 0"
		}
		return "INT NOT NULL DEFAULT 0"
	case KindFloat:
		if pg {
			return "DOUBLE PRECISION NOT NULL DEFAULT 0"
		}
		return "DOUBLE NOT NULL DEFAULT 0"
	case KindBool:
		if pg {
			return "BOOLEAN NOT NULL DEFAULT FALSE"
		}
		return "TINYINT(1) NOT NULL DEFAULT 0"
	case KindDate:
		if f.required {
			return "DATE NOT NULL"
		}
		return "DATE NULL"
	case KindDateTime:
		ts := "DATETIME(3)"
		if pg {
			ts = "TIMESTAMP(3)"
		}
		if f.required {
			return ts + " NOT NULL"
		}
		return ts + " NULL"
	}
	return ""
}
