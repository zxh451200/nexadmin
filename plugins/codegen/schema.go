package codegen

import (
	"context"
	"sort"

	"gorm.io/gorm"
)

// RawColumn 是从数据库读到的一列原始信息（与具体引擎无关）。
type RawColumn struct {
	Name       string
	TypeName   string // gorm ColumnType.DatabaseTypeName，如 BIGINT / varchar / timestamptz
	FullType   string // 完整类型，如 tinyint(1)；读不到则为空
	Nullable   bool
	PrimaryKey bool
	HasDefault bool
	Comment    string
	Length     int64
}

// SchemaReader 抽象表结构读取，便于单测替身；生产实现只用 GORM Migrator 的引擎无关方法。
type SchemaReader interface {
	Tables(ctx context.Context) ([]string, error)
	Columns(ctx context.Context, table string) ([]RawColumn, error)
}

type gormReader struct{ db *gorm.DB }

// NewGormReader 基于 *gorm.DB 的 Migrator 读取当前库的表与列（MySQL / MariaDB / PostgreSQL 通用）。
func NewGormReader(db *gorm.DB) SchemaReader { return &gormReader{db: db} }

func (r *gormReader) Tables(ctx context.Context) ([]string, error) {
	return r.db.WithContext(ctx).Migrator().GetTables()
}

func (r *gormReader) Columns(ctx context.Context, table string) ([]RawColumn, error) {
	cts, err := r.db.WithContext(ctx).Migrator().ColumnTypes(table)
	if err != nil {
		return nil, err
	}
	out := make([]RawColumn, 0, len(cts))
	for _, ct := range cts {
		c := RawColumn{Name: ct.Name(), TypeName: ct.DatabaseTypeName()}
		if v, ok := ct.ColumnType(); ok {
			c.FullType = v
		}
		if v, ok := ct.Nullable(); ok {
			c.Nullable = v
		}
		if v, ok := ct.PrimaryKey(); ok {
			c.PrimaryKey = v
		}
		if _, ok := ct.DefaultValue(); ok {
			c.HasDefault = true
		}
		if v, ok := ct.Comment(); ok {
			c.Comment = v
		}
		if v, ok := ct.Length(); ok {
			c.Length = v
		}
		out = append(out, c)
	}
	return out, nil
}

// businessTables 过滤出可生成的业务表：名字合法且不是框架表；结果排序去重。
func businessTables(all []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(all))
	for _, t := range all {
		if seen[t] || ValidTable(t) != nil {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
