package report

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
)

// ---- 结构化查询模型 ----
//
// 报表绝不接受原始 SQL。请求只能用「白名单元数据里的列名」去引用列，且只能在固定的几个槽位里出现：
// 维度、聚合、过滤、排序。SQL 文本里的每个标识符都取自 Source/Column 元数据（不是请求字符串），
// 所有值走参数绑定；聚合函数、操作符、时间分桶格式都是代码里的常量。

// Dim 是分组维度；Bucket 仅对 time 列有效（day|month|year）。
type Dim struct {
	Column string `json:"column"`
	Bucket string `json:"bucket,omitempty"`
}

// Measure 是度量：count 的 Column 可为空（COUNT(*)）；sum/avg 需 number 列；min/max 需 number 或 time 列。
type Measure struct {
	Agg    string `json:"agg"`
	Column string `json:"column,omitempty"`
	Label  string `json:"label,omitempty"`
}

// Filter 是一个过滤条件，多个条件之间只有 AND。Value 的形状随操作符：in = 数组，between = 两元素数组，其余为标量。
type Filter struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

// Order 引用输出列：d0、d1… 是维度，m0、m1… 是度量。
type Order struct {
	Ref  string `json:"ref"`
	Desc bool   `json:"desc,omitempty"`
}

// Spec 是一次查询的完整描述。
type Spec struct {
	Dimensions []Dim     `json:"dimensions"`
	Measures   []Measure `json:"measures"`
	Filters    []Filter  `json:"filters"`
	OrderBy    []Order   `json:"orderBy"`
	Limit      int       `json:"limit"`
	// Distinct 仅在没有度量时生效，用于筛选器下拉取候选值。
	Distinct bool `json:"distinct,omitempty"`
}

const (
	maxDims     = 6
	maxMeasures = 8
	maxFilters  = 20
	maxOrder    = 4
	maxInValues = 100
	maxStrLen   = 200
)

var orderRefRe = regexp.MustCompile(`^([dm])([0-9]{1,2})$`)

// 操作符白名单：请求里的 op 必须与键精确相等，值是写进 SQL 的常量。
var opSQL = map[string]string{
	"=": "=", "!=": "<>", ">": ">", "<": "<", ">=": ">=", "<=": "<=",
	"in": "IN", "like": "LIKE", "between": "BETWEEN",
}

var aggSQL = map[string]string{"count": "COUNT", "sum": "SUM", "avg": "AVG", "min": "MIN", "max": "MAX"}

// Target 是一次查询可用的表与列：白名单表 ∩ 数据集选中的列。
type Target struct {
	Src  *Source
	cols map[string]Column
}

// Target 在白名单里解析表与列。每次使用都会重新校验（拒绝列表 + 白名单），
// 所以配置里撤掉某张表/某列后，已有数据集和图表立刻失效，不会继续出数。
func (r *Registry) Target(table string, cols []string) (*Target, error) {
	if !validIdent(table) || deniedTable(table) {
		return nil, errNotAllowed
	}
	src, ok := r.Get(table)
	if !ok {
		return nil, errNotAllowed
	}
	if len(cols) == 0 {
		return nil, specErr("数据集至少选择一列")
	}
	tg := &Target{Src: src, cols: map[string]Column{}}
	for _, name := range cols {
		c, ok := src.Col(name) // 精确匹配：大小写/引号/schema 变体都找不到
		if !ok || !validIdent(name) || deniedColumn(name) {
			return nil, errNotAllowed
		}
		tg.cols[name] = c
	}
	return tg, nil
}

// Columns 返回目标里可用的列（按白名单顺序）。
func (t *Target) Columns() []Column {
	out := make([]Column, 0, len(t.cols))
	for _, c := range t.Src.Cols {
		if _, ok := t.cols[c.Name]; ok {
			out = append(out, c)
		}
	}
	return out
}

func specErr(msg string) *response.Error {
	return response.BadRequest("error.bad_request").WithMsg(msg)
}

// errNotAllowed 故意不指明是哪个标识符、也不说明是被拒绝列表还是白名单拦下的。
var errNotAllowed = specErr("数据源或字段不可用")

// ResultCol 描述结果列。
type ResultCol struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // dim | measure
	Type  string `json:"type"` // string | number | time | bool
}

// Result 是查询结果。Rows 的键是 ResultCol.Key。
type Result struct {
	Columns   []ResultCol      `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated"`
}

// Env 是一次查询的运行环境。
type Env struct {
	TenantID int64
	// Scope 必填：nil 视为配置错误并拒绝执行（fail closed）。内部需要不裁剪时传 &datascope.Scope{All: true}。
	Scope   *datascope.Scope
	Timeout time.Duration
	MaxRows int
}

