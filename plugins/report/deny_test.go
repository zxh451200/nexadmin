package report

import (
	"strings"
	"testing"
	"time"
)

func TestDeniedTableVariants(t *testing.T) {
	denied := []string{
		"sys_user", "SYS_USER", "Sys_User", " sys_user ", "`sys_user`", `"sys_user"`, "[sys_user]", "'sys_user'",
		"public.sys_user", `"public"."sys_user"`, "`db`.`SYS_CONFIG`", "dbo.[sys_config]",
		"sys_config", "sys_setting", "sys_pat", "sys_openapi_app", "sys_webhook", "sys_sso_app", "sys_tenant",
		"sys_role", "sys_menu", "sys_oper_log", "sys_file", "sys_user_identity",
		"plg_report_dataset", "PLG_REPORT_SCREEN",
		"information_schema.columns", "pg_catalog.pg_shadow", "pg_user", "mysql.user", "performance_schema.users",
		"sqlite_master", "gorm_migrations", "schema_migrations", "app_secrets", "oauth_tokens", "user_credentials",
		"license_keys", "", "   ", "`",
	}
	for _, s := range denied {
		if !deniedTable(s) {
			t.Errorf("deniedTable(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"plg_cron_job", "plg_cron_log", "sys_login_log", "biz_order", "SYS_LOGIN_LOG"} {
		// SYS_LOGIN_LOG 归一化后是被放行的 sys_login_log；严格标识符校验会另外拒绝大写（见 TestIdentStrict）。
		if deniedTable(s) {
			t.Errorf("deniedTable(%q) = true, want false", s)
		}
	}
}

func TestDeniedColumnVariants(t *testing.T) {
	denied := []string{
		"password", "PASSWORD", "`password`", `"Password"`, "u.password", "sys_user.password", `"public"."sys_user"."password"`,
		"password_hash", "passwd", "pwd", "user_pwd", "hash", "pass_hash", "token", "access_token", "refresh_token", "api_token",
		"secret", "client_secret", "secret_key", "api_key", "apikey", "access_key", "private_key", "salt", "pw_salt",
		"otp", "totp_seed", "mfa_code", "signature", "auth_code", "cookie", "session_id", "jwt", "credential", "cipher_text",
		"encrypted_value", "",
	}
	for _, s := range denied {
		if !deniedColumn(s) {
			t.Errorf("deniedColumn(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"name", "status", "created_at", "login_type", "browser", "os", "cost_ms", "footprint", "monkey", "handler"} {
		if deniedColumn(s) {
			t.Errorf("deniedColumn(%q) = true, want false", s)
		}
	}
}

func TestIdentStrict(t *testing.T) {
	bad := []string{
		"", "A", "Status", "1abc", "a b", "a-b", "a;b", "a`b", `a"b`, "a'b", "a--b", "a/*b*/", "a.b", "a(b)", "a)b", "(select 1)",
		"a\nb", "a\x00b", "é", strings.Repeat("a", 64),
	}
	for _, s := range bad {
		if validIdent(s) {
			t.Errorf("validIdent(%q) = true, want false", s)
		}
	}
	for _, s := range []string{"a", "tenant_id", "plg_cron_job", "d0", strings.Repeat("a", 63)} {
		if !validIdent(s) {
			t.Errorf("validIdent(%q) = false, want true", s)
		}
	}
}

func TestDefaultSourcesAreValidAndSafe(t *testing.T) {
	var warned []string
	reg := newRegistry(defaultRaw(), func(table string, err error) { warned = append(warned, table+": "+err.Error()) })
	if len(warned) > 0 {
		t.Fatalf("default sources rejected: %v", warned)
	}
	want := []string{"plg_cron_job", "plg_cron_log", "sys_login_log"}
	got := reg.List()
	if len(got) != len(want) {
		t.Fatalf("default sources = %d, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.Table != want[i] {
			t.Errorf("source[%d] = %s, want %s", i, s.Table, want[i])
		}
		if s.TenantCol != "tenant_id" {
			t.Errorf("%s: tenant column = %q", s.Table, s.TenantCol)
		}
		for _, c := range s.Cols {
			if deniedColumn(c.Name) || c.Name == "tenant_id" {
				t.Errorf("%s.%s must not be selectable", s.Table, c.Name)
			}
		}
	}
	// 敏感/个人信息列不在默认白名单里。
	login, _ := reg.Get("sys_login_log")
	for _, n := range []string{"username", "ip", "location"} {
		if _, ok := login.Col(n); ok {
			t.Errorf("sys_login_log.%s must not be selectable", n)
		}
	}
	if login.UsernameCol != "username" || !login.ScopeRequired {
		t.Errorf("sys_login_log must be data-scoped by username")
	}
	job, _ := reg.Get("plg_cron_job")
	if _, ok := job.Col("params"); ok {
		t.Error("plg_cron_job.params must not be selectable")
	}
	cl, _ := reg.Get("plg_cron_log")
	for _, n := range []string{"output", "error"} {
		if _, ok := cl.Col(n); ok {
			t.Errorf("plg_cron_log.%s must not be selectable", n)
		}
	}
}

func okSrc() rawSource {
	return rawSource{Table: "biz_order", TenantColumn: "tenant_id", Columns: []rawColumn{{Name: "status", Type: TypeString}}}
}

func TestConfigCannotBypassDenyList(t *testing.T) {
	mut := func(f func(*rawSource)) rawSource { s := okSrc(); f(&s); return s }
	cases := map[string]rawSource{
		"sys_user":            mut(func(s *rawSource) { s.Table = "sys_user" }),
		"sys_user upper":      mut(func(s *rawSource) { s.Table = "SYS_USER" }),
		"sys_user backtick":   mut(func(s *rawSource) { s.Table = "`sys_user`" }),
		"sys_user schema":     mut(func(s *rawSource) { s.Table = "public.sys_user" }),
		"sys_config":          mut(func(s *rawSource) { s.Table = "sys_config" }),
		"sys_oper_log":        mut(func(s *rawSource) { s.Table = "sys_oper_log" }), // sys_ 里只有 sys_login_log 被硬编码放行
		"own table":           mut(func(s *rawSource) { s.Table = "plg_report_dataset" }),
		"information_schema":  mut(func(s *rawSource) { s.Table = "information_schema" }),
		"password column":     mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "password", Type: TypeString}) }),
		"hash column":         mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "password_hash", Type: TypeString}) }),
		"token column":        mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "refresh_token", Type: TypeString}) }),
		"secret column":       mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "app_secret", Type: TypeString}) }),
		"upper column":        mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "Status", Type: TypeString}) }),
		"quoted column":       mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "`status`", Type: TypeString}) }),
		"qualified column":    mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "t.status", Type: TypeString}) }),
		"injection column":    mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "a;drop table x", Type: TypeString}) }),
		"unknown type":        mut(func(s *rawSource) { s.Columns[0].Type = "blob" }),
		"no tenant column":    mut(func(s *rawSource) { s.TenantColumn = "" }),
		"tenant selectable":   mut(func(s *rawSource) { s.Columns = append(s.Columns, rawColumn{Name: "tenant_id", Type: TypeNumber}) }),
		"global with tenant":  mut(func(s *rawSource) { s.Global = true }),
		"bad tenant column":   mut(func(s *rawSource) { s.TenantColumn = "tenant_id or 1=1" }),
		"bad owner column":    mut(func(s *rawSource) { s.OwnerColumn = "created_by`" }),
		"denied owner column": mut(func(s *rawSource) { s.OwnerColumn = "api_token" }),
		"no columns":          mut(func(s *rawSource) { s.Columns = nil }),
		"duplicate column":    mut(func(s *rawSource) { s.Columns = append(s.Columns, s.Columns[0]) }),
	}
	for name, rs := range cases {
		if _, err := buildSource(rs); err == nil {
			t.Errorf("%s: buildSource accepted a source that must be rejected", name)
		}
	}
	// 一个合法的配置源可以通过；未配置租户列的平台级表必须显式 global。
	if _, err := buildSource(okSrc()); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	g := okSrc()
	g.TenantColumn, g.Global = "", true
	if _, err := buildSource(g); err != nil {
		t.Fatalf("explicit global source rejected: %v", err)
	}
	// 经 newRegistry 整体加载：非法项被丢弃，合法项保留。
	var warned int
	reg := newRegistry([]rawSource{cases["sys_user"], okSrc(), cases["password column"]}, func(string, error) { warned++ })
	if warned != 2 || len(reg.List()) != 1 {
		t.Fatalf("warned=%d sources=%d, want 2 and 1", warned, len(reg.List()))
	}
}

