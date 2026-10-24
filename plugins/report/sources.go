package report

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"nexadmin/core/config"
)

// 列类型。
const (
	TypeString = "string"
	TypeNumber = "number"
	TypeTime   = "time"
	TypeBool   = "bool"
)

// 硬上限：配置只能把它们调小。
const (
	MaxLimit       = 5000 // 单次查询/导出的最大行数
	DefaultLimit   = 1000
	maxTimeout     = 60 * time.Second
	defaultTimeout = 10 * time.Second
)

// Column 是白名单里的一列元数据。Name 是真实列名，SQL 里只会出现它（来自元数据，从不来自请求字符串）。
type Column struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

// Source 是「可用于报表的表」。
type Source struct {
	Table string   `json:"table"`
	Label string   `json:"label"`
	Cols  []Column `json:"columns"`

	// TenantCol 是租户列，非空时每次查询必带 TenantCol = 当前租户。Global=true 的表（无租户列的平台级数据）才允许为空。
	TenantCol string `json:"-"`
	Global    bool   `json:"-"`
	// OwnerCol / UsernameCol 是数据范围（core/datascope）可复用的归属列：用户 id 列 / 账号列。二者都是内部列，不必出现在 Cols 里。
	OwnerCol    string `json:"-"`
	UsernameCol string `json:"-"`
	// ScopeRequired=true 且受限操作者面对没有归属列的表时，直接拒绝（默认放行，见设计说明）。
	ScopeRequired bool `json:"-"`

	byName map[string]Column
}

// Col 按精确名字取列。
func (s *Source) Col(name string) (Column, bool) {
	c, ok := s.byName[name]
	return c, ok
}

// Registry 是已校验过的白名单。
type Registry struct {
	sources map[string]*Source
}

// Get 取一张白名单表。
func (r *Registry) Get(table string) (*Source, bool) {
	if r == nil {
		return nil, false
	}
	s, ok := r.sources[table]
	return s, ok
}

// List 按表名排序返回全部白名单表。
func (r *Registry) List() []*Source {
	out := make([]*Source, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Table < out[j].Table })
	return out
}

// Settings 是插件运行参数。
type Settings struct {
	MaxRows int
	Timeout time.Duration
}

// 默认白名单：只放少量安全的业务表，且只列非敏感列。
//   - plg_cron_job：不含 params（可能带 URL/请求头）
//   - plg_cron_log：不含 output/error（可能带响应正文）
//   - sys_login_log：不含 username/ip（个人信息）；username 仅作数据范围归属列使用，不可被选择
func defaultRaw() []rawSource {
	return []rawSource{
		{Table: "plg_cron_job", Label: "定时任务", TenantColumn: "tenant_id", Columns: []rawColumn{
			{Name: "name", Label: "任务名称", Type: TypeString},
			{Name: "handler", Label: "处理器", Type: TypeString},
			{Name: "enabled", Label: "是否启用", Type: TypeBool},
			{Name: "retry_count", Label: "重试次数", Type: TypeNumber},
			{Name: "last_status", Label: "最近状态", Type: TypeString},
			{Name: "last_run_at", Label: "最近执行时间", Type: TypeTime},
			{Name: "created_at", Label: "创建时间", Type: TypeTime},
		}},
		{Table: "plg_cron_log", Label: "定时任务执行日志", TenantColumn: "tenant_id", Columns: []rawColumn{
			{Name: "job_name", Label: "任务名称", Type: TypeString},
			{Name: "handler", Label: "处理器", Type: TypeString},
			{Name: "trigger_type", Label: "触发方式", Type: TypeString},
			{Name: "status", Label: "状态", Type: TypeString},
			{Name: "attempts", Label: "尝试次数", Type: TypeNumber},
			{Name: "cost_ms", Label: "耗时(ms)", Type: TypeNumber},
			{Name: "started_at", Label: "开始时间", Type: TypeTime},
			{Name: "finished_at", Label: "结束时间", Type: TypeTime},
		}},
		{Table: "sys_login_log", Label: "登录日志", TenantColumn: "tenant_id", UsernameColumn: "username", ScopeRequired: true, Columns: []rawColumn{
			{Name: "login_type", Label: "登录方式", Type: TypeString},
			{Name: "status", Label: "结果", Type: TypeString},
			{Name: "fail_reason", Label: "失败原因", Type: TypeString},
			{Name: "browser", Label: "浏览器", Type: TypeString},
			{Name: "os", Label: "操作系统", Type: TypeString},
			{Name: "created_at", Label: "登录时间", Type: TypeTime},
		}},
	}
}

// rawColumn / rawSource / rawConfig 对应 config.yaml 的 report 段：
//
//	report:
//	  use_defaults: true          # false = 不使用内置白名单
//	  max_rows: 5000              # 只能调小，上限 5000
//	  query_timeout: 10s          # 上限 60s
//	  sources:
//	    - table: biz_order
//	      label: 订单
//	      tenant_column: tenant_id  # 必填；平台级无租户表须显式 global: true
//	      owner_column: created_by  # 可选：数据范围按用户 id 列裁剪
//	      username_column: ""       # 可选：数据范围按账号列裁剪
//	      scope_required: false     # true = 受限数据范围的操作者且表无归属列时拒绝
//	      columns:
//	        - {name: status, label: 状态, type: string}
//
// 配置里的表/列仍要通过硬编码拒绝列表，命中的会被丢弃并记警告日志。
type rawColumn struct {
	Name  string `mapstructure:"name"`
	Label string `mapstructure:"label"`
	Type  string `mapstructure:"type"`
}

