package codegen

import (
	"context"

	"nexadmin/core/response"
)

// Service 是代码生成器的业务层。无状态：只读当前库的表结构，渲染结果全部在内存里。
type Service struct{ reader SchemaReader }

// NewService 创建服务。
func NewService(r SchemaReader) *Service { return &Service{reader: r} }

// TableInfo 是 GET /tables 的一项。
type TableInfo struct {
	Name string `json:"name"`
}

// GenRequest 是 /preview 与 /download 的请求体。Columns 只提供每列的展示配置，
// 列的类型 / 可空 / 主键一律由服务端重新读库，不信任客户端。
type GenRequest struct {
	Table    string         `json:"table" binding:"required,max=64"`
	Module   string         `json:"module" binding:"required,max=64"`
	GoModule string         `json:"goModule" binding:"max=100"`
	Columns  []ColumnConfig `json:"columns" binding:"max=200,dive"`
}

func badReq(err error) error { return response.BadRequest("error.bad_request").WithMsg(err.Error()) }

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
		return nil, response.NotFound("error.not_found").WithMsg("table not found: " + sanitizeComment(table))
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

// Generate 校验请求并渲染全部文件（内存中，不落盘）。
func (s *Service) Generate(ctx context.Context, req GenRequest) (*Output, error) {
	if err := ValidModule(req.Module); err != nil {
		return nil, badReq(err)
	}
	a, err := s.analyze(ctx, req.Table)
	if err != nil {
		return nil, err
	}
	spec, err := buildSpec(a, req.GoModule, req.Module, req.Columns)
	if err != nil {
		return nil, badReq(err)
	}
	out, err := Render(spec)
	if err != nil {
		return nil, response.Internal().Wrap(err)
	}
	return out, nil
}
