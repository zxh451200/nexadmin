package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// TestHTTPSurfaceIntegration 走真实 MySQL + 真实 Gin 路由，验证「HTTP → 适配层 → 运行时 → 引擎 → 库」
// 的完整链路：发起幂等、待办/已办/我发起、非审批人 403、结束 409、加签/回退/撤权。
func TestHTTPSurfaceIntegration(t *testing.T) {
	db := setupWorkflowDB(t)
	svc := NewService(db, nopLogger{})
	h := &handler{svc: svc}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	// 用 Super 身份跳过 RBAC，只验证插件自身的行为；租户仍显式放进 context。
	api.Use(func(c *gin.Context) {
		id, err := idgen.Parse(actorID(c.GetHeader("X-Test-User")))
		if err != nil {
			id = 2001
		}
		c.Set("nexadmin.claims", &auth.Claims{
			UserID: id, TenantID: 42, Username: "user-" + c.GetHeader("X-Test-User"), Super: true,
		})
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 42))
		c.Next()
	})
	h.Register(api, nil)

	const userMgr, userHR1, userHR2, userStranger = "001", "002", "003", "009"
	xmlDef := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
  xmlns:flowlite="urn:flowlite:engine:1.0" id="Defs" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="leave" name="请假" isExecutable="true">
    <bpmn:startEvent id="start" name="开始" />
    <bpmn:userTask id="manager" name="主管" flowlite:approvers="%s" />
    <bpmn:userTask id="hr" name="HR" flowlite:approvers="%s,%s" flowlite:mode="all" />
    <bpmn:endEvent id="end" name="结束" />
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="manager" />
    <bpmn:sequenceFlow id="f2" sourceRef="manager" targetRef="hr" />
    <bpmn:sequenceFlow id="f3" sourceRef="hr" targetRef="end" />
  </bpmn:process>
