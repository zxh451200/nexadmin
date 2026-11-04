package totp

// 个人中心 confirm/disable 的防护:验证码猜测按「用户+IP」限次;解绑必须先过当前登录密码。
// 缓存用真实 memory 实现(Counter 的原子 Incr 不该被 mock 掉),数据库用 sqlmock。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	gormmysql "gorm.io/driver/mysql"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func memCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// pendingRow 是一条「已生成密钥未确认」的行:密钥即 RFC 6238 测试密钥(测试环境无主密钥,落库即明文)。
func pendingRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "user_id", "secret_enc", "enabled", "last_step"}).
		AddRow(1, 1, 7, rfcKey, false, 0)
}

func isStatus(err error, code int) bool {
	e, ok := err.(*response.Error)
	return ok && e.Status == code
}

// Confirm 连续猜错:前 maxFails-1 次返回码错误,第 maxFails 次失败即把该来源锁进冷却;
// 之后即使拿对码也被 423 拒绝,且不再访问数据库。
func TestConfirmAttemptLimit(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(db, memCache(t), nil)
	ctx := context.Background()

	for i := 0; i < maxFails-1; i++ {
		mock.ExpectQuery("SELECT \\* FROM `plg_totp_secret`").WillReturnRows(pendingRow())
		// 长度不对的码必然被拒,不依赖密钥内容。
		if err := svc.Confirm(ctx, 7, "12345", "10.0.0.1"); !isStatus(err, http.StatusBadRequest) {
			t.Fatalf("attempt %d: want code_invalid(400), got %v", i+1, err)
		}
	}
	// 第 maxFails 次:校验失败的同时把该来源锁进冷却,返回 423。
	mock.ExpectQuery("SELECT \\* FROM `plg_totp_secret`").WillReturnRows(pendingRow())
	if err := svc.Confirm(ctx, 7, "12345", "10.0.0.1"); !isStatus(err, http.StatusLocked) {
		t.Fatalf("fail %d must lock the source, got %v", maxFails, err)
	}
	// 之后再试:先查冷却,不再碰数据库——sqlmock 默认未预期的查询即失败,这里不登记任何期望。
	if err := svc.Confirm(ctx, 7, "12345", "10.0.0.1"); !isStatus(err, http.StatusLocked) {
		t.Fatalf("want locked(423) after %d fails, got %v", maxFails, err)
	}
	// 换一个 IP 不受影响:仍走正常校验(登记一次 SELECT)。
	mock.ExpectQuery("SELECT \\* FROM `plg_totp_secret`").WillReturnRows(pendingRow())
	if err := svc.Confirm(ctx, 7, "12345", "10.0.0.2"); !isStatus(err, http.StatusBadRequest) {
		t.Fatalf("other ip must not be locked, got %v", err)
	}
}

// Disable 缺密码报专属错误 totp.password_required(不是通用 binding 错误):旧客户端/版本错位期间能一眼看出缺的是密码。
func TestDisableMissingPasswordReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock := mockDB(t)
	h := &handler{svc: NewService(db, memCache(t), nil)}
	r := gin.New()
	r.POST("/plugins/totp/disable", h.disable)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/plugins/totp/disable", strings.NewReader(`{"code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "password_required") {
		t.Fatalf("missing password must be 400 with totp.password_required, got %d %s", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Disable 必须先过当前登录密码:没有密码校验器直接 503;密码错误原样返回 Provider 的错误,且不碰数据库。
func TestDisableRequiresReauth(t *testing.T) {
	db, mock := mockDB(t)
	ctx := context.Background()

	svc := NewService(db, memCache(t), nil)
	if err := svc.Disable(ctx, 7, "123456", "pw", "10.0.0.1"); !isStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("no verifier must be 503, got %v", err)
	}

	failing := func(context.Context, uint64, string) error { return response.BadRequest("auth.invalid_credentials") }
	svc = NewService(db, memCache(t), failing)
	if err := svc.Disable(ctx, 7, "123456", "wrong", "10.0.0.1"); !isStatus(err, http.StatusBadRequest) {
		t.Fatalf("want provider's invalid_credentials, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("password check must happen before any db access: %v", err)
	}
}

// 密码通过但验证码错误:计入限次;密码 + 验证码都通过才删除绑定。
func TestDisablePasswordThenCode(t *testing.T) {
	db, mock := mockDB(t)
	passing := func(context.Context, uint64, string) error { return nil }
	svc := NewService(db, memCache(t), passing)
	ctx := context.Background()

	// 码错(长度不对):拒绝并计一次失败。
	mock.ExpectQuery("SELECT \\* FROM `plg_totp_secret`").WillReturnRows(pendingRow())
	if err := svc.Disable(ctx, 7, "12345", "pw", "10.0.0.1"); !isStatus(err, http.StatusBadRequest) {
		t.Fatalf("bad code must be rejected, got %v", err)
	}

	// 码对(当前步长):密码先过,验证码再过,然后 DELETE。
	mock.ExpectQuery("SELECT \\* FROM `plg_totp_secret`").WillReturnRows(pendingRow())
	code, err := CodeAt(rfcKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_totp_secret`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := svc.Disable(ctx, 7, code, "pw", "10.0.0.1"); err != nil {
		t.Fatalf("correct password+code must disable, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 同一来源锁进冷却后,Confirm 的正确码也会被挡住(冷却对正确码生效,不给爆破者判定信号)。
func TestLockBlocksCorrectCodeToo(t *testing.T) {
	db, mock := mockDB(t)
	svc := NewService(db, memCache(t), nil)
	ctx := context.Background()

	for i := 0; i < maxFails; i++ {
		mock.ExpectQuery("SELECT \\* FROM `plg_totp_secret`").WillReturnRows(pendingRow())
		_ = svc.Confirm(ctx, 7, "12345", "10.0.0.9")
	}
	code, err := CodeAt(rfcKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Confirm(ctx, 7, code, "10.0.0.9"); !isStatus(err, http.StatusLocked) {
		t.Fatalf("locked source must reject even the correct code, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
