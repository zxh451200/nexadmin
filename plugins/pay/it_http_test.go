package pay

// 真库 + 真路由的 HTTP 层用例：不只问「服务层返回什么 error」，还要问「线上收到什么报文」。
//
// 背景：契约写的是外层信封的 reason 带 PAY_* 机器码，但框架信封原先没有这个落点——
// 409 到了调用方只剩一句文案，前端没法把「这张结算单已经打过款了」和「状态不允许」
// 分开，重试提示只能是含糊的「操作失败」。本用例把 createPayout 挂到真 gin 引擎、
// 用真库重复提交同一条结算单，断言 HTTP 409 + reason=pay.PAY_PAYOUT_SETTLEMENT_TAKEN；
// 第一条请求必须成功，否则整根断言就是「什么都 409」的空转。
//
// 守卫：NEXADMIN_IT_MYSQL_DSN 才跑（库名留空，用例自建一次性库）。HTTP 形状与引擎无关，
// DSN 指到 MySQL 或 MariaDB 都成立。

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// httpPermProvider 让真 middleware.Perm 走通：uid → 权限码。
type httpPermProvider struct{ perms map[idgen.ID][]string }

func (p httpPermProvider) Login(context.Context, auth.LoginRequest) (*auth.User, error) {
	return nil, nil
}

func (p httpPermProvider) Profile(_ context.Context, id idgen.ID) (*auth.User, error) {
	return &auth.User{ID: id, Perms: p.perms[id]}, nil
}

// postPayout 打一次 POST /plugins/pay/payouts。
func postPayout(t *testing.T, e *gin.Engine, settlementID string) (int, map[string]any) {
	t.Helper()
	body := `{"settlementIds":["` + settlementID + `"],"method":"channel","note":"it http"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/pay/payouts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uid", "1")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("应答不是 JSON（HTTP %d）：%s", w.Code, w.Body.String())
	}
	return w.Code, out
}

func TestITMySQLHTTPPayoutConflict(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL HTTP integration test")
	}
	if !strings.Contains(dsn, "multiStatements=true") {
		if strings.Contains(dsn, "?") {
			dsn += "&multiStatements=true"
		} else {
			dsn += "?multiStatements=true"
		}
	}
	if !strings.Contains(dsn, "parseTime=true") && !strings.Contains(dsn, "parseTime=True") {
		dsn += "&parseTime=true&loc=UTC"
	}

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_payit_http_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	t.Cleanup(func() {
		if gdb != nil {
			if sqlDB, err := gdb.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})
	gdb, err = gorm.Open(mysql.Open(strings.Replace(dsn, ")/", ")/"+dbName+"", 1)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	runPluginMigrations(t, "../pay/migrations/mysql", gdb)

	// 真引擎：与生产同一套 gin 路由 + 权限中间件，只把认证换成按 uid 发权限的桩。
	gin.SetMode(gin.TestMode)
	as, err := auth.NewService(&config.Config{Auth: config.Auth{JWTSecret: strings.Repeat("s", 32)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	as.SetProvider(httpPermProvider{map[idgen.ID][]string{
		1: {"pay:settle", "pay:list"},
	}})
	prev := auth.Default()
	auth.SetDefault(as)
	t.Cleanup(func() { auth.SetDefault(prev) })

	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 7))
		if uid := c.GetHeader("X-Uid"); uid != "" {
			n, _ := strconv.ParseInt(uid, 10, 64)
			auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(n), TenantID: 7, Username: "it", Super: true})
		}
		c.Next()
	})
	grp := e.Group("/api/v1")
	m := &module{}
	m.Register(grp, &core.Deps{DB: gdb, Config: &config.Config{}, Public: grp})

	ctx := tenant.With(context.Background(), 7)
	st := Settlement{
		SubjectID: "it-http-subj", PeriodStart: "2026-11-05", PeriodEnd: "2026-11-05",
		GrossMinor: 3000, CommissionMinor: 300, NetMinor: 2700,
		Currency: "CNY", Status: SettlementConfirmed, OrderCount: 1,
	}
	if err := gdb.WithContext(ctx).Create(&st).Error; err != nil {
		t.Fatalf("create settlement: %v", err)
	}
	settlementID := strconv.FormatInt(int64(st.ID), 10)

	// 正控制：第一条必须真的建出批次——否则下面的 409 可能只是「这条路由永远失败」。
	code, body := postPayout(t, e, settlementID)
	if code != http.StatusOK {
		t.Fatalf("首次建批 HTTP %d：%v", code, body)
	}
	if data, _ := body["data"].(map[string]any); data == nil || data["batchNo"] == "" {
		t.Fatalf("首次建批没有返回批次号：%v", body)
	}

	// 目标断言：同一条结算单再建一次 → 409 + 机器码 + 已本地化文案。
	code, body = postPayout(t, e, settlementID)
	if code != http.StatusConflict {
		t.Fatalf("重复建批 HTTP %d, want 409：%v", code, body)
	}
	if got, _ := body["reason"].(string); got != errPayoutSettlementTaken {
		t.Fatalf("reason = %q, want %q —— 调用方拿不到机器码就只能看到一句文案，没法提示「这笔已打过款」",
			got, errPayoutSettlementTaken)
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatal("message 为空：失败应答必须带可读文案")
	}
	if data, ok := body["data"]; ok && data != nil {
		t.Fatalf("失败应答不该带业务数据：%v", data)
	}

	// 收口范围取证：fail() 是逐调用点替换的，只验一条路由会被「碰巧改到了这条」蒙过去。
	// 404：查不存在的支付单。
	probe := func(method, path, payload string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Uid", "1")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s 应答不是 JSON（HTTP %d）：%s", method, path, w.Code, w.Body.String())
		}
		return w.Code, out
	}
	code, body = probe(http.MethodGet, "/api/v1/plugins/pay/orders/nope-404", "")
	if code != http.StatusNotFound {
		t.Fatalf("查不存在的支付单 HTTP %d, want 404：%v", code, body)
	}
	if got, _ := body["reason"].(string); got != errNotFound {
		t.Fatalf("404 的 reason = %q, want %q（这条路由没走失败出口）", got, errNotFound)
	}

	// 400：空结算单列表（参数校验分支）。
	code, body = probe(http.MethodPost, "/api/v1/plugins/pay/payouts", `{"settlementIds":[]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("空 settlementIds HTTP %d, want 400：%v", code, body)
	}
	if got, _ := body["reason"].(string); got != errInvalidAmount {
		t.Fatalf("400 的 reason = %q, want %q（这条路由没走失败出口）", got, errInvalidAmount)
	}
}
