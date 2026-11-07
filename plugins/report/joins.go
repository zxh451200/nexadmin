package report

// 关联查询：数据集可声明最多 maxJoins 条白名单表之间的等值内连接（管理员在数据集里配好），
// 图表与运行时筛选只能「引用」已声明的关联列，不能新增关联——这保持了单表引擎的安全边界：
// 请求里永远只有白名单元数据里的列引用，表名与 ON 条件全部来自数据集配置。
// 关联表同样强制租户条件；数据范围仍按主表归属列裁剪（关联表是主表行的展开，不重复裁剪）。

import "strings"

// maxJoins 是一张数据集允许声明的关联上限：两跳足够覆盖「主表 + 维度表」的分析场景，
// 更多关联应该建视图进白名单，而不是在配置里拼出一张隐形大宽表。
const maxJoins = 2

// Join 是一条等值内连接声明：主表.Left = Table.Right（都在白名单内，Left 是主表已选列）。
type Join struct {
	Table string `json:"table"`
	Left  string `json:"left"`
	Right string `json:"right"`
}

// resolvedJoin 是解析完成、可直接进 SQL 的关联。
type resolvedJoin struct {
	table string
	src   *Source
	left  string // 主表列名
	right string // 关联表列名
}

// joinedTable 是关联表在查询目标里的投影：白名单源 + 该数据集选中的关联列。
type joinedTable struct {
	src  *Source
	cols map[string]Column
}

// refSplit 把列引用拆成 (表, 列)：无点 = 主表列；带点 = "关联表.列"。
func refSplit(ref string) (table, col string) {
	if i := strings.Index(ref, "."); i > 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// AddJoin 校验并登记一条关联：表在白名单且未被拒绝、不与主表/已有关联重复，
// Left 是主表已选列、Right 是关联表白名单列。ON 的右列自动成为可引用列。
func (t *Target) AddJoin(r *Registry, j Join) error {
	if len(t.joins) >= maxJoins {
		return specErr("report.joins_too_many")
	}
	if !validIdent(j.Table) || deniedTable(j.Table) || j.Table == t.Src.Table {
		return errNotAllowed
	}
	if _, dup := t.joined[j.Table]; dup {
		return errNotAllowed
	}
	src, ok := r.Get(j.Table)
	if !ok {
		return errNotAllowed
	}
	if tbl, _ := refSplit(j.Right); tbl != "" {
		return errNotAllowed
	}
	rc, ok := src.Col(j.Right)
	if !ok || !validIdent(j.Right) || deniedColumn(j.Right) {
		return errNotAllowed
	}
	if _, _, ok := t.resolveCol(j.Left); !ok {
		return errNotAllowed
	}
	t.joins = append(t.joins, resolvedJoin{table: j.Table, src: src, left: j.Left, right: j.Right})
	t.joined[j.Table] = &joinedTable{src: src, cols: map[string]Column{j.Right: rc}}
	return nil
}

// AddJoinedRef 登记一个关联表列引用（"table.column"）：表必须已声明关联，列必须在
// 关联表白名单里。数据集把选中的关联列逐个登记进来，图表才能引用它们。
func (t *Target) AddJoinedRef(ref string) error {
	table, name := refSplit(ref)
	jt := t.joined[table]
	if jt == nil || !validIdent(table) || !validIdent(name) || deniedColumn(name) {
		return errNotAllowed
	}
	c, ok := jt.src.Col(name)
	if !ok {
		return errNotAllowed
	}
	jt.cols[name] = c
	return nil
}

// resolveCol 解析列引用 → (所属表名, 列元数据)。主表列返回的表名为空。
func (t *Target) resolveCol(ref string) (string, Column, bool) {
	table, name := refSplit(ref)
	if table == "" {
		c, ok := t.cols[name]
		return "", c, ok
	}
	jt := t.joined[table]
	if jt == nil {
		return "", Column{}, false
	}
	c, ok := jt.cols[name]
	return table, c, ok
}

// DatasetTarget 按数据集配置构造查询目标：无点列进主表白名单，带点列（"表.列"）必须
// 匹配一条已声明的关联并存在于该表白名单。这是数据集保存与图表执行共用的唯一入口。
func (r *Registry) DatasetTarget(table string, cols []string, joins []Join) (*Target, error) {
	plain := make([]string, 0, len(cols))
	dotted := map[string][]string{}
	for _, c := range cols {
		if tbl, _ := refSplit(c); tbl != "" {
			dotted[tbl] = append(dotted[tbl], c)
		} else {
			plain = append(plain, c)
		}
	}
	tg, err := r.Target(table, plain)
	if err != nil {
		return nil, err
	}
	for _, j := range joins {
		if err := tg.AddJoin(r, j); err != nil {
			return nil, err
		}
		for _, ref := range dotted[j.Table] {
			if err := tg.AddJoinedRef(ref); err != nil {
				return nil, err
			}
		}
	}
	// 带点列指向了未声明的关联表：数据集配置自相矛盾，整体拒绝。
	for tbl := range dotted {
		if _, ok := tg.joined[tbl]; !ok {
			return nil, errNotAllowed
		}
	}
	return tg, nil
}