func TestTamperedRegistryStillDenied(t *testing.T) {
	// 即使有人绕过 buildSource 直接塞一个危险条目进 Registry，Target 与 build 也会再次用拒绝列表拦下。
	evil := &Source{Table: "sys_user", TenantCol: "tenant_id", Cols: []Column{{Name: "username", Type: TypeString}},
		byName: map[string]Column{"username": {Name: "username", Type: TypeString}}}
	reg := &Registry{sources: map[string]*Source{"sys_user": evil}}
	if _, err := reg.Target("sys_user", []string{"username"}); err == nil {
		t.Fatal("Target accepted sys_user")
	}
	evil2 := &Source{Table: "biz_t", TenantCol: "tenant_id", Cols: []Column{{Name: "password", Type: TypeString}},
		byName: map[string]Column{"password": {Name: "password", Type: TypeString}}}
	reg2 := &Registry{sources: map[string]*Source{"biz_t": evil2}}
	if _, err := reg2.Target("biz_t", []string{"password"}); err == nil {
		t.Fatal("Target accepted a password column")
	}
	// build 层：手工构造的 Target 指向被拒绝的表。
	e := newEngine(nil2DB(t), nil)
	tg := &Target{Src: evil, cols: map[string]Column{"username": {Name: "username", Type: TypeString}}}
	if _, _, err := e.build(t.Context(), tg, Spec{Dimensions: []Dim{{Column: "username"}}}, testEnv()); err == nil {
		t.Fatal("build accepted a denied table")
	}
}

func TestClampSettings(t *testing.T) {
	st := clampSettings(rawConfig{MaxRows: 999999, QueryTimeout: time.Hour})
	if st.MaxRows != MaxLimit || st.Timeout != maxTimeout {
		t.Fatalf("not clamped: %+v", st)
	}
	st = clampSettings(rawConfig{})
	if st.MaxRows != MaxLimit || st.Timeout != defaultTimeout {
		t.Fatalf("defaults wrong: %+v", st)
	}
	st = clampSettings(rawConfig{MaxRows: 100, QueryTimeout: 2 * time.Second})
	if st.MaxRows != 100 || st.Timeout != 2*time.Second {
		t.Fatalf("lowering must be honoured: %+v", st)
	}
}