// Engine 执行结构化查询。
type Engine struct {
	db  *gorm.DB
	log *zap.Logger
}

func newEngine(db *gorm.DB, log *zap.Logger) *Engine {
	if log == nil {
		log = zap.NewNop()
	}
	return &Engine{db: db, log: log}
}

type plan struct {
	cols  []ResultCol
	limit int
}

func quoteFor(db *gorm.DB) func(string) string {
	if db.Dialector != nil && db.Dialector.Name() == "postgres" {
		return func(s string) string { return `"` + s + `"` }
	}
	return func(s string) string { return "`" + s + "`" }
}

func dialectName(db *gorm.DB) string {
	if db.Dialector != nil && db.Dialector.Name() == "postgres" {
		return "postgres"
	}
	return "mysql"
}

var bucketFmt = map[string][2]string{ // bucket -> {mysql, postgres}
	"day":   {"%Y-%m-%d", "YYYY-MM-DD"},
	"month": {"%Y-%m", "YYYY-MM"},
	"year":  {"%Y", "YYYY"},
}

// build 校验 spec 并构造查询（不执行）。
func (e *Engine) build(ctx context.Context, tg *Target, sp Spec, env Env) (*gorm.DB, *plan, error) {
	if tg == nil || tg.Src == nil {
		return nil, nil, errNotAllowed
	}
	// 目标在构造之后配置可能变了：执行前再确认一次表仍在拒绝列表之外。
	if !validIdent(tg.Src.Table) || deniedTable(tg.Src.Table) {
		return nil, nil, errNotAllowed
	}
	src := tg.Src
	if env.TenantID == 0 && !src.Global {
		return nil, nil, response.Forbidden("error.forbidden").WithMsg("缺少租户上下文")
	}
	if env.Scope == nil {
		return nil, nil, response.Forbidden("error.forbidden").WithMsg("缺少数据范围")
	}
	if len(sp.Dimensions) > maxDims || len(sp.Measures) > maxMeasures || len(sp.Filters) > maxFilters || len(sp.OrderBy) > maxOrder {
		return nil, nil, specErr("查询配置超出数量限制")
	}
	if len(sp.Dimensions)+len(sp.Measures) == 0 {
		return nil, nil, specErr("至少选择一个维度或度量")
	}

	q := quoteFor(e.db)
	dialect := dialectName(e.db)
	var sel []clause.Column
	var cols []ResultCol

	for i, d := range sp.Dimensions {
		c, ok := tg.cols[d.Column]
		if !ok || !validIdent(c.Name) || deniedColumn(c.Name) {
			return nil, nil, errNotAllowed
		}
		expr := q(c.Name)
		typ := c.Type
		if d.Bucket != "" {
			f, ok := bucketFmt[d.Bucket]
			if !ok || c.Type != TypeTime {
				return nil, nil, specErr("时间分桶无效")
			}
			if dialect == "postgres" {
				expr = fmt.Sprintf("to_char(%s, '%s')", expr, f[1])
			} else {
				expr = fmt.Sprintf("DATE_FORMAT(%s, '%s')", expr, f[0])
			}
			typ = TypeString
		}
		key := "d" + strconv.Itoa(i)
		sel = append(sel, clause.Column{Name: expr + " AS " + q(key), Raw: true})
		cols = append(cols, ResultCol{Key: key, Label: c.Label, Kind: "dim", Type: typ})
	}
	for i, m := range sp.Measures {
		fn, ok := aggSQL[m.Agg]
		if !ok {
			return nil, nil, specErr("聚合方式无效")
		}
		arg := "*"
		label := m.Label
		if m.Column != "" {
			c, ok := tg.cols[m.Column]
			if !ok || !validIdent(c.Name) || deniedColumn(c.Name) {
				return nil, nil, errNotAllowed
			}
			switch m.Agg {
			case "sum", "avg":
				if c.Type != TypeNumber {
					return nil, nil, specErr("该聚合只支持数值列")
				}
			case "min", "max":
				if c.Type != TypeNumber && c.Type != TypeTime {
					return nil, nil, specErr("该聚合只支持数值或时间列")
				}
			}
			arg = q(c.Name)
			if label == "" {
				label = c.Label
			}
		} else if m.Agg != "count" {
			return nil, nil, specErr("该聚合需要指定列")
		}
		key := "m" + strconv.Itoa(i)
		sel = append(sel, clause.Column{Name: fn + "(" + arg + ") AS " + q(key), Raw: true})
		cols = append(cols, ResultCol{Key: key, Label: label, Kind: "measure", Type: TypeNumber})
	}

	tx := e.db.WithContext(ctx).Table(src.Table)
	distinct := sp.Distinct && len(sp.Measures) == 0
	tx = tx.Clauses(clause.Select{Distinct: distinct, Columns: sel})

	// 租户条件：来自上下文，永远第一个、永远存在；过滤条件只能用 AND 追加，无法覆盖或绕开它。
	if src.TenantCol != "" {
		tx = tx.Where(q(src.TenantCol)+" = ?", env.TenantID)
	}
	// 数据范围：能复用 core/datascope 的就复用（归属列来自元数据）。
	var scopeCond string
	var scopeArgs []any
	var scoped bool
	switch {
	case src.OwnerCol != "":
		scopeCond, scopeArgs, scoped = env.Scope.UserIDCond(q(src.OwnerCol))
	case src.UsernameCol != "":
		scopeCond, scopeArgs, scoped = env.Scope.UsernameCond(q(src.UsernameCol))
	default:
		if env.Scope.Limited() && src.ScopeRequired {
			return nil, nil, response.Forbidden("error.forbidden").WithMsg("该数据源需要数据范围，当前账号无权查看")
		}
	}
	if scoped {
		tx = tx.Where("("+scopeCond+")", scopeArgs...)
	}

	for _, f := range sp.Filters {
		cond, args, err := tg.filterSQL(f, q)
		if err != nil {
			return nil, nil, err
		}
		tx = tx.Where("("+cond+")", args...)
	}

	if len(sp.Measures) > 0 && len(sp.Dimensions) > 0 {
		g := make([]clause.Column, len(sp.Dimensions))
		for i := range g {
			g[i] = clause.Column{Name: strconv.Itoa(i + 1), Raw: true}
		}
		tx = tx.Clauses(clause.GroupBy{Columns: g})
	}

	var ob []clause.OrderByColumn
	for _, o := range sp.OrderBy {
		m := orderRefRe.FindStringSubmatch(o.Ref)
		if m == nil {
			return nil, nil, specErr("排序字段无效")
		}
		n, _ := strconv.Atoi(m[2])
		pos := 0
		if m[1] == "d" && n < len(sp.Dimensions) {
			pos = n + 1
		} else if m[1] == "m" && n < len(sp.Measures) {
			pos = len(sp.Dimensions) + n + 1
		}
		if pos == 0 {
			return nil, nil, specErr("排序字段无效")
		}
		ob = append(ob, clause.OrderByColumn{Column: clause.Column{Name: strconv.Itoa(pos), Raw: true}, Desc: o.Desc})
	}
	if len(ob) == 0 && len(sp.Dimensions) > 0 {
		ob = append(ob, clause.OrderByColumn{Column: clause.Column{Name: "1", Raw: true}})
	}
	if len(ob) > 0 {
		tx = tx.Clauses(clause.OrderBy{Columns: ob})
	}

	limit := sp.Limit
	if limit < 0 {
		return nil, nil, specErr("limit 无效")
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	ceiling := MaxLimit
	if env.MaxRows > 0 && env.MaxRows < ceiling {
		ceiling = env.MaxRows
	}
	if limit > ceiling {
		limit = ceiling
	}
	tx = tx.Limit(limit + 1) // 多取一行用来判断是否被截断
	return tx, &plan{cols: cols, limit: limit}, nil
}

// filterSQL 把一个过滤条件翻成「带占位符的片段 + 参数」。列名取自元数据，值全部参数化。
func (t *Target) filterSQL(f Filter, q func(string) string) (string, []any, error) {
	c, ok := t.cols[f.Column]
	if !ok || !validIdent(c.Name) || deniedColumn(c.Name) {
		return "", nil, errNotAllowed
	}
	op, ok := opSQL[f.Op]
	if !ok {
		return "", nil, specErr("操作符不被允许")
	}
	col := q(c.Name)
	switch f.Op {
	case "in":
		list, ok := f.Value.([]any)
		if !ok || len(list) == 0 || len(list) > maxInValues {
			return "", nil, specErr("in 需要 1 到 100 个值")
		}
		vals := make([]any, len(list))
		for i, v := range list {
			cv, err := coerce(c, v)
			if err != nil {
				return "", nil, err
			}
			vals[i] = cv
		}
		return col + " IN ?", []any{vals}, nil
	case "between":
		list, ok := f.Value.([]any)
		if !ok || len(list) != 2 {
			return "", nil, specErr("between 需要两个值")
		}
		if c.Type != TypeNumber && c.Type != TypeTime {
			return "", nil, specErr("between 只支持数值或时间列")
		}
		lo, err := coerce(c, list[0])
		if err != nil {
			return "", nil, err
		}
		hi, err := coerce(c, list[1])
		if err != nil {
			return "", nil, err
		}
		return col + " BETWEEN ? AND ?", []any{lo, hi}, nil
	case "like":
		if c.Type != TypeString {
			return "", nil, specErr("like 只支持文本列")
		}
		s, ok := f.Value.(string)
		if !ok || len(s) > maxStrLen {
			return "", nil, specErr("like 值无效")
		}
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
		return col + " LIKE ?", []any{"%" + esc + "%"}, nil
	case ">", "<", ">=", "<=":
		if c.Type != TypeNumber && c.Type != TypeTime {
			return "", nil, specErr("比较操作只支持数值或时间列")
		}
	}
	if _, isList := f.Value.([]any); isList {
		return "", nil, specErr("该操作符需要单个值")
	}
	v, err := coerce(c, f.Value)
	if err != nil {
		return "", nil, err
	}
	return col + " " + op + " ?", []any{v}, nil
}

var timeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}

