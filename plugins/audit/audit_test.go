package audit

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestIsSensitive(t *testing.T) {
	for _, f := range []string{"password", "Password", "pass_word", "user_password_hash", "client_secret", "access_token", "refresh-token", "apiKey", "api_key", "private_key"} {
		if !IsSensitive(f) {
			t.Errorf("%q should be sensitive", f)
		}
	}
	for _, f := range []string{"name", "email", "status", "tenant_id", "remark"} {
		if IsSensitive(f) {
			t.Errorf("%q should not be sensitive", f)
		}
	}
}

func TestDiffUpdateOnlyChangedFields(t *testing.T) {
	old := map[string]any{"id": int64(1), "name": "a", "age": int64(3), "updated_at": "t1"}
	nw := map[string]any{"id": int64(1), "name": "b", "age": int64(3), "updated_at": "t2"}
	d := Diff(old, nw)
	if len(d) != 1 {
		t.Fatalf("want 1 changed field, got %v", d)
	}
	if c := d["name"]; c.Old != "a" || c.New != "b" {
		t.Fatalf("name change = %+v", c)
	}
}

func TestDiffNoChangeIgnoresUpdatedAt(t *testing.T) {
	if d := Diff(map[string]any{"a": "x", "updated_at": "1"}, map[string]any{"a": "x", "updated_at": "2"}); len(d) != 0 {
		t.Fatalf("expected empty diff, got %v", d)
	}
}

func TestDiffCreateAndDelete(t *testing.T) {
	row := map[string]any{"id": int64(9), "name": "n", "remark": nil}
	c := Diff(nil, row)
	if len(c) != 2 || c["name"].Old != nil || c["name"].New != "n" {
		t.Fatalf("create diff = %v", c)
	}
	d := Diff(row, nil)
	if len(d) != 2 || d["name"].Old != "n" || d["name"].New != nil {
		t.Fatalf("delete diff = %v", d)
	}
}

func TestDiffMasksSensitive(t *testing.T) {
	d := Diff(map[string]any{"password": "old-secret-value", "api_token": "t1", "name": "x"},
		map[string]any{"password": "new-secret-value", "api_token": "t1", "name": "x"})
	c, ok := d["password"]
	if !ok || c.Old != Masked || c.New != Masked {
		t.Fatalf("password change should be masked, got %+v", c)
	}
	if _, ok := d["api_token"]; ok {
		t.Fatalf("unchanged token must not appear")
	}
	// 新增时旧值保持 nil，新值脱敏
	cr := Diff(nil, map[string]any{"client_secret": "abc"})
	if cr["client_secret"].Old != nil || cr["client_secret"].New != Masked {
		t.Fatalf("create secret = %+v", cr["client_secret"])
	}
}

func TestNormalizeValue(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 8*3600))
	if got := normalizeValue(ts); got != "2026-01-01T19:04:05Z" {
		t.Errorf("time = %v", got)
	}
	if got := normalizeValue([]byte("abc")); got != "abc" {
		t.Errorf("bytes = %v", got)
	}
	s := "p"
	if got := normalizeValue(&s); got != "p" {
		t.Errorf("ptr = %v", got)
	}
	var np *string
	if got := normalizeValue(np); got != nil {
		t.Errorf("nil ptr = %v", got)
	}
	// 同一个数用不同整型表示不算变化
	if d := Diff(map[string]any{"n": int32(5)}, map[string]any{"n": int64(5)}); len(d) != 0 {
		t.Errorf("int widths should compare equal: %v", d)
	}
}

func TestLongValueClipped(t *testing.T) {
	long := strings.Repeat("好", maxValueRunes+50)
	d := Diff(map[string]any{"bio": "x"}, map[string]any{"bio": long})
	s, _ := d["bio"].New.(string)
	if !strings.HasSuffix(s, "…") || len([]rune(s)) != maxValueRunes+1 {
		t.Fatalf("clip failed: %d runes", len([]rune(s)))
	}
}

func TestFieldNames(t *testing.T) {
	got := FieldNames(map[string]Change{"b": {}, "a": {}})
	if got != "a,b" {
		t.Fatalf("got %q", got)
	}
	big := map[string]Change{}
	for i := 0; i < 200; i++ {
		big[strings.Repeat("f", 10)+string(rune('a'+i%26))+strings.Repeat("g", i)] = Change{}
	}
	if n := len(FieldNames(big)); n > maxFieldsLen {
		t.Fatalf("summary too long: %d", n)
	}
}

