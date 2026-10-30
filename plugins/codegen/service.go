package codegen

import (
	"context"
	"fmt"

	"nexadmin/core/response"
)

// Service 是代码生成器的业务层。无状态：读当前库的表结构，或按定义合成；渲染结果全部在内存里。
type Service struct{ reader SchemaReader }

// NewService 创建服务。
func NewService(r SchemaReader) *Service { return &Service{reader: r} }

// TableInfo 是 GET /tables 的一项。
type TableInfo struct {
	Name string `json:"name"`
}

// GenRequest 是 /preview 与 /download 的请求体。Table 与 Define 二选一：已有表给 Table，
// 新建表给 Define（生成物会附带三份迁移）。Columns 只提供每列的展示配置，
// 列的类型 / 可空 / 主键一律由服务端重新读库或按定义合成，不信任客户端。
type GenRequest struct {
	Table    string         `json:"table" binding:"max=64"`
	Module   string         `json:"module" binding:"required,max=64"`
	GoModule string         `json:"goModule" binding:"max=100"`
	Columns  []ColumnConfig `json:"columns" binding:"max=200,dive"`
	Define   *TableDef      `json:"define"`
}

// SchemaRequest 是 /schema 的入参：已有表给 Table，新建表给 Define。
type SchemaRequest struct {
	Table  string    `json:"table" binding:"max=64"`
	Define *TableDef `json:"define"`
}

// badReq 把内部校验错误降级成统一的用户提示；诊断细节只随 Wrap 进日志，不直接回显。
func badReq(err error) error { return response.BadRequest("codegen.invalid_input").Wrap(err) }

// Tables 返回当前库的业务表（已排除 sys_* / plg_* / 迁移记录表等框架表与名字不合规的表）。
func (s *Service) Tables(ctx context.Context) ([]TableInfo, error) {
	all, err := s.reader.Tables(ctx)
	if err != nil {
		return nil, err
	}
	names := businessTables(all)
	out := make([]TableInfo, len(names))
	for i, n := range names {
		out[i] = TableInfo{Name: n}
	}
	return out, nil
}

// analyze 要求表名先过白名单，且必须出现在 Tables() 的结果集里。
func (s *Service) analyze(ctx context.Context, table string) (*Analysis, error) {
	if err := ValidTable(table); err != nil {
		return nil, badReq(err)
	}
	all, err := s.reader.Tables(ctx)
	if err != nil {
		return nil, err
	}
	found := false
	for _, n := range businessTables(all) {
		if n == table {
			found = true
			break
		}
	}
	if !found {
		return nil, response.NotFound("codegen.table_not_found", sanitizeComment(table))
	}
	raw, err := s.reader.Columns(ctx, table)
	if err != nil {
		return nil, err
	}
	a, err := analyze(table, raw)
	if err != nil {
		return nil, badReq(err)
	}
	return a, nil
}

// Columns 返回列信息与默认推荐配置。
func (s *Service) Columns(ctx context.Context, table string) (*TableSchema, error) {
	a, err := s.analyze(ctx, table)
	if err != nil {
		return nil, err
	}
	sc := a.schema()
	return &sc, nil
}

// Schema 返回列信息与默认推荐配置；已有表读库，新建表按定义合成。
func (s *Service) Schema(ctx context.Context, req SchemaRequest) (*TableSchema, error) {
	a, _, err := s.resolve(ctx, req.Table, req.Define)
	if err != nil {
		return nil, err
	}
	sc := a.schema()
	return &sc, nil
}

// resolve 解析入参：已有表读库；新建表用定义合成同形列信息。二选一，且新表模式必须带定义。
func (s *Service) resolve(ctx context.Context, table string, def *TableDef) (*Analysis, *tableDef, error) {
	if def != nil {
		if table != "" {
			return nil, nil, badReq(fmt.Errorf("table and define are mutually exclusive"))
		}
		td, err := def.normalize()
		if err != nil {
			return nil, nil, badReq(err)
		}
		a, err := analyze(td.table, td.rawColumns())
		if err != nil {
			return nil, nil, badReq(err)
		}
		return a, td, nil
	}
	if table == "" {
		return nil, nil, badReq(fmt.Errorf("either table or define is required"))
	}
	a, err := s.analyze(ctx, table)
	if err != nil {
		return nil, nil, err
	}
	return a, nil, nil
}

// Generate 校验请求并渲染全部文件（内存中，不落盘）。
func (s *Service) Generate(ctx context.Context, req GenRequest) (*Output, error) {
	if err := ValidModule(req.Module); err != nil {
		return nil, badReq(err)
	}
	a, def, err := s.resolve(ctx, req.Table, req.Define)
	if err != nil {
		return nil, err
	}
	spec, err := buildSpec(a, req.GoModule, req.Module, req.Columns)
	if err != nil {
		return nil, badReq(err)
	}
	if def != nil {
		spec.Migrations = def.migrations()
	}
	out, err := Render(spec)
	if err != nil {
		return nil, response.Internal().Wrap(err)
	}
	return out, nil
}
