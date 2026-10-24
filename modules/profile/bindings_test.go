package profile

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/response"
)

func TestCanUnbind(t *testing.T) {
	cases := []struct {
		pwd    bool
		others int
		want   bool
	}{{false, 0, false}, {true, 0, true}, {false, 1, true}, {true, 2, true}}
	for _, c := range cases {
		if got := canUnbind(c.pwd, c.others); got != c.want {
			t.Errorf("canUnbind(%v,%d) = %v, want %v", c.pwd, c.others, got, c.want)
		}
	}
}

func newSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
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
	return &Service{db: db, cfg: &config.Config{}}, mock
}

func expectUser(mock sqlmock.Sqlmock, password string) {
	mock.ExpectQuery("FROM `sys_user` WHERE id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "username", "password"}).AddRow(1, "alice", password))
}

func expectIdentities(mock sqlmock.Sqlmock, providers ...string) {
	rows := sqlmock.NewRows([]string{"id", "user_id", "provider", "external_id"})
	for i, p := range providers {
		rows.AddRow(int64(i+1), 1, p, "ext-"+p)
	}
	mock.ExpectQuery("FROM `sys_user_identity` WHERE user_id").WillReturnRows(rows)
}

func expectDelete(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_user_identity`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// 解绑保底：既没有密码、也没有其他绑定时，不允许解绑唯一的登录方式。
func TestUnbindKeepsAtLeastOneLoginMethod(t *testing.T) {
	s, mock := newSvc(t)
	expectUser(mock, "")
	expectIdentities(mock, "google")
	err := s.UnbindProvider(context.Background(), 1, "google")
	var re *response.Error
	if !errors.As(err, &re) || re.Status != 400 {
		t.Fatalf("want 400, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil { // 没有 DELETE
		t.Error(err)
	}
}

func TestUnbindAllowedWithPassword(t *testing.T) {
	s, mock := newSvc(t)
	expectUser(mock, "$2a$10$hash")
	expectIdentities(mock, "google")
	expectDelete(mock)
	if err := s.UnbindProvider(context.Background(), 1, "google"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestUnbindAllowedWithAnotherBinding(t *testing.T) {
	s, mock := newSvc(t)
	expectUser(mock, "")
	expectIdentities(mock, "google", "github")
	expectDelete(mock)
	if err := s.UnbindProvider(context.Background(), 1, "google"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestUnbindNotBoundIsNoop(t *testing.T) {
	s, mock := newSvc(t)
	expectUser(mock, "")
	expectIdentities(mock, "github")
	if err := s.UnbindProvider(context.Background(), 1, "google"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestUnbindUnknownProvider(t *testing.T) {
	s, _ := newSvc(t)
	err := s.UnbindProvider(context.Background(), 1, "wechat-personal")
	var re *response.Error
	if !errors.As(err, &re) || re.Status != 404 {
		t.Fatalf("want 404, got %v", err)
	}
}

// 个人中心列表只显示配置完整的 provider，绑定信息读 sys_user_identity。
func TestBindingsListsOnlyConfiguredProviders(t *testing.T) {
	s, mock := newSvc(t)
	s.cfg.Auth.Providers = map[string]any{
		"github": map[string]any{"client_id": "a", "client_secret": "b", "redirect_uris": []any{"https://x/cb"}},
		"google": map[string]any{"client_id": "a"}, // 不完整：不显示
	}
	mock.ExpectQuery("FROM `sys_user` WHERE id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "username", "phone"}).AddRow(1, "alice", "13800138000"))
	mock.ExpectQuery("FROM `sys_user_identity` WHERE user_id").WillReturnRows(
		sqlmock.NewRows([]string{"id", "user_id", "provider", "external_id", "external_name"}).AddRow(1, 1, "github", "42", "octo"))
	b, err := s.Bindings(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Providers) != 1 || b.Providers[0].Key != "github" || !b.Providers[0].Bound || b.Providers[0].ExternalName != "octo" || b.Phone != "138****8000" {
		t.Fatalf("bindings = %+v", b)
	}
}
