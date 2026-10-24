package identity

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/config"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

// 全局租户 scope 在 database.Open 里注册（这里是裸 gorm），所以 store 的每条语句都自己带 tenant_id：
// 用 sqlmock 校验每个读写方法的 SQL 都含租户条件，且参数里有该租户 ID。
func TestGormStoreAlwaysFiltersByTenant(t *testing.T) {
	db, mock := mockDB(t)
	st := gormStore{db: db}
	ctx := context.Background()
	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "tenant_id", "type", "name", "enabled", "config"}).AddRow(5, 7, "ldap", "n", true, "{}")
	}
	mock.ExpectQuery("SELECT \\* FROM `plg_identity_source` WHERE tenant_id = \\? ORDER BY").WithArgs(int64(7)).WillReturnRows(rows())
	mock.ExpectQuery("SELECT \\* FROM `plg_identity_source` WHERE tenant_id = \\? AND id = \\? ORDER BY .* LIMIT \\?").WithArgs(int64(7), int64(5), 1).WillReturnRows(rows())
	mock.ExpectQuery("SELECT \\* FROM `plg_identity_source` WHERE tenant_id = \\? AND type = \\? AND enabled = \\? ORDER BY").WithArgs(int64(7), "cas", true).WillReturnRows(rows())
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_identity_source` WHERE tenant_id = \\?").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `plg_identity_source` SET .* WHERE tenant_id = \\? AND id = \\?").WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(7), int64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_identity_source` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if _, err := st.list(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := st.get(ctx, 7, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := st.listEnabled(ctx, 7, "cas"); err != nil {
		t.Fatal(err)
	}
	if n, err := st.count(ctx, 7); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	src := &Source{Name: "n", Enabled: true, Config: "{}"}
	src.ID, src.TenantID = 5, 7
	if err := st.save(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err := st.remove(ctx, 7, 5); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAuditRecordsConfigChangesWithoutSecrets(t *testing.T) {
	initTest(t)
	var got []auditEntry
	svc := NewService(newMemStore(), nil)
	svc.audit = func(_ context.Context, e auditEntry) { got = append(got, e) }
	h := &handler{svc: svc}
	// 通过真实路由触发：用带全部权限的令牌 + 注入了租户的上下文
	e := newAuditEngine(t, h)
	cfg := mustJSON(t, LDAPConfig{URL: "ldap://127.0.0.1:1", AllowPlaintext: true, BindDN: "cn=a", BindPassword: "TOPSECRET-PW", BaseDN: "dc=x", UserFilter: "(uid={username})"})
	body := `{"type":"ldap","name":"corp","enabled":true,"config":` + string(cfg) + `}`
	w := doJSON(e, "POST", "/sources", body)
	if w.Code != 200 || strings.Contains(w.Body.String(), "TOPSECRET-PW") {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	id := between(w.Body.String(), `"id":"`, `"`)
	doJSON(e, "PUT", "/sources/"+id+"/enabled", `{"enabled":false}`)
	doJSON(e, "POST", "/sources/"+id+"/test-login", `{"username":"alice","password":"alice-PW-123"}`)
	doJSON(e, "DELETE", "/sources/"+id, ``)
	types := []string{}
	for _, a := range got {
		types = append(types, a.Type)
		if a.Operator != "admin1" || a.Method == "" || a.URL == "" {
			t.Fatalf("audit entry lacks context: %+v", a)
		}
		dump := strings.Join([]string{a.Type, a.URL}, " ") + toJSON(a.Params)
		if strings.Contains(dump, "TOPSECRET-PW") || strings.Contains(dump, "alice-PW-123") || strings.Contains(strings.ToLower(dump), "password") {
			t.Fatalf("audit entry leaks a secret: %s", dump)
		}
	}
	if strings.Join(types, ",") != "create,update,test,delete" {
		t.Fatalf("audit types = %v", types)
	}
}

func TestMigrationsExistForAllEnginesWithSameColumns(t *testing.T) {
	col := regexp.MustCompile(`(?m)^\s+(\w+)\s+(?:BIGINT|VARCHAR|TINYINT|BOOLEAN|TEXT|DATETIME|TIMESTAMP)`)
	var base []string
	for _, eng := range []string{"mysql", "mariadb", "postgres"} {
		b, err := os.ReadFile(filepath.Join("migrations", eng, "001_init.sql"))
		if err != nil {
			t.Fatalf("%s: %v", eng, err)
		}
		var cols []string
		for _, m := range col.FindAllStringSubmatch(string(b), -1) {
			cols = append(cols, m[1])
		}
		if !strings.Contains(string(b), "plg_identity_source") || strings.Contains(string(b), "ON UPDATE") || strings.Contains(strings.ToUpper(string(b)), "AUTO_INCREMENT") {
			t.Fatalf("%s: unexpected SQL", eng)
		}
		if base == nil {
			base = cols
		} else if strings.Join(base, ",") != strings.Join(cols, ",") {
			t.Fatalf("%s columns %v differ from %v", eng, cols, base)
		}
	}
	if strings.Join(base, ",") != "id,tenant_id,type,name,enabled,config,created_at,updated_at" {
		t.Fatalf("columns = %v", base)
	}
}

func TestStartRegistersLDAPAuthenticatorOnce(t *testing.T) {
	svc, _, ch := newAuthService(t)
	m := &module{}
	d := &core.Deps{Config: &config.Config{}, Cache: ch, Auth: svc, Log: zap.NewNop()}
	if err := m.Start(context.Background(), d); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := m.Start(context.Background(), d); err != nil {
		t.Fatalf("second start must be a no-op: %v", err)
	}
	if err := svc.RegisterExternalAuthenticator(&ldapAuthenticator{}); err == nil {
		t.Fatal("the ldap method must already be registered by Start")
	}
}
