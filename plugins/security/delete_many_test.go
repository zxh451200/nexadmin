package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// gormStore.DeleteRules 必须自己带上 tenant_id：批量删最容易写成「按 id 删」，
// 那样别的租户的规则会一起没掉。去掉 tenant_id = ? 这条条件，本用例必红。
func TestStoreDeleteRulesScopedToTenant(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	st := &gormStore{db: db}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_security_ip_rule` WHERE tenant_id = \\? AND id IN \\(\\?,\\?\\)").
		WithArgs(int64(1), int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := st.DeleteRules(tenant.With(context.Background(), 1), 1, []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteRules: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// superOp 的三条规则：rA 白名单含自己、rB 白名单不含自己、rD 是与自己无关的 deny。
// 单看 rD 删掉是安全的，单看 rA 删掉会自锁 —— 批量的 guard 必须按「一次去掉整批」判，
// 并且判不过就一条都不删（不能退化成「先把能删的删掉」）。
func setupThree(t *testing.T) (*env, User, []idgen.ID) {
	t.Helper()
	e := newEnv(t)
	op := User{TID: 1, ID: 1, Name: "admin", IP: "10.0.0.7", Super: true}
	ctx := context.Background()
	var ids []idgen.ID
	for _, f := range []RuleForm{
		{Action: "allow", CIDR: "10.0.0.0/24", Scope: "super", Enabled: true},
		{Action: "allow", CIDR: "192.0.2.0/24", Scope: "super", Enabled: true},
		{Action: "deny", CIDR: "8.8.8.8/32", Scope: "all", Enabled: true},
	} {
		r, err := e.s.SaveRule(ctx, op, 0, f)
		if err != nil {
			t.Fatalf("SaveRule %+v: %v", f, err)
		}
		ids = append(ids, r.ID)
	}
	return e, op, ids
}

func TestDeleteRulesRejectsWholeBatchThatWouldLockOutOperator(t *testing.T) {
	e, op, ids := setupThree(t)
	ctx := context.Background()

	if _, err := e.s.DeleteRules(ctx, op, []idgen.ID{ids[0], ids[2]}); statusOf(err) != http.StatusConflict {
		t.Fatalf("batch removing the only self-covering allow must be refused, got %v", err)
	}
	left, _ := e.st.ListRules(ctx, 1)
	if len(left) != 3 {
		t.Fatalf("guard 拒绝后一条都不该少，剩下 %d 条", len(left))
	}
	// 没有任何删除成功：不该留下 delete_ip_rule 审计（SaveRule 也会写审计，所以只查删除这一类）
	if got := auditRows(t, e, op, "delete_ip_rule"); len(got) != 0 {
		t.Fatalf("被拒绝的批量删除不该留审计：%+v", got)
	}
}

// 删成功的三条副作用一个都不能少：规则行删掉、本进程规则缓存清掉（漏了访问控制最长要等
// ruleCacheTTL 才对其它请求生效）、每条被删规则各写一行 delete_ip_rule 审计。
// 把 DeleteRules 里的 invalidateRules 或 audit 循环注释掉，本用例必红。
func TestDeleteRulesDropsCacheAndAuditsEachRule(t *testing.T) {
	e, op, ids := setupThree(t)
	ctx := context.Background()
	foreign := otherTenantRule(t, e) // 别的租户的规则：看不见，也就删不掉

	// 预置一个缓存快照，模拟门刚刚读过这个租户的规则
	e.s.rmu.Lock()
	e.s.rcache[op.TID] = ruleSnap{views: viewsOf(mustRules(t, e, ctx, op), e.s.log), at: time.Now()}
	e.s.rmu.Unlock()

	n, err := e.s.DeleteRules(ctx, op, []idgen.ID{ids[1], foreign, 424242})
	if err != nil {
		t.Fatalf("DeleteRules: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1（越权 / 不存在的 id 不计入）", n)
	}
	left, _ := e.st.ListRules(ctx, 1)
	if len(left) != 2 || left[0].CIDR != "10.0.0.0/24" {
		t.Fatalf("want 192.0.2.0/24 gone and the rest intact, got %+v", left)
	}
	e.s.rmu.Lock()
	_, cached := e.s.rcache[op.TID]
	e.s.rmu.Unlock()
	if cached {
		t.Error("rule cache not invalidated after batch delete")
	}
	got := auditRows(t, e, op, "delete_ip_rule")
	if len(got) != 1 || got[0].Target != "192.0.2.0/24" {
		t.Fatalf("want one delete_ip_rule audit for the removed cidr, got %+v", got)
	}
}

// 整批都会自锁时，带 confirmLockout=true 才放行，并且审计里要记下这次二次确认（与单条同口径）。
func TestDeleteRulesConfirmLockoutAuditsNote(t *testing.T) {
	e, op, ids := setupThree(t)
	ctx := context.Background()
	op.ConfirmLockout = true
	n, err := e.s.DeleteRules(ctx, op, []idgen.ID{ids[0], ids[2]})
	if err != nil {
		t.Fatalf("confirmed batch delete: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d, want 2", n)
	}
	got := auditRows(t, e, op, "delete_ip_rule")
	if len(got) != 2 {
		t.Fatalf("want 2 delete audits, got %+v", got)
	}
	for _, l := range got {
		if !strings.Contains(l.Detail, "confirm_lockout") {
			t.Errorf("audit detail %q missing confirm_lockout", l.Detail)
		}
	}
}

// 一条都没命中（都被删过了）报 not found，不能静默返回 deleted=0。
func TestDeleteRulesNotFoundWhenNothingMatches(t *testing.T) {
	e, op, _ := setupThree(t)
	if _, err := e.s.DeleteRules(context.Background(), op, []idgen.ID{424242, 424243}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
}

// ?ids= 缺失或非法一律 400：退化成「不带 ids 就清空白名单」是最难发现的一种自锁。
func TestDeleteRulesHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0"} {
		e := gin.New()
		h := &handler{svc: nil} // 校验在任何业务逻辑之前完成，svc 不该被碰
		e.DELETE("/plugins/security/ip-rules", h.deleteRules)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/security/ip-rules"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

func mustRules(t *testing.T, e *env, ctx context.Context, op User) []IPRule {
	t.Helper()
	rows, err := e.st.ListRules(ctx, op.TID)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// otherTenantRule 在租户 2 建一条规则，返回它的 id：用来证明批量删不会跨租户动手。
func otherTenantRule(t *testing.T, e *env) idgen.ID {
	t.Helper()
	op2 := User{TID: 2, ID: 2, Name: "admin2", IP: "10.0.0.7"}
	r, err := e.s.SaveRule(context.Background(), op2, 0, RuleForm{Action: "allow", CIDR: "10.0.0.0/24", Scope: "all", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}

// auditRows 取某类操作的管理审计（SaveRule 也会写审计，所以按 action 过滤）。
func auditRows(t *testing.T, e *env, op User, action string) []LogRow {
	t.Helper()
	logs, _, err := e.st.ListLogs(context.Background(), op.TID, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []LogRow
	for _, l := range logs {
		if l.Action == action {
			out = append(out, l)
		}
	}
	return out
}
