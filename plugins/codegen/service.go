package codegen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Service 是代码生成器的业务层。渲染无状态：读当前库的表结构或按定义合成，结果全在内存里；
// 唯一落库的是「保存的生成配置」（plg_codegen_config），让向导可以存一份下次继续改。
type Service struct {
	reader SchemaReader
	db     *gorm.DB
}

// NewService 创建服务。db 可为 nil（单测只渲染时）：配置读写随之不可用。
func NewService(r SchemaReader, db *gorm.DB) *Service { return &Service{reader: r, db: db} }

// TableInfo 是 GET /tables 的一项。
type TableInfo struct {
	Name string `json:"name"`
}

// SubTableSpec 是主子表模式的子表：Table 必须已有（子表不能凭空定义），
// ForeignKey 是子表里指向主表的 BIGINT *_id 列，子行随主表整存整取。
type SubTableSpec struct {
	Table      string         `json:"table" binding:"max=64"`
	ForeignKey string         `json:"foreignKey" binding:"max=64"`
	Columns    []ColumnConfig `json:"columns" binding:"max=200,dive"`
}

// GenRequest 是 /preview 与 /download 的请求体。Table 与 Define 二选一：已有表给 Table，
// 新建表给 Define（生成物会附带三份迁移）。Columns 只提供每列的展示配置，
// 列的类型 / 可空 / 主键一律由服务端重新读库或按定义合成，不信任客户端。
// Sub 可选：给了就按主子表生成（子行编辑器 + 事务整存整取）。
type GenRequest struct {
	Table    string         `json:"table" binding:"max=64"`
	Module   string         `json:"module" binding:"required,max=64"`
	GoModule string         `json:"goModule" binding:"max=100"`
	Columns  []ColumnConfig `json:"columns" binding:"max=200,dive"`
	Define   *TableDef      `json:"define"`
	Sub      *SubTableSpec  `json:"sub"`
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
	_, out, err := s.generate(ctx, req)
	return out, err
}

// generate 同 Generate，但同时返回已校验的 Spec（安装时要用它的模块路径注册空导入）。
func (s *Service) generate(ctx context.Context, req GenRequest) (*Spec, *Output, error) {
	if err := ValidModule(req.Module); err != nil {
		return nil, nil, badReq(err)
	}
	a, def, err := s.resolve(ctx, req.Table, req.Define)
	if err != nil {
		return nil, nil, err
	}
	spec, err := buildSpec(a, req.GoModule, req.Module, req.Columns)
	if err != nil {
		return nil, nil, badReq(err)
	}
	if def != nil {
		spec.Migrations = def.migrations()
	}
	if req.Sub != nil {
		if req.Sub.Table == a.Table {
			return nil, nil, badReq(fmt.Errorf("sub table must differ from the main table"))
		}
		subA, err := s.analyze(ctx, req.Sub.Table)
		if err != nil {
			return nil, nil, err
		}
		sub, err := buildSubSpec(subA, req.Sub.ForeignKey, req.Sub.Columns, spec.Pascal)
		if err != nil {
			return nil, nil, badReq(err)
		}
		if sub.Tenant && !spec.Tenant {
			return nil, nil, badReq(fmt.Errorf("sub table %q is tenant-scoped but the main table is not", sub.Table))
		}
		spec.Sub = sub
	}
	out, err := Render(spec)
	if err != nil {
		return nil, nil, response.Internal().Wrap(err)
	}
	return spec, out, nil
}

// InstallResult 是直写项目的回执：写入了哪些文件、哪些因已存在被跳过，
// 以及模块是否已注册进 internal/modules/modules.go。
type InstallResult struct {
	Root     string   `json:"root"`
	Written  []string `json:"written"`
	Skipped  []string `json:"skipped"`
	// Registered 表示本次是否新写了 modules.go 的空导入。
	Registered bool `json:"registered"`
	// RegisterNote 说明未写入的原因：already（已有）/ no_file / no_markers / failed。
	RegisterNote  string `json:"registerNote,omitempty"`
	RegisterError string `json:"registerError,omitempty"`
}