</bpmn:definitions>`, actorID(userMgr), actorID(userHR1), actorID(userHR2))

	// 1. 建定义（XML 路径），并确认列表/导出可用。
	def := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/defs",
		map[string]any{"key": "leave", "name": "请假", "status": "published", "xml": xmlDef})
	defID := strField(t, def, "id")
	if strField(t, def, "status") != "published" || intField(t, def, "version") != 1 {
		t.Fatalf("定义 = %v", def)
	}
	list := doJSON(t, r, userMgr, http.MethodGet, "/api/v1/plugins/workflow/defs?page=1&pageSize=10", nil)
	if intField(t, list, "total") != 1 {
		t.Fatalf("定义列表 = %v", list)
	}
	if xml := doRaw(t, r, userMgr, http.MethodGet, "/api/v1/plugins/workflow/defs/"+defID+"/xml", nil); !bytes.Contains(xml, []byte("flowlite:approvers")) {
		t.Fatalf("XML 导出丢失扩展属性:\n%s", xml)
	}

	// 2. 发起：第一次落库，第二次同业务键幂等返回同一实例。
	in := map[string]any{"defId": defID, "title": "张三请假", "bizType": "leave", "businessKey": "L-1"}
	inst := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances", in)
	instID := strField(t, inst, "id")
	if strField(t, inst, "status") != "running" || intField(t, inst, "defVersion") != 1 {
		t.Fatalf("实例 = %v", inst)
	}
	again := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances", in)
	if strField(t, again, "id") != instID {
		t.Fatalf("幂等发起返回了不同实例: %v vs %v", again["id"], instID)
	}

	// 3. 待办：只有主管能办；越权审批 403。
	todo := doJSON(t, r, userMgr, http.MethodGet, "/api/v1/plugins/workflow/instances?todo=1&page=1&pageSize=20", nil)
	if intField(t, todo, "total") != 1 {
		t.Fatalf("主管待办 = %v", todo)
	}
	if body := doErr(t, r, userHR1, http.MethodPost, "/api/v1/plugins/workflow/instances/"+instID+"/approve", map[string]any{"comment": "越权"}); body != http.StatusForbidden {
		t.Fatalf("非审批人审批状态 = %d", body)
	}

	// 4. 走完或签 + 会签。
	doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances/"+instID+"/approve", map[string]any{"comment": "同意"})
	if todo := doJSON(t, r, userHR1, http.MethodGet, "/api/v1/plugins/workflow/instances?todo=1", nil); intField(t, todo, "total") != 1 {
		t.Fatalf("HR 待办 = %v", todo)
	}
	doJSON(t, r, userHR1, http.MethodPost, "/api/v1/plugins/workflow/instances/"+instID+"/approve", map[string]any{})
	after := doJSON(t, r, userHR2, http.MethodPost, "/api/v1/plugins/workflow/instances/"+instID+"/approve", map[string]any{})
	if strField(t, after, "status") != "approved" {
		t.Fatalf("会签后状态 = %v", after)
	}
	if intField(t, after, "revision") < 3 {
		t.Fatalf("revision 未递增 = %v", after["revision"])
	}

	// 5. 已办 / 我发起 / 结束后再审批 409。
	if done := doJSON(t, r, userHR1, http.MethodGet, "/api/v1/plugins/workflow/instances?done=1", nil); intField(t, done, "total") != 1 {
		t.Fatalf("我已办 = %v", done)
	}
	if mine := doJSON(t, r, userMgr, http.MethodGet, "/api/v1/plugins/workflow/instances?mine=started", nil); intField(t, mine, "total") != 1 {
		t.Fatalf("我发起 = %v", mine)
	}
	if code := doErr(t, r, userHR2, http.MethodPost, "/api/v1/plugins/workflow/instances/"+instID+"/approve", map[string]any{}); code != http.StatusConflict {
		t.Fatalf("结束后审批状态 = %d", code)
	}

	// 6. 加签 / 回退 / 撤回。
	inst2 := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances",
		map[string]any{"defId": defID, "title": "加签单", "bizType": "leave", "businessKey": "L-2"})
	id2 := strField(t, inst2, "id")
	if code := doErr(t, r, userStranger, http.MethodPost, "/api/v1/plugins/workflow/instances/"+id2+"/countersign",
		map[string]any{"approver": actorID(userStranger)}); code != http.StatusForbidden {
		t.Fatalf("非当前审批人加签状态 = %d", code)
	}
	signed := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances/"+id2+"/countersign",
		map[string]any{"approver": actorID(userStranger), "comment": "请一起看"})
	if len(signed["pending"].([]any)) != 2 {
		t.Fatalf("加签后待办 = %v", signed["pending"])
	}
	rolled := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances/"+id2+"/rollback",
		map[string]any{"toNode": "manager", "comment": "重审"})
	if cur := rolled["current"].([]any); len(cur) != 1 || cur[0] != "manager" {
		t.Fatalf("回退后 current = %v", rolled["current"])
	}
	if code := doErr(t, r, userHR1, http.MethodPost, "/api/v1/plugins/workflow/instances/"+id2+"/withdraw", map[string]any{}); code != http.StatusForbidden {
		t.Fatalf("非提交人撤回状态 = %d", code)
	}
	if out := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances/"+id2+"/withdraw", map[string]any{}); strField(t, out, "status") != "cancelled" {
		t.Fatalf("撤回后 = %v", out)
	}

	// 7. 超时升级：扫描器跨租户读取，但推进时必须按实例租户重建 context 才能写回。
	timeoutXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
  xmlns:flowlite="urn:flowlite:engine:1.0" id="Defs2" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="timeout_demo" name="超时演示" isExecutable="true">
    <bpmn:startEvent id="start" name="开始" />
    <bpmn:userTask id="manager" name="主管" flowlite:approvers="%s" flowlite:timeoutMinutes="1" flowlite:escalateTo="hr" />
    <bpmn:userTask id="hr" name="HR" flowlite:approvers="%s" />
    <bpmn:endEvent id="end" name="结束" />
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="manager" />
    <bpmn:sequenceFlow id="f2" sourceRef="manager" targetRef="hr" />
    <bpmn:sequenceFlow id="f3" sourceRef="hr" targetRef="end" />
  </bpmn:process>
</bpmn:definitions>`, actorID(userMgr), actorID(userHR1))
	timeoutDef := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/defs",
		map[string]any{"key": "timeout_demo", "name": "超时演示", "status": "published", "xml": timeoutXML})
	timeoutInst := doJSON(t, r, userMgr, http.MethodPost, "/api/v1/plugins/workflow/instances",
		map[string]any{"defId": strField(t, timeoutDef, "id"), "title": "超时单"})
	tid := strField(t, timeoutInst, "id")
	// 把等待时间推到 2 分钟前（只改本测试租户的这一行）。
	if err := db.Exec("UPDATE plg_workflow_inst SET updated_at = ? WHERE id = ? AND tenant_id = ?",
		time.Now().UTC().Add(-2*time.Minute), tid, 42).Error; err != nil {
		t.Fatalf("回拨更新时间失败: %v", err)
	}
	n, err := svc.SweepEscalate(context.Background(), time.Now().UTC())
	if err != nil || n != 1 {
		t.Fatalf("SweepEscalate = %d, err=%v", n, err)
	}
	escalated := doJSON(t, r, userHR1, http.MethodGet, "/api/v1/plugins/workflow/instances/"+tid, nil)
	if cur := escalated["current"].([]any); len(cur) != 1 || cur[0] != "hr" {
		t.Fatalf("升级后 current = %v", escalated["current"])
	}
	if todo := doJSON(t, r, userHR1, http.MethodGet, "/api/v1/plugins/workflow/instances?todo=1&page=1&pageSize=50", nil); intField(t, todo, "total") < 1 {
		t.Fatalf("升级后 HR 待办 = %v", todo)
	}
}

// ---- 测试用 HTTP 辅助 ----

func doRaw(t *testing.T, r http.Handler, user, method, path string, body any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s 状态 = %d, body=%s", method, path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func doJSON(t *testing.T, r http.Handler, user, method, path string, body any) map[string]any {
	t.Helper()
	raw := doRaw(t, r, user, method, path, body)
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("响应不是 JSON: %v\n%s", err, raw)
	}
	if resp.Code != 0 {
		t.Fatalf("%s %s code=%d body=%s", method, path, resp.Code, raw)
	}
	return resp.Data
}

func doErr(t *testing.T, r http.Handler, user, method, path string, body any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func strField(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key].(string)
	if !ok {
		t.Fatalf("字段 %s 不是字符串: %v", key, m[key])
	}
	return v
}

func intField(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("字段 %s 不是数字: %v", key, m[key])
	}
	return int(v)
}

// actorID 把三位测试编号拼成 19 位雪花风格 ID（与中间件注入的 Claims.UserID 一致）。
func actorID(u string) string { return "2" + u + "000000000000000" }
