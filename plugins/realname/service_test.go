package realname

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	validNumber    = "11010119900307803X"
	badCheckNumber = "110101199003078031"
	fullName       = "张三丰"
)

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	m.Run()
}

var tenantCtx = tenant.With(context.Background(), idgen.ID(7))

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

func memCache(t *testing.T) cache.Cache {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// configRows 的列顺序与 plg_realname_config 一致。
func configRows(rows ...[]any) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{
		"id", "tenant_id", "enabled", "provider", "endpoint", "app_key",
		"app_secret_enc", "timeout_ms", "daily_limit", "created_at", "updated_at",
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

func expectConfigQuery(m sqlmock.Sqlmock, rows *sqlmock.Rows) {
	m.ExpectQuery("SELECT .* FROM `plg_realname_config`").WillReturnRows(rows)
}

func expectConfigEmpty(m sqlmock.Sqlmock) {
	expectConfigQuery(m, sqlmock.NewRows([]string{"id"}))
}

func expectVerificationInsert(m sqlmock.Sqlmock) {
	m.ExpectExec("INSERT INTO `plg_realname_verification`").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// upstream 是假的核验服务：记录每次收到的请求，并按 scripted 队列回放响应。
type upstream struct {
	srv      *httptest.Server
	hits     int
	bodies   []string
	appKeys  []string
	scripted []string // 每个请求返回的响应体
}

func newUpstream(t *testing.T, scripted ...string) *upstream {
	t.Helper()
	u := &upstream{scripted: scripted}
	u.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body)
		u.hits++
		u.bodies = append(u.bodies, string(raw))
		u.appKeys = append(u.appKeys, r.Header.Get("X-App-Key"))
		w.Header().Set("Content-Type", "application/json")
		next := `{"matched":true,"message":"一致"}`
		if len(u.scripted) > u.hits-1 {
			next = u.scripted[u.hits-1]
		}
		_, _ = w.Write([]byte(next))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func enabledConfig(endpoint, key string, dailyLimit int) *Config {
	return &Config{
		Enabled: true, Provider: ProviderGeneric, Endpoint: endpoint,
		AppKey: key, AppSecretEnc: "s3cr3t", TimeoutMs: 3000, DailyLimit: dailyLimit,
	}
}

func TestParseUsesBuiltinRegionDataset(t *testing.T) {
	svc := NewService(nil, nil, nil)
	v, err := svc.Parse("", validNumber)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusValid {
		t.Fatalf("status = %s, want %s", v.Status, StatusValid)
	}
	if v.Birthday != "1990-03-07" || v.Sex != "male" {
		t.Errorf("birthday/sex = %s/%s", v.Birthday, v.Sex)
	}
	if strings.Join(v.RegionNames, "/") == "" || v.RegionCode != "110101" {
		t.Errorf("region = %+v (%q)", v.RegionNames, v.RegionCode)
	}
	if v.NumberMasked == "" || strings.Contains(v.NumberMasked, validNumber) {
		t.Errorf("回显必须是脱敏串，实际 %q", v.NumberMasked)
	}
	if v.Age <= 0 {
		t.Errorf("age = %d", v.Age)
	}

	invalid, err := svc.Parse("", badCheckNumber)
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Status != StatusInvalid || invalid.Reason != "checksum" {
		t.Errorf("invalid = %+v, want status=invalid reason=checksum", invalid)
	}

	if _, err := svc.Parse("passport", validNumber); err == nil {
		t.Error("非居民身份证类型应拒绝，而不是当作身份证解析")
	}
}

func TestVerifyOfflineGivesFormatVerdictOnly(t *testing.T) {
	db, m := mockDB(t)
	expectConfigEmpty(m)
	expectVerificationInsert(m)

	svc := NewService(db, memCache(t), nil)
	res, err := svc.Verify(tenantCtx, VerifyInput{Number: validNumber, Name: fullName})
	if err != nil {
		t.Fatal(err)
	}
	if res.Online {
		t.Error("未启用在线核验不应标记 online")
	}
	if res.Verification.Status != StatusValid || res.Verification.Reason != "" {
		t.Errorf("status/reason = %s/%q", res.Verification.Status, res.Verification.Reason)
	}
	if res.Verification.Provider != "" {
		t.Errorf("provider = %q, want 空", res.Verification.Provider)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestVerifyInvalidNumberNeverLeavesProcess(t *testing.T) {
	u := newUpstream(t)
	db, m := mockDB(t)
	// 号码不合规时不该读配置，更不该出网：只期望一次 INSERT。
	expectVerificationInsert(m)

	svc := NewService(db, memCache(t), nil)
	svc.httpClient = u.srv.Client()
	res, err := svc.Verify(tenantCtx, VerifyInput{Number: badCheckNumber, Name: fullName})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verification.Status != StatusInvalid {
		t.Errorf("status = %s, want invalid", res.Verification.Status)
	}
	if u.hits != 0 {
		t.Errorf("不合规号码仍发起了 %d 次外部调用", u.hits)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestVerifyMatchIsCachedAcrossCalls(t *testing.T) {
	u := newUpstream(t, `{"matched":true,"message":"姓名与号码一致"}`)
	db, m := mockDB(t)
	// 配置直接注入而不走库：这里要断言的是"第二次命中缓存、不再出网"。
	for i := 0; i < 2; i++ {
		expectVerificationInsert(m)
	}

	svc := NewService(db, memCache(t), nil)
	svc.httpClient = u.srv.Client()
	cfg := enabledConfig(u.srv.URL, "key-1", 0)

	for i := 0; i < 2; i++ {
		// 直接放行同一份配置，避免每次都改库；生产路径走 config()，语义一致。
		res, err := svc.verify(tenantCtx, VerifyInput{Number: validNumber, Name: fullName}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Online || res.Verification.Status != StatusMatch {
			t.Fatalf("第 %d 次: online=%v status=%s", i+1, res.Online, res.Verification.Status)
		}
	}
	if u.hits != 1 {
		t.Errorf("第二次应命中缓存，实际外部调用 %d 次", u.hits)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	body := u.bodies[0]
	if !strings.Contains(body, validNumber) || !strings.Contains(body, fullName) {
		t.Errorf("核验请求应带明文姓名与号码，实际 %s", body)
	}
	if u.appKeys[0] != "key-1" {
		t.Errorf("X-App-Key = %q", u.appKeys[0])
	}
}

func TestVerifyDailyQuotaBlocksSecondCall(t *testing.T) {
	u := newUpstream(t, `{"matched":false,"message":"姓名与号码不一致"}`)
	db, m := mockDB(t)
	expectVerificationInsert(m)
	expectVerificationInsert(m)

	svc := NewService(db, memCache(t), nil)
	svc.httpClient = u.srv.Client()
	cfg := enabledConfig(u.srv.URL, "key-1", 1) // 每天只允许一次外部调用

	first, err := svc.verify(tenantCtx, VerifyInput{Number: validNumber, Name: fullName}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verification.Status != StatusMismatch {
		t.Fatalf("first status = %s, want mismatch", first.Verification.Status)
	}

	second, err := svc.verify(tenantCtx, VerifyInput{Number: "500154199301135886", Name: "李四"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if second.Verification.Status != StatusError || second.Verification.Reason != ReasonDailyLimit {
		t.Errorf("second = %s/%q, want error/daily_limit", second.Verification.Status, second.Verification.Reason)
	}
	if u.hits != 1 {
		t.Errorf("超出每日额度后不应再调用外部服务，实际 %d 次", u.hits)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 台账的隐私保证是结构性的：模型里就没有明文姓名列和号码列。
func TestLedgerCarriesNoPlaintext(t *testing.T) {
	db, m := mockDB(t)
	expectConfigEmpty(m)
	expectVerificationInsert(m)

	svc := NewService(db, memCache(t), nil)
	res, err := svc.Verify(tenantCtx, VerifyInput{Number: validNumber, Name: fullName, Remark: "酒店入住登记"})
	if err != nil {
		t.Fatal(err)
	}
	dump := fmt.Sprintf("%+v", *res.Verification)
	if strings.Contains(dump, validNumber) {
		t.Errorf("台账结构体里出现了明文号码: %s", dump)
	}
	if strings.Contains(dump, fullName) {
		t.Errorf("台账结构体里出现了完整姓名: %s", dump)
	}
	if !strings.Contains(dump, "NumberHash") {
		t.Error("台账应带号码摘要，否则无法回溯同一号码")
	}
}

func TestSaveConfigRefusesInsecureEndpoint(t *testing.T) {
	db, m := mockDB(t)
	// 前两次会先读配置再拒绝；provider 未知在读取之前就拒绝。
	for i := 0; i < 2; i++ {
		expectConfigEmpty(m)
	}

	svc := NewService(db, memCache(t), nil)
	if _, err := svc.SaveConfig(tenantCtx, ConfigInput{
		Enabled: true, Provider: ProviderGeneric, Endpoint: "http://verify.example.com/match", AppKey: "k",
	}); err == nil || !strings.Contains(err.Error(), "realname.endpoint_invalid") {
		t.Fatalf("http 地址应被拒绝，实际 %v", err)
	}
	if _, err := svc.SaveConfig(tenantCtx, ConfigInput{
		Enabled: true, Provider: ProviderGeneric, Endpoint: "https://verify.example.com/match",
	}); err == nil || !strings.Contains(err.Error(), "realname.credentials_required") {
		t.Fatalf("没有密钥就开启应被拒绝，实际 %v", err)
	}
	if _, err := svc.SaveConfig(tenantCtx, ConfigInput{
		Enabled: true, Provider: "aliyun", Endpoint: "https://x.example.com", AppKey: "k",
	}); err == nil || !strings.Contains(err.Error(), "realname.provider_unknown") {
		t.Fatalf("未接入的 provider 应被拒绝，实际 %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSaveConfigKeepsStoredSecret(t *testing.T) {
	db, m := mockDB(t)
	expectConfigQuery(m, configRows(
		[]any{1, 7, false, ProviderGeneric, "https://old.example.com", "old-key", "stored-enc", 3000, 100, time.Now(), time.Now()},
	))
	// SET 子句按列名字典序拼出 7 个参数 + id：里面没有 app_secret_enc，密钥因此不会被覆盖
	// （若被带上，参数个数与顺序都会变，这里就会红）。
	m.ExpectExec("UPDATE `plg_realname_config`").
		WithArgs("new-key", 100, false, "https://old.example.com", ProviderGeneric, 3000, sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	svc := NewService(db, memCache(t), nil)
	view, err := svc.SaveConfig(tenantCtx, ConfigInput{
		Enabled: false, Provider: ProviderGeneric, Endpoint: "https://old.example.com",
		AppKey: "new-key", AppSecret: "********", TimeoutMs: 3000, DailyLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasSecret {
		t.Error("回传打码占位值应保持既有密钥，视图却说没有")
	}
	if view.AppKey != "new-key" {
		t.Errorf("appKey = %q", view.AppKey)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