func TestExcluded(t *testing.T) {
	for _, tb := range []string{"plg_audit_log", "plg_audit_config", "sys_oper_log", "sys_login_log", "plg_cron_log", "order_history", "schema_migrations", "gorm_x"} {
		if !Excluded(tb) {
			t.Errorf("%s should be excluded", tb)
		}
	}
	for _, tb := range []string{"sys_user", "sys_role", "orders", "plg_cron_job", "catalog"} {
		if Excluded(tb) {
			t.Errorf("%s should be auditable", tb)
		}
	}
}

func TestAvailable(t *testing.T) {
	got := Available([]string{"sys_user", "plg_audit_log", "sys_oper_log", "orders", "sys_user", ""})
	if strings.Join(got, ",") != "orders,sys_user" {
		t.Fatalf("got %v", got)
	}
}

func TestScopeDefaultsToNothing(t *testing.T) {
	s := NewScope()
	if s.Audited(1, "sys_user") {
		t.Fatal("nothing should be audited by default")
	}
}

func TestScopeFilter(t *testing.T) {
	s := NewScope()
	s.Set(1, []string{"sys_user", "plg_audit_log", "sys_oper_log"})
	if !s.Audited(1, "sys_user") {
		t.Error("sys_user should be audited for tenant 1")
	}
	if s.Audited(2, "sys_user") {
		t.Error("tenant 2 must not inherit tenant 1 scope")
	}
	if s.Audited(1, "sys_role") {
		t.Error("unselected table must not be audited")
	}
	if s.Audited(1, "plg_audit_log") || s.Audited(1, "sys_oper_log") {
		t.Error("excluded tables must never be audited even if present in config")
	}
	s.Replace(map[int64][]string{2: {"orders"}})
	if s.Audited(1, "sys_user") || !s.Audited(2, "orders") {
		t.Error("Replace should swap the whole scope")
	}
	s.Set(2, nil)
	if s.Audited(2, "orders") {
		t.Error("empty Set should clear the tenant")
	}
}

func TestCleanSelection(t *testing.T) {
	clean, bad := CleanSelection([]string{"b", "a", "a", " ", "plg_audit_log", "nope"}, []string{"a", "b", "c"})
	if strings.Join(clean, ",") != "a,b" || strings.Join(bad, ",") != "plg_audit_log,nope" {
		t.Fatalf("clean=%v bad=%v", clean, bad)
	}
}

func TestOperatorFromContext(t *testing.T) {
	if id, name := operatorFrom(context.Background()); id != 0 || name != SystemOperator {
		t.Fatalf("default operator = %d %q", id, name)
	}
	ctx := WithOperator(context.Background(), 42, "alice")
	if id, name := operatorFrom(ctx); id != 42 || name != "alice" {
		t.Fatalf("operator = %d %q", id, name)
	}
}

func TestSkipMarker(t *testing.T) {
	if skipped(context.Background()) || !skipped(markSkip(context.Background())) {
		t.Fatal("skip marker broken")
	}
}

func TestPKHelpers(t *testing.T) {
	row := map[string]any{"a": int64(1), "b": "x"}
	if pkString(row, []string{"a"}) != "1" || pkString(row, []string{"a", "b"}) != "a=1,b=x" {
		t.Fatal("pkString")
	}
	if pkKey(row, []string{"a", "b"}) == pkKey(map[string]any{"a": int64(1), "b": "y"}, []string{"a", "b"}) {
		t.Fatal("pkKey must differ")
	}
}

// 队列满时丢弃并计数，不阻塞调用方。
func TestEnqueueDropsWhenFull(t *testing.T) {
	r := NewRecorder(nil, nil, NewScope())
	ch := map[string]Change{"a": {Old: "1", New: "2"}}
	done := make(chan struct{})
	go func() {
		for i := 0; i < queueSize+25; i++ {
			r.enqueue(context.Background(), "t", "1", ActionUpdate, ch)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("enqueue blocked on a full queue")
	}
	if got := r.Dropped(); got != 25 {
		t.Fatalf("dropped = %d, want 25", got)
	}
	// 空 diff 不入队也不计数
	r.enqueue(context.Background(), "t", "1", ActionUpdate, nil)
	if r.Dropped() != 25 {
		t.Fatal("empty change set must be ignored")
	}
}

func TestDecodeTables(t *testing.T) {
	if got := decodeTables(`["a","b"]`); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if decodeTables("not json") != nil {
		t.Fatal("bad json should yield nil")
	}
}

func TestLikeEscape(t *testing.T) {
	if got := likeEscape(`50%_a\`); got != `50\%\_a\\` {
		t.Fatalf("got %q", got)
	}
}