// projectRoot 解析写入根目录：优先 NEXADMIN_ROOT 环境变量，否则用后端进程的工作目录
// （dev 模式 `go run ./cmd/server` 就是仓库根）。只有看起来像项目根（有 go.mod）的
// 目录才接受——写错地方比写不了更糟。
func projectRoot() (string, error) {
	root := os.Getenv("NEXADMIN_ROOT")
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = wd
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("%q has no go.mod; set NEXADMIN_ROOT to the project root", root)
	}
	return filepath.Abs(root)
}

// detectGoModule 是「Go 模块路径」留空时的 import 前缀：从项目根向上找最近的 go.mod，读它的 module 行。
// 母版里得到 nexadmin（与旧默认一致），派生项目里得到用户自己的模块路径——
// 写死 nexadmin 会让二开项目生成的代码 import 一个根本不存在的包。
// 找不到 go.mod 时报错而不是悄悄回落 nexadmin：悄悄生成的代码要到编译那一刻才炸，
// 而且报的是「package nexadmin/... does not exist」，与真正的原因（根目录没定位到）隔了一层。
func detectGoModule() (string, error) {
	dir, err := projectRoot()
	if err != nil {
		if dir, err = os.Getwd(); err != nil {
			return "", response.BadRequest("codegen.install_no_root").Wrap(err)
		}
	}
	for d := filepath.Clean(dir); ; {
		if m, ok := goModuleLine(filepath.Join(d, "go.mod")); ok {
			return m, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return "", response.BadRequest("codegen.install_no_root").Wrap(fmt.Errorf("%q 往上没有 go.mod", filepath.Clean(dir)))
		}
		d = up
	}
}