type rawSource struct {
	Table          string      `mapstructure:"table"`
	Label          string      `mapstructure:"label"`
	TenantColumn   string      `mapstructure:"tenant_column"`
	Global         bool        `mapstructure:"global"`
	OwnerColumn    string      `mapstructure:"owner_column"`
	UsernameColumn string      `mapstructure:"username_column"`
	ScopeRequired  bool        `mapstructure:"scope_required"`
	Columns        []rawColumn `mapstructure:"columns"`
}

type rawConfig struct {
	UseDefaults  *bool         `mapstructure:"use_defaults"`
	MaxRows      int           `mapstructure:"max_rows"`
	QueryTimeout time.Duration `mapstructure:"query_timeout"`
	Sources      []rawSource   `mapstructure:"sources"`
}

// buildSource 校验并构造一张白名单表；任何问题都返回错误（调用方丢弃该表）。
func buildSource(rs rawSource) (*Source, error) {
	if !validIdent(rs.Table) {
		return nil, fmt.Errorf("invalid table name")
	}
	if deniedTable(rs.Table) {
		return nil, fmt.Errorf("table is on the deny list")
	}
	s := &Source{Table: rs.Table, Label: rs.Label, Global: rs.Global, ScopeRequired: rs.ScopeRequired, byName: map[string]Column{}}
	if s.Label == "" {
		s.Label = rs.Table
	}
	switch {
	case rs.TenantColumn != "":
		if rs.Global {
			return nil, fmt.Errorf("global source must not declare a tenant column")
		}
		if !validIdent(rs.TenantColumn) || deniedColumn(rs.TenantColumn) {
			return nil, fmt.Errorf("invalid tenant column")
		}
		s.TenantCol = rs.TenantColumn
	case !rs.Global:
		return nil, fmt.Errorf("tenant_column is required (or set global: true for platform-level tables)")
	}
	for _, c := range []struct{ name string }{{rs.OwnerColumn}, {rs.UsernameColumn}} {
		if c.name != "" && (!validIdent(c.name) || deniedColumn(c.name)) {
			return nil, fmt.Errorf("invalid scope column")
		}
	}
	s.OwnerCol, s.UsernameCol = rs.OwnerColumn, rs.UsernameColumn
	if len(rs.Columns) == 0 {
		return nil, fmt.Errorf("no columns")
	}
	for _, c := range rs.Columns {
		if !validIdent(c.Name) {
			return nil, fmt.Errorf("invalid column name")
		}
		if deniedColumn(c.Name) {
			return nil, fmt.Errorf("column %q is on the deny list", c.Name)
		}
		switch c.Type {
		case TypeString, TypeNumber, TypeTime, TypeBool:
		default:
			return nil, fmt.Errorf("column %q has unknown type", c.Name)
		}
		if _, dup := s.byName[c.Name]; dup {
			return nil, fmt.Errorf("duplicate column %q", c.Name)
		}
		if c.Name == s.TenantCol {
			return nil, fmt.Errorf("tenant column must not be selectable")
		}
		col := Column{Name: c.Name, Label: c.Label, Type: c.Type}
		if col.Label == "" {
			col.Label = c.Name
		}
		s.byName[c.Name] = col
		s.Cols = append(s.Cols, col)
	}
	return s, nil
}

// newRegistry 由原始配置构造白名单；无效的表被丢弃并通过 warn 回调报告（不含 SQL）。
func newRegistry(raws []rawSource, warn func(table string, err error)) *Registry {
	r := &Registry{sources: map[string]*Source{}}
	for _, rs := range raws {
		s, err := buildSource(rs)
		if err != nil {
			if warn != nil {
				warn(rs.Table, err)
			}
			continue
		}
		r.sources[s.Table] = s // 后出现的同名表覆盖先出现的（配置覆盖默认）
	}
	return r
}

// clampSettings 把配置值夹到硬上限内。
func clampSettings(rc rawConfig) Settings {
	st := Settings{MaxRows: rc.MaxRows, Timeout: rc.QueryTimeout}
	if st.MaxRows <= 0 || st.MaxRows > MaxLimit {
		st.MaxRows = MaxLimit
	}
	if st.Timeout <= 0 {
		st.Timeout = defaultTimeout
	}
	if st.Timeout > maxTimeout {
		st.Timeout = maxTimeout
	}
	return st
}

// loadRegistry 读取默认白名单，并合并 config.yaml 的 report 段。读取失败时只用默认值并记警告。
func loadRegistry(cfg *config.Config, log *zap.Logger) (*Registry, Settings) {
	if log == nil {
		log = zap.NewNop()
	}
	var rc rawConfig
	if cfg != nil && cfg.UserConfigPath != "" {
		if _, err := os.Stat(cfg.UserConfigPath); err == nil {
			v := viper.New()
			v.SetConfigType("yaml")
			v.SetConfigFile(cfg.UserConfigPath)
			if err := v.ReadInConfig(); err != nil {
				log.Warn("report: read config failed, using defaults", zap.Error(err))
			} else if err := v.UnmarshalKey("report", &rc); err != nil {
				log.Warn("report: decode report config failed, using defaults", zap.Error(err))
				rc = rawConfig{}
			}
		}
	}
	var raws []rawSource
	if rc.UseDefaults == nil || *rc.UseDefaults {
		raws = append(raws, defaultRaw()...)
	}
	raws = append(raws, rc.Sources...)
	reg := newRegistry(raws, func(table string, err error) {
		log.Warn("report: source rejected", zap.String("table", table), zap.Error(err))
	})
	return reg, clampSettings(rc)
}
