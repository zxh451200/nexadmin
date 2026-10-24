package user

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/oauthflow"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/oauth"
)

type env struct {
	d    *core.Deps
	flow *oauthflow.Flow
	mock sqlmock.Sqlmock
	ctx  context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	cfg := &config.Config{}
	log := zap.NewNop()
	flow, err := oauthflow.New(cfg, ch, log)
	if err != nil {
		t.Fatal(err)
	}
	return &env{d: &core.Deps{Config: cfg, DB: db, Cache: ch, Log: log}, flow: flow, mock: mock, ctx: tenant.With(context.Background(), 1)}
}

func (e *env) identityRows(userID int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "provider", "external_id"}).AddRow(9, 1, userID, "google", "sub-1")
}

func userRows(status string, ids ...int64) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"id", "tenant_id", "username", "status"})
	for _, id := range ids {
		r.AddRow(id, 1, "alice", status)
	}
	return r
}

var googleID = &oauth.Identity{Provider: "google", ExternalID: "sub-1", Email: "alice@corp.com", EmailVerified: true, Name: "Alice"}

func (e *env) resolve(conf config.ProviderConf, id *oauth.Identity) (*loginOutcome, string, error) {
	return (&module{}).resolveLogin(e.ctx, e.d, e.flow, conf, id)
}

func (e *env) done(t *testing.T) {
	t.Helper()
	if err := e.mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestResolveBoundIdentityLogsIn(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(e.identityRows(5))
	e.mock.ExpectQuery("FROM `sys_user` WHERE id").WillReturnRows(userRows("1", 5))
	out, _, err := e.resolve(config.ProviderConf{}, googleID)
	if err != nil || out.User == nil || int64(out.User.ID) != 5 || out.NeedBind {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.done(t)
}

func TestResolveBoundIdentityDisabledUserFails(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(e.identityRows(5))
	e.mock.ExpectQuery("FROM `sys_user` WHERE id").WillReturnRows(userRows("0", 5))
	if _, reason, err := e.resolve(config.ProviderConf{}, googleID); err == nil || reason != reasonDisabled {
		t.Fatalf("want disabled failure, reason=%q err=%v", reason, err)
	}
	e.done(t)
}

// allowed_email_domains 不满足：不碰数据库就拒绝，且对外是统一失败提示。
func TestResolveDomainRestrictionRejectsBeforeAnyLookup(t *testing.T) {
	e := newEnv(t)
	conf := config.ProviderConf{AllowedEmailDomains: []string{"other.com"}}
	_, reason, err := e.resolve(conf, googleID)
	if err == nil || reason != reasonBlocked || err.Error() != errOAuthFailed().Error() {
		t.Fatalf("reason=%q err=%v", reason, err)
	}
	e.done(t)
}

// 邮箱未验证：绝不按邮箱匹配（不会查 sys_user），只能走“绑定已有账号”。
func TestResolveUnverifiedEmailNeverMatchesAccount(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	id := *googleID
	id.EmailVerified = false
	out, _, err := e.resolve(config.ProviderConf{MatchBy: "email"}, &id)
	if err != nil || !out.NeedBind || out.BindToken == "" || out.User != nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.done(t)
}

func TestResolveVerifiedEmailMatchesAndLinks(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("FROM `sys_user` WHERE LOWER\\(email\\)").WithArgs("alice@corp.com", 2).WillReturnRows(userRows("1", 7))
	e.mock.ExpectQuery("count\\(\\*\\) FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	e.mock.ExpectBegin()
	e.mock.ExpectExec("INSERT INTO `sys_user_identity`").WillReturnResult(sqlmock.NewResult(1, 1))
	e.mock.ExpectCommit()
	out, _, err := e.resolve(config.ProviderConf{MatchBy: "email"}, googleID)
	if err != nil || out.User == nil || int64(out.User.ID) != 7 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.done(t)
}

// 匹配到多个账号（有歧义）不猜，走手动绑定；auto_create 关闭时不会建号。
func TestResolveAmbiguousMatchFallsBackToBind(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("FROM `sys_user` WHERE LOWER\\(email\\)").WillReturnRows(userRows("1", 7, 8))
	out, _, err := e.resolve(config.ProviderConf{MatchBy: "email"}, googleID)
	if err != nil || !out.NeedBind {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.done(t)
}

// 匹配到的账号在该 provider 已绑了别的第三方身份：不自动覆盖。
func TestResolveAlreadyLinkedAccountNotOverwritten(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("FROM `sys_user` WHERE LOWER\\(email\\)").WillReturnRows(userRows("1", 7))
	e.mock.ExpectQuery("count\\(\\*\\) FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	out, _, err := e.resolve(config.ProviderConf{MatchBy: "email"}, googleID)
	if err != nil || !out.NeedBind {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	e.done(t)
}

func TestResolveMatchedDisabledAccountFails(t *testing.T) {
	e := newEnv(t)
	e.mock.ExpectQuery("FROM `sys_user_identity`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	e.mock.ExpectQuery("FROM `sys_user` WHERE LOWER\\(email\\)").WillReturnRows(userRows("0", 7))
	if _, reason, err := e.resolve(config.ProviderConf{MatchBy: "email"}, googleID); err == nil || reason != reasonDisabled {
		t.Fatalf("reason=%q err=%v", reason, err)
	}
	e.done(t)
}
