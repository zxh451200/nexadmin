package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nexadmin/core/auth"
	corei18n "nexadmin/core/i18n"
	coreplugin "nexadmin/core/plugin"
)

type toggleResult struct {
	code int
	body string
	Info struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		Reason         string `json:"reason"`
		NeedRestart    bool   `json:"needRestart"`
		ConfigDisabled bool   `json:"configDisabled"`
	}
}

// toggle 打一次启停开关并解析返回的 Info。
func toggle(t *testing.T, cl *auth.Claims, id string, body string) toggleResult {
	t.Helper()
	w := api(t, cl, http.MethodPost, "/plugins/"+id+"/enabled", body)
	var res toggleResult
	res.code = w.Code
	res.body = w.Body.String()
	var b struct {
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("响应不是 JSON: %s", w.Body.String())
	}
	if len(b.Data) > 0 {
		if err := json.Unmarshal(b.Data, &res.Info); err != nil {
			t.Fatalf("data 不是插件 Info: %v (%s)", err, b.Data)
		}
	}
	return res
}

// 启停开关的权限边界与状态回传：
// 未登录 401、非平台超管 403（有 plugin:edit 也不行）、缺 enabled 字段 400、未知 id 404、
// 清单有致命问题的插件开关不适用（400 而不是「切成功了但状态没变」）。
func TestToggleEnabledBoundaries(t *testing.T) {
	m := mount(t)
	super := &auth.Claims{UserID: 1, Super: true}
	url := "/plugins/" + proID + "/enabled"

	if w := api(t, nil, http.MethodPost, url, `{"enabled":false}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，得到 %d %s", w.Code, w.Body.String())
	}
	appEdit := &auth.Claims{Principal: auth.PrincipalApp, Perms: []string{PermEdit}}
	if w := api(t, appEdit, http.MethodPost, url, `{"enabled":false}`); w.Code != http.StatusForbidden {
		t.Fatalf("应用主体即使有 plugin:edit 也应 403，得到 %d", w.Code)
	}
	// 不是超管、但有权限点的后台用户同样被 requirePlatformUser 挡下。
	listOnly := &auth.Claims{UserID: 2, Principal: auth.PrincipalApp, Perms: []string{PermList, PermEdit}}
	if w := api(t, listOnly, http.MethodPost, url, `{"enabled":false}`); w.Code != http.StatusForbidden {
		t.Fatalf("非平台超管应 403，得到 %d", w.Code)
	}

	// 缺 enabled 字段：不能被当成「停用」——写坏的 body 不该停掉一个插件。
	if w := api(t, super, http.MethodPost, url, `{}`); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), corei18n.T(context.Background(), "plugin.enabled_required")) {
		t.Fatalf("缺 enabled 应 400 并给出原因，得到 %d %s", w.Code, w.Body.String())
	}
	if st := statusOf(t, m, proID); st != coreplugin.StatusUnlicensed {
		t.Fatalf("被拒绝的请求不该改变插件状态: %s", st)
	}

	if r := toggle(t, super, noPlugin, `{"enabled":false}`); r.code != http.StatusNotFound {
		t.Fatalf("未知 id 应 404，得到 %d %s", r.code, r.body)
	}
	// freeID 的清单 edition 与注册不一致（fatal）：开关不适用。
	if r := toggle(t, super, freeID, `{"enabled":false}`); r.code != http.StatusBadRequest ||
		!strings.Contains(r.body, corei18n.T(context.Background(), "plugin.not_switchable")) {
		t.Fatalf("fatal 插件应 400 not_switchable，得到 %d %s", r.code, r.body)
	}

	// 停用 -> 回 Info（status=disabled + 指向页面的原因），并真的落盘。
	r := toggle(t, super, proID, `{"enabled":false}`)
	if r.code != http.StatusOK || r.Info.Status != coreplugin.StatusDisabled || r.Info.ConfigDisabled {
		t.Fatalf("停用: %d %s", r.code, r.body)
	}
	if !strings.Contains(r.Info.Reason, "插件管理页") {
		t.Errorf("停用原因要说清来源与下一步: %q", r.Info.Reason)
	}
	raw, err := os.ReadFile(coreplugin.DisabledPath(m.Root()))
	if err != nil {
		t.Fatalf("开关没有落盘: %v", err)
	}
	if !strings.Contains(string(raw), proID) {
		t.Fatalf("停用清单内容不对: %s", raw)
	}
	// 列表里也必须是停用态（前端据此把开关画成关）。
	if st := statusOf(t, m, proID); st != coreplugin.StatusDisabled {
		t.Fatalf("列表状态应同步为 disabled，得到 %s", st)
	}

	r = toggle(t, super, proID, `{"enabled":true}`)
	if r.code != http.StatusOK || r.Info.Status != coreplugin.StatusUnlicensed {
		t.Fatalf("启用后应回到未授权（授权闸门的真实状态）: %d %s", r.code, r.body)
	}
}

// 停用后的插件不预留任何自己的路由（与配置 disabled_plugins 同口径）：授权导入端点也一起消失，
// 想导授权得先启用。这里把实际行为钉住——不是 403/400 那种「有入口但被拒」，而是压根没有入口，
// core 侧的 ErrDisabledByOperator 是对「仍然挂着转发路由」的插件的兜底（见 core/plugin 的同名用例）。
func TestDisabledPluginHasNoLicenseRoute(t *testing.T) {
	m := mount(t)
	super := &auth.Claims{UserID: 1, Super: true}
	if r := toggle(t, super, proID, `{"enabled":false}`); r.code != http.StatusOK {
		t.Fatalf("停用失败: %d %s", r.code, r.body)
	}
	t.Cleanup(func() {
		if r := toggle(t, super, proID, `{"enabled":true}`); r.code != http.StatusOK {
			t.Errorf("恢复现场失败: %d %s", r.code, r.body)
		}
	})
	content, err := json.Marshal(map[string]string{"content": signLicense(t, proID, time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	if w := api(t, super, http.MethodPost, "/plugins/"+proID+"/license", string(content)); w.Code != http.StatusNotFound {
		t.Fatalf("停用期间不该有授权导入入口，得到 %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(m.Root(), "data", "licenses", proID+".lic")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("被挡掉的导入不该落盘授权: %v", err)
	}
	if st := statusOf(t, m, proID); st != coreplugin.StatusDisabled {
		t.Fatalf("被挡掉的导入不该解除停用: %s", st)
	}
	// 重新启用后转发路由回到原来的形状（dormant 插件重新出现在 /plugins/<id>/* 预留里）。
	// 启停不改「启动时挂载了什么」，只改闸门判定：这里能拿到 400（走到授权校验）就说明入口回来了。
	if r := toggle(t, super, proID, `{"enabled":true}`); r.code != http.StatusOK || r.Info.Status != coreplugin.StatusUnlicensed {
		t.Fatalf("启用应回到未授权的真实状态: %+v %d", r.Info, r.code)
	}
	if w := api(t, super, http.MethodPost, "/plugins/"+proID+"/license", `{"content":"garbage"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("启用后授权入口应恢复（400 表示走到了授权校验），得到 %d %s", w.Code, w.Body.String())
	}
}

// statusOf 从列表接口读某个插件的当前状态（走真实 handler，不碰内部字段）。
func statusOf(t *testing.T, m *coreplugin.Manager, id string) string {
	t.Helper()
	w := api(t, &auth.Claims{UserID: 1, Super: true}, http.MethodGet, "/plugins", "")
	if w.Code != http.StatusOK {
		t.Fatalf("列表 %d %s", w.Code, w.Body.String())
	}
	var b struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("解析列表失败: %v (%s)", err, w.Body.String())
	}
	for _, p := range b.Data {
		if p.ID == id {
			return p.Status
		}
	}
	t.Fatalf("列表里没有 %s", id)
	return ""
}