// parseTime 解析时间值，一律按 UTC（库连接也是 UTC）。
func parseTime(s string) (time.Time, bool) {
	for _, l := range timeLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// coerce 按列类型把 JSON 值转成强类型参数；类型不符一律拒绝，绝不原样透传。
func coerce(c Column, v any) (any, error) {
	bad := specErr("过滤值类型不匹配")
	switch c.Type {
	case TypeString:
		s, ok := v.(string)
		if !ok || len(s) > maxStrLen {
			return nil, bad
		}
		return s, nil
	case TypeNumber:
		switch x := v.(type) {
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, bad
			}
			if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
				return int64(x), nil
			}
			return x, nil
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
				return n, nil
			}
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, bad
			}
			return f, nil
		}
		return nil, bad
	case TypeTime:
		s, ok := v.(string)
		if !ok {
			return nil, bad
		}
		t, ok := parseTime(strings.TrimSpace(s))
		if !ok {
			return nil, bad
		}
		return t, nil
	case TypeBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch strings.ToLower(x) {
			case "true", "1":
				return true, nil
			case "false", "0":
				return false, nil
			}
		case float64:
			if x == 0 || x == 1 {
				return x == 1, nil
			}
		}
		return nil, bad
	}
	return nil, bad
}

// ValidateSpec 只做校验，不访问数据库（保存图表时用）。
func (e *Engine) ValidateSpec(tg *Target, sp Spec) error {
	_, _, err := e.build(context.Background(), tg, sp, Env{TenantID: 1, Scope: &datascope.Scope{All: true}})
	return err
}

