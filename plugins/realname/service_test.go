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
	"nexadmin/pkg/idcard"
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

// 区划数据模块没启用或码不在现行/历史表里时，号码本身照样合法，归属地留空。
func TestParseUnknownRegionIsStillValid(t *testing.T) {
	svc := NewService(nil, nil, nil)
	v, err := svc.Parse("", "999999199003077759")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusValid || v.Reason != "" {
		t.Errorf("status/reason = %s/%q，区划查不到不该影响合法性", v.Status, v.Reason)
	}
	if v.RegionLevel != 0 || len(v.RegionNames) != 0 {
		t.Errorf("region = %d %v，应留空", v.RegionLevel, v.RegionNames)
	}
	if v.Birthday != "1990-03-07" || v.Sex != "male" {
		t.Errorf("派生字段 = %s/%s", v.Birthday, v.Sex)
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

// 巩义市 410181：真实反馈里被误判成"格式不通过"的号码，必须走通并给出完整三级归属地。
func TestParseRealCountyLevelCity(t *testing.T) {
	svc := NewService(nil, nil, nil)
	v, err := svc.Parse("", "41018119890415451X")
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusValid {
		t.Fatalf("status = %s reason = %s，合法号码被判非法", v.Status, v.Reason)
	}
	if got := strings.Join(v.RegionNames, "/"); got != "河南省/郑州市/巩义市" {
		t.Errorf("RegionNames = %q，归属地路径不完整（中间层曾被我拼成 8 位码）", got)
	}
	if v.RegionLevel != 3 {
		t.Errorf("RegionLevel = %d, want 3", v.RegionLevel)
	}
	if v.Birthday != "1989-04-15" || v.Sex != "male" {
		t.Errorf("birthday/sex = %s/%s", v.Birthday, v.Sex)
	}
}

// 折叠必须带上租户与数据范围条件：gorm 拼子查询不跑查询回调，租户过滤不会自动进去。
func TestListFoldingCarriesTenantBoundaries(t *testing.T) {
	db, m := mockDB(t)
	// 折叠形态：外层 WHERE ... id IN (SELECT MAX(id) ... WHERE tenant_id ...)
	m.ExpectQuery(`SELECT count.*FROM .plg_realname_verification. WHERE .*IN .*SELECT MAX\(id\).*tenant_id`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	m.ExpectQuery(`SELECT .* FROM .plg_realname_verification. WHERE .*IN .*SELECT MAX\(id\)`).
		WillReturnRows(sqlmock.NewRows(verificationCols()))

	svc := NewService(db, memCache(t), nil)
	_, total, err := svc.ListVerifications(tenantCtx, ListQuery{Latest: true, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total = %d", total)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 没有租户上下文时不折叠：SQL 里不该出现子查询。锚定的 count 模式匹配不到带 WHERE 的折叠形态。
func TestListWithoutTenantDoesNotFold(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(`^SELECT count.*FROM .plg_realname_verification.$`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	m.ExpectQuery(`^SELECT .* FROM .plg_realname_verification. ORDER BY`).
		WillReturnRows(sqlmock.NewRows(verificationCols()))

	svc := NewService(db, memCache(t), nil)
	rows, total, err := svc.ListVerifications(context.Background(), ListQuery{Latest: true, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 0 {
		t.Fatalf("total=%d rows=%d", total, len(rows))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func verificationCols() []string {
	return []string{
		"id", "tenant_id", "created_at", "updated_at", "name_masked", "id_type",
		"number_masked", "number_hash", "status", "reason", "provider", "provider_msg",
		"latency_ms", "operator_id", "operator_name", "remark",
	}
}

// 港澳台居住证与五星卡没有对应的二要素通道：不能送出去，也不能白扣额度。
func TestVerifyNonResidentIDNeverCallsProvider(t *testing.T) {
	for _, tc := range []struct{ number, idType string }{
		{"810000199408230021", IdTypeHMTPermit},
		{"941840199003078038", IdTypeForeigner},
	} {
		u := newUpstream(t, `{"matched":true,"message":"不该被调用"}`)
		db, m := mockDB(t)
		expectVerificationInsert(m)

		svc := NewService(db, memCache(t), nil)
		svc.httpClient = u.srv.Client()
		res, err := svc.verify(tenantCtx, VerifyInput{Number: tc.number, Name: "测试"}, enabledConfig(u.srv.URL, "k", 0))
		if err != nil {
			t.Fatal(err)
		}
		if u.hits != 0 {
			t.Errorf("%s 仍外呼了 %d 次", tc.number, u.hits)
		}
		if res.Verification.Reason != ReasonTypeUnsupported || res.Verification.IdType != tc.idType {
			t.Errorf("reason/idType = %q/%q", res.Verification.Reason, res.Verification.IdType)
		}
		if res.Online {
			t.Error("未调用外部服务不应标记 online")
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	}
}

// 用真实区划数据核对：台湾居民居住证给出证件口径，五星卡给出申领地与国籍。
func TestParseKindsAgainstRealRegionDataset(t *testing.T) {
	svc := NewService(nil, nil, nil)
	hmt, err := svc.Parse("", "830000199201300022")
	if err != nil {
		t.Fatal(err)
	}
	if hmt.Kind != idcard.KindHMTPermit || strings.Join(hmt.RegionNames, "/") != "台湾地区" {
		t.Errorf("830000 应给证件口径的台湾地区，实际 kind=%s names=%v", hmt.Kind, hmt.RegionNames)
	}

	prp, err := svc.Parse("", "941840199003078038")
	if err != nil {
		t.Fatal(err)
	}
	if prp.Kind != idcard.KindForeignerPRP || prp.Country != "US" {
		t.Errorf("五星卡 kind/country = %s/%q", prp.Kind, prp.Country)
	}
	if strings.Join(prp.RegionNames, "/") != "河南省" || prp.RegionCode != "410000" {
		t.Errorf("申领地 = %v (%s)，前 6 位不是区划码", prp.RegionNames, prp.RegionCode)
	}
	if prp.Birthday != "1990-03-07" || prp.Sex != "male" {
		t.Errorf("派生 = %s/%s", prp.Birthday, prp.Sex)
	}
}