// goModuleLine 读 go.mod 的 module 行（只认 `module <路径>` 单行写法）。
func goModuleLine(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`), true
		}
	}
	return "", false
}

// Install 把渲染结果写进项目根目录；已存在的文件默认跳过（overwrite=false）。
// register=true 时再把模块空导入写进 internal/modules/modules.go 的标记区（幂等）：
// 生成的模块靠 init 里的 RegisterBuiltin 挂路由，不注册的话接口在重启后依然 404。
// 路径在渲染层已过白名单，这里再核一道并确保没有逃逸出根目录。
func (s *Service) Install(ctx context.Context, req GenRequest, overwrite, register bool) (*InstallResult, error) {
	spec, out, err := s.generate(ctx, req)
	if err != nil {
		return nil, err
	}
	root, err := projectRoot()
	if err != nil {
		return nil, response.BadRequest("codegen.install_no_root").Wrap(err)
	}
	res := &InstallResult{Root: root, Written: []string{}, Skipped: []string{}}
	for _, f := range out.Files {
		if !validOutPath(f.Path) {
			return nil, response.Internal().Wrap(fmt.Errorf("unsafe output path %q", f.Path))
		}
		full := filepath.Join(root, filepath.FromSlash(f.Path))
		if rel, err := filepath.Rel(root, full); err != nil || rel == ".." || filepath.IsAbs(rel) {
			return nil, response.Internal().Wrap(fmt.Errorf("output path escapes project root: %q", f.Path))
		}
		if !overwrite {
			if _, err := os.Stat(full); err == nil {
				res.Skipped = append(res.Skipped, f.Path)
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			return nil, err
		}
		res.Written = append(res.Written, f.Path)
	}
	if register {
		changed, note, rerr := registerModule(root, spec.GoModule+"/internal/modules/"+spec.Module)
		res.Registered, res.RegisterNote = changed, note
		if rerr != nil {
			// 文件已经写下去了：不让整个安装回执变成失败，但把原因如实带回界面。
			res.RegisterNote = "failed"
			res.RegisterError = rerr.Error()
		}
	}
	return res, nil
}

const (
	modulesMarkBegin = "// nexadmin:modules:begin"
	modulesMarkEnd   = "// nexadmin:modules:end"
)

// registerModule 在 internal/modules/modules.go 的标记区里幂等追加空导入。
// 未改写时 note 说明原因（already / no_file / no_markers），不当作错误。
func registerModule(root, importPath string) (changed bool, note string, err error) {
	file := filepath.Join(root, "internal", "modules", "modules.go")
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "no_file", nil
	}
	if err != nil {
		return false, "", err
	}
	line := fmt.Sprintf(`_ %q`, importPath)
	lines := strings.Split(string(src), "\n")
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case modulesMarkBegin:
			begin = i
		case modulesMarkEnd:
			end = i
		}
	}
	if begin < 0 || end <= begin {
		return false, "no_markers", nil
	}
	specs := make([]string, 0, end-begin)
	for _, l := range lines[begin+1 : end] {
		if t := strings.TrimSpace(l); t != "" {
			if t == line {
				return false, "already", nil
			}
			specs = append(specs, t)
		}
	}
	specs = append(specs, line)
	sort.Strings(specs)
	out := make([]string, 0, len(lines))
	out = append(out, lines[:begin+1]...)
	for _, s := range specs {
		out = append(out, "\t"+s)
	}
	out = append(out, lines[end:]...)
	formatted, err := format.Source([]byte(strings.Join(out, "\n")))
	if err != nil {
		return false, "", err
	}
	if err := os.WriteFile(file, formatted, 0o644); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// ---- 保存的生成配置 ----

// maxConfigs 是配置存量的上限：配置是给人工复用的小数据，超了就该清理，而不是无限堆。
const maxConfigs = 200

// ConfigForm 是保存生成配置的入参；Table 与 Define 二选一，与 GenRequest 同构。
type ConfigForm struct {
	Name     string         `json:"name" binding:"required,max=64"`
	Module   string         `json:"module" binding:"required,max=32"`
	GoModule string         `json:"goModule" binding:"max=100"`
	Table    string         `json:"table" binding:"max=64"`
	Define   *TableDef      `json:"define"`
	Columns  []ColumnConfig `json:"columns" binding:"max=200,dive"`
	Sub      *SubTableSpec  `json:"sub"`
}

// SavedConfig 是载入配置所需的最小形状（前端据此还原向导状态）。
type SavedConfig struct {
	ID       idgen.ID       `json:"id"`
	Name     string         `json:"name"`
	Module   string         `json:"module"`
	GoModule string         `json:"goModule"`
	Table    string         `json:"table"`
	Define   *TableDef      `json:"define,omitempty"`
	Columns  []ColumnConfig `json:"columns"`
	Sub      *SubTableSpec  `json:"sub,omitempty"`
}

// ListConfigs 列出全部保存的配置（按更新时间倒序）。
func (s *Service) ListConfigs(ctx context.Context) ([]SavedConfig, error) {
	if s.db == nil {
		return nil, response.Internal()
	}
	var rows []CodegenConfig
	if err := s.db.WithContext(ctx).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SavedConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.view())
	}
	return out, nil
}

// SaveConfig 按名字保存（同名即更新）。上限 maxConfigs，防止无人清理的配置无限增长。
func (s *Service) SaveConfig(ctx context.Context, f ConfigForm) (*SavedConfig, error) {
	if s.db == nil {
		return nil, response.Internal()
	}
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		return nil, badReq(fmt.Errorf("config name is required"))
	}
	if err := ValidModule(f.Module); err != nil {
		return nil, badReq(err)
	}
	if f.GoModule != "" {
		if err := ValidGoModule(f.GoModule); err != nil {
			return nil, badReq(err)
		}
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&CodegenConfig{}).Count(&n).Error; err != nil {
		return nil, err
	}
	defineJSON, columnsJSON, subJSON := "", "", ""
	if f.Define != nil {
		td, err := f.Define.normalize()
		if err != nil {
			return nil, badReq(err)
		}
		raw, err := json.Marshal(f.Define)
		if err != nil {
			return nil, response.Internal().Wrap(err)
		}
		defineJSON = string(raw)
		f.Table = td.table
	}
	if len(f.Columns) > 0 {
		raw, err := json.Marshal(f.Columns)
		if err != nil {
			return nil, response.Internal().Wrap(err)
		}
		columnsJSON = string(raw)
	}
	if f.Sub != nil {
		raw, err := json.Marshal(f.Sub)
		if err != nil {
			return nil, response.Internal().Wrap(err)
		}
		subJSON = string(raw)
	}
	var row CodegenConfig
	err := s.db.WithContext(ctx).Where("name = ?", f.Name).First(&row).Error
	switch {
	case err == nil:
		row.Module, row.GoModule, row.Table = f.Module, f.GoModule, f.Table
		row.DefineJSON, row.ColumnsJSON, row.SubJSON = defineJSON, columnsJSON, subJSON
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return nil, err
		}
	case isNotFound(err):
		if n >= maxConfigs {
			return nil, response.BadRequest("codegen.config_too_many")
		}
		row = CodegenConfig{
			Name: f.Name, Module: f.Module, GoModule: f.GoModule, Table: f.Table,
			DefineJSON: defineJSON, ColumnsJSON: columnsJSON, SubJSON: subJSON,
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	v := row.view()
	return &v, nil
}

// DeleteConfig 删除一份保存的配置；不存在的 ID 忽略。
func (s *Service) DeleteConfig(ctx context.Context, id idgen.ID) error {
	if s.db == nil {
		return response.Internal()
	}
	return s.db.WithContext(ctx).Delete(&CodegenConfig{}, "id = ?", int64(id)).Error
}

// DeleteConfigs 批量删除保存的生成配置。逐条判断与单条 DeleteConfig 完全一致：一行配置自包含
// （表定义 / 列配置 / 子表都在同一行的 JSON 列里），既不引用别的表也没有「内置不可删」这类护栏，
// 所以批量也不新增判断；一条 DELETE 在 GORM 自带事务里走完，不会出现「删了一半」。
// plg_codegen_config 是平台级表（PlatformModel 没有 tenant_id 列），这里可钉的范围就是点名的 id 集合：
// 去掉 id IN 条件等于清掉所有人存过的配置。越权 / 已不存在的 id 删不到，也不计入实删数；
// 0 命中报不存在，不静默返回 deleted=0 让界面以为删成功了。
func (s *Service) DeleteConfigs(ctx context.Context, ids []idgen.ID) (int64, error) {
	if s.db == nil {
		return 0, response.Internal()
	}
	list := make([]int64, 0, len(ids))
	for _, id := range ids {
		list = append(list, int64(id))
	}
	res := s.db.WithContext(ctx).Where("id IN ?", list).Delete(&CodegenConfig{})
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("error.not_found")
	}
	return res.RowsAffected, nil
}

func isNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// view 把存储行还原成可载入的配置；JSON 损坏按缺失处理（配置坏了不该挡住列表）。
func (r CodegenConfig) view() SavedConfig {
	v := SavedConfig{ID: r.ID, Name: r.Name, Module: r.Module, GoModule: r.GoModule, Table: r.Table}
	if r.DefineJSON != "" {
		var d TableDef
		if json.Unmarshal([]byte(r.DefineJSON), &d) == nil {
			v.Define = &d
		}
	}
	if r.ColumnsJSON != "" {
		_ = json.Unmarshal([]byte(r.ColumnsJSON), &v.Columns)
	}
	if r.SubJSON != "" {
		var s SubTableSpec
		if json.Unmarshal([]byte(r.SubJSON), &s) == nil {
			v.Sub = &s
		}
	}
	return v
}