// Run 执行查询：带超时与行数上限；任何数据库错误只进日志，对外只给通用文案。
func (e *Engine) Run(ctx context.Context, tg *Target, sp Spec, env Env) (*Result, error) {
	timeout := env.Timeout
	if timeout <= 0 || timeout > maxTimeout {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tx, pl, err := e.build(ctx, tg, sp, env)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Rows()
	if err != nil {
		return nil, e.execErr(ctx, err)
	}
	defer rows.Close()

	res := &Result{Columns: pl.cols, Rows: []map[string]any{}}
	vals := make([]any, len(pl.cols))
	ptrs := make([]any, len(pl.cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if len(res.Rows) >= pl.limit {
			res.Truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, e.execErr(ctx, err)
		}
		row := make(map[string]any, len(pl.cols))
		for i, c := range pl.cols {
			row[c.Key] = convertValue(vals[i], c.Type)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, e.execErr(ctx, err)
	}
	return res, nil
}

// execErr 把执行错误收敛成不含 SQL/库细节的响应；原始错误通过 Wrap 只进日志。
func (e *Engine) execErr(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return response.New(http.StatusGatewayTimeout, "error.internal").WithMsg("查询超时，请缩小范围后重试").Wrap(err)
	}
	e.log.Warn("report: query failed", zap.Error(err))
	return response.Internal().WithMsg("查询失败，请稍后重试").Wrap(err)
}

func convertValue(v any, typ string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return convertValue(string(x), typ)
	case string:
		if typ == TypeNumber {
			if n, err := strconv.ParseInt(x, 10, 64); err == nil && math.Abs(float64(n)) < 1<<53 {
				return n
			}
			if f, err := strconv.ParseFloat(x, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				return f
			}
		}
		return x
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case float32:
		return float64(x)
	case int64:
		if typ == TypeBool {
			return x != 0
		}
		return x
	}
	return v
}
