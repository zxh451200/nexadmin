package aigateway

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	m.Run()
}

var testNow = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc:              func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

var tenantCtx = tenant.With(context.Background(), 7)

func modelRows(rows ...[]any) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{
		"id", "tenant_id", "kind", "name", "provider", "base_url", "model",
		"api_key_enc", "dimension", "enabled", "is_default", "created_at", "updated_at",
	})
	for _, row := range rows {
		vals := make([]driver.Value, len(row))
		for i, v := range row {
			vals[i] = v
		}
		r.AddRow(vals...)
	}
	return r
}

func llmRow() []any {
	return []any{1001, 7, "llm", "GPT-4o", "openai", "https://api.openai.com", "gpt-4o",
		"", 0, true, true, testNow, testNow}
}

// Resolve：kind 命中默认启用模型并返回解密密钥；未配置返回 ErrDisabled。
func TestResolve(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT \\* FROM `plg_aigateway_model` WHERE").
		WillReturnRows(modelRows(llmRow()))

	cfg, err := s.Resolve(tenantCtx, KindLLM)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "gpt-4o" || cfg.Provider != "openai" || cfg.BaseURL == "" {
		t.Fatalf("unexpected config %+v", cfg)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	db2, m2 := mockDB(t)
	s2 := NewService(db2, nopLogger{})
	m2.ExpectQuery("SELECT \\* FROM `plg_aigateway_model` WHERE").
		WillReturnRows(modelRows())
	if _, err := s2.Resolve(tenantCtx, KindLLM); err != ErrDisabled {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if err := m2.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 非法 kind 在动库前拒绝。
func TestResolveInvalidKind(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})
	if _, err := s.Resolve(tenantCtx, "nope"); err == nil {
		t.Fatal("invalid kind should fail")
	}
}

// Save：非法 kind 与被 SSRF 静态校验拦截的 BaseURL 都拒绝。
func TestSaveModelValidation(t *testing.T) {
	db, _ := mockDB(t)
	s := NewService(db, nopLogger{})

	if _, err := s.SaveModel(tenantCtx, 0, ModelInput{Kind: "nope", Name: "x"}); err == nil {
		t.Fatal("invalid kind should fail")
	}
	if _, err := s.SaveModel(tenantCtx, 0, ModelInput{
		Kind: KindLLM, Name: "x", BaseURL: "http://127.0.0.1:11434",
	}); err == nil {
		t.Fatal("loopback base url should be blocked")
	}
}

// Save：默认位互斥——设默认后清掉同 kind 其余默认。
func TestSaveModelDefaultExclusive(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_aigateway_model`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectExec("UPDATE `plg_aigateway_model`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	if _, err := s.SaveModel(tenantCtx, 0, ModelInput{
		Kind: KindLLM, Name: "main", Provider: "openai",
		BaseURL: "https://api.openai.com", Model: "gpt-4o", Enabled: true, IsDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用量台账：totalTokens 缺省按 prompt+completion 计算，非法 kind 静默丢弃。
func TestRecordUsage(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_aigateway_usage`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	s.RecordUsage(tenantCtx, UsageInput{
		ModelID: 1001, Kind: KindLLM, Provider: "openai", Model: "gpt-4o",
		PromptTokens: 100, CompletionTokens: 50, BizType: "rag", BizNo: "chat-1",
	})

	// 非法 kind 不落库（上面的期望已用尽，这里不应再触发任何 SQL）。
	s.RecordUsage(tenantCtx, UsageInput{Kind: "nope"})

	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用量汇总按 kind 聚合。
func TestUsageSummary(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT kind, COUNT\\(\\*\\) AS calls,").
		WillReturnRows(sqlmock.NewRows([]string{"kind", "calls", "prompt_tokens", "completion_tokens", "total_tokens"}).
			AddRow("llm", 3, 1000, 500, 1500))

	rows, err := s.UsageSummary(tenantCtx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Kind != KindLLM || rows[0].TotalTokens != 1500 {
		t.Fatalf("unexpected summary %+v", rows)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// SSRF 静态校验：内网/回环/元数据地址一律拦截，公网地址放行。
func TestValidateURLStatic(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1:11434",
		"http://localhost:8080",
		"http://10.0.0.5",
		"http://192.168.1.10/v1",
		"http://172.16.0.1",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]:9000",
		"http://user:pass@api.example.com",
		"ftp://api.example.com",
		"http://my-service.internal",
	}
	for _, raw := range blocked {
		if _, err := validateURLStatic(raw); err == nil {
			t.Fatalf("%q should be blocked", raw)
		}
	}
	for _, raw := range []string{"https://api.openai.com", "https://api.openai.com/v1"} {
		if _, err := validateURLStatic(raw); err != nil {
			t.Fatalf("%q should pass: %v", raw, err)
		}
	}
}

func TestProbeEndpoint(t *testing.T) {
	u, err := validateURLStatic("https://api.openai.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got := probeEndpoint(u); got != "https://api.openai.com/v1/models" {
		t.Fatalf("probe = %q", got)
	}
	u2, _ := validateURLStatic("https://api.openai.com")
	if got := probeEndpoint(u2); got != "https://api.openai.com/v1/models" {
		t.Fatalf("probe = %q", got)
	}
}
