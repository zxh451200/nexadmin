package report

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
)

// 租户条件永远存在、永远在 WHERE 最前面，用户条件各自括号并以 AND 连接，值只出现在参数里。
func TestTenantConditionAlwaysPresentAndNotOverridable(t *testing.T) {
	db, _ := newDB(t, "mysql")
	e := newEngine(db, nil)
	sql, vars := dry(t, e, logTarget(t), Spec{
		Dimensions: []Dim{{Column: "status"}},
		Filters: []Filter{
			{Column: "status", Op: "=", Value: "x' OR 1=1 --"},
			{Column: "attempts", Op: "<=", Value: 9.0},
			{Column: "status", Op: "in", Value: []any{"a", "b' OR '1'='1"}},
			{Column: "job_name", Op: "like", Value: `a%b_c\d`},
		},
	}, testEnv())
	want := "WHERE `tenant_id` = ? AND (`status` = ?) AND (`attempts` <= ?) AND (`status` IN (?,?)) AND (`job_name` LIKE ?)"
	if !strings.Contains(sql, want) {
		t.Fatalf("unexpected SQL:\n%s\nwant to contain:\n%s", sql, want)
	}
	if strings.Contains(sql, "OR 1=1") || strings.Contains(sql, "OR '1'") {
		t.Fatalf("user value leaked into SQL text: %s", sql)
	}
	wantVars := []any{int64(7), "x' OR 1=1 --", int64(9), "a", "b' OR '1'='1", `%a\%b\_c\\d%`, 1001}
	if len(vars) != len(wantVars) {
		t.Fatalf("vars = %#v", vars)
	}
	for i := range wantVars {
		if vars[i] != wantVars[i] {
			t.Errorf("vars[%d] = %#v, want %#v", i, vars[i], wantVars[i])
		}
	}
	// 企图通过过滤列去碰/覆盖租户列：tenant_id 不是可选列，任何写法都被拒绝。
	for _, p := range []string{"tenant_id", "tenant_id = tenant_id OR 1=1", "1=1 OR tenant_id", "`tenant_id`", "TENANT_ID"} {
		sp := Spec{Dimensions: []Dim{{Column: "status"}}, Filters: []Filter{{Column: p, Op: "=", Value: 1.0}}}
		if _, _, err := e.build(context.Background(), logTarget(t), sp, testEnv()); err == nil {
			t.Errorf("filter on %q accepted", p)
		}
	}
	// 整个查询里恰好一次租户条件，且参数是上下文里的租户，与请求内容无关。
	if n := strings.Count(sql, "`tenant_id`"); n != 1 {
		t.Errorf("tenant_id appears %d times", n)
	}
}

func TestTenantRequiredAndScopeRequired(t *testing.T) {
	e := newEngine(nil2DB(t), nil)
	sp := Spec{Dimensions: []Dim{{Column: "status"}}}
	env := testEnv()
	env.TenantID = 0
	if _, _, err := e.build(context.Background(), logTarget(t), sp, env); err == nil {
		t.Error("query without tenant accepted")
	}
	env = testEnv()
	env.Scope = nil
	if _, _, err := e.build(context.Background(), logTarget(t), sp, env); err == nil {
		t.Error("query without datascope accepted")
	}
	// 不同租户产生不同参数、同一条语句。
	s1, v1 := dry(t, e, logTarget(t), sp, testEnv())
	other := testEnv()
	other.TenantID = 8
	s2, v2 := dry(t, e, logTarget(t), sp, other)
	if s1 != s2 || v1[0] != int64(7) || v2[0] != int64(8) {
		t.Errorf("tenant arg not bound per-call: %v %v", v1, v2)
	}
}

func TestDataScopeReuse(t *testing.T) {
	reg := newRegistry(defaultRaw(), nil)
	login, err := reg.Target("sys_login_log", []string{"status", "created_at"})
	if err != nil {
		t.Fatal(err)
	}
	db, _ := newDB(t, "mysql")
	e := newEngine(db, nil)
	sp := Spec{Dimensions: []Dim{{Column: "status"}}}

	limited := testEnv()
	limited.Scope = &datascope.Scope{UserID: 42, Depts: []int64{3, 4}}
	sql, _ := dry(t, e, login, sp, limited)
	if !strings.Contains(sql, "WHERE `tenant_id` = ? AND ((`username` IN (SELECT username FROM sys_user WHERE deleted_at IS NULL AND (id = ? OR dept_id IN (?,?)))))") {
		t.Fatalf("scope condition missing: %s", sql)
	}
	sql, _ = dry(t, e, login, sp, testEnv())
	if strings.Contains(sql, "sys_user") {
		t.Fatalf("All scope must not add a condition: %s", sql)
	}

	// 受限操作者 + 无归属列：ScopeRequired -> 拒绝；否则按租户整体可见（设计里明说的限制）。
	col := Column{Name: "a", Type: TypeString}
	src := &Source{Table: "biz_t", TenantCol: "tenant_id", ScopeRequired: true, Cols: []Column{col}, byName: map[string]Column{"a": col}}
	tg := &Target{Src: src, cols: src.byName}
	if _, _, err := e.build(context.Background(), tg, Spec{Dimensions: []Dim{{Column: "a"}}}, limited); err == nil {
		t.Error("ScopeRequired source without scope column must reject limited viewers")
	}
	src.ScopeRequired = false
	if _, _, err := e.build(context.Background(), tg, Spec{Dimensions: []Dim{{Column: "a"}}}, limited); err != nil {
		t.Errorf("tenant-wide source should be allowed: %v", err)
	}
	// 有用户 id 归属列 -> UserIDCond。
	src.OwnerCol = "created_by"
	sql, _ = dry(t, e, tg, Spec{Dimensions: []Dim{{Column: "a"}}}, limited)
	if !strings.Contains(sql, "(`created_by` = ? OR `created_by` IN (SELECT id FROM sys_user WHERE dept_id IN (?,?)))") {
		t.Errorf("owner scope condition missing: %s", sql)
	}
}

var _ = gorm.ErrRecordNotFound
