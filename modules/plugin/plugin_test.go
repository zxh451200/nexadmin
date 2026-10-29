package plugin

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	coreplugin "nexadmin/core/plugin"
	"nexadmin/core/response"
)

const (
	proID    = "demo-pro"
	otherID  = "demo-other"
	freeID   = "demo-free"
	hotID    = "demo-hot"
	noPlugin = "demo-missing"
)

type fakeModule struct{}

func (fakeModule) Name() string                          { return "fake" }
func (fakeModule) Register(*gin.RouterGroup, *core.Deps) {}

var (
	once sync.Once
	priv ed25519.PrivateKey
)

// mount 只做一次：准备临时项目根目录和授权公钥，登记三个插件并 Mount。
// 免费插件故意让清单 edition 与注册不一致（fatal，不会被挂载），这样 Mount 不需要真实的 core.App。
func mount(t *testing.T) *coreplugin.Manager {
	t.Helper()
	once.Do(func() {
		root, err := os.MkdirTemp("", "plugin-mod-test-")
		if err != nil {
			t.Fatal(err)
		}
		for id, ed := range map[string]string{proID: "pro", otherID: "pro", freeID: "pro", hotID: "pro"} {
			dir := filepath.Join(root, "plugins", id)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			y := "id: " + id + "\nname: " + id + "\nversion: 1.0.0\nedition: " + ed + "\n"
			if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		pub, pk, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		priv = pk
		coreplugin.Register(coreplugin.Plugin{ID: proID, Version: "1.0.0", Edition: coreplugin.Pro, Module: fakeModule{}})
		coreplugin.Register(coreplugin.Plugin{ID: otherID, Version: "1.0.0", Edition: coreplugin.Pro, Module: fakeModule{}})
		coreplugin.Register(coreplugin.Plugin{ID: freeID, Version: "1.0.0", Edition: coreplugin.Free, Module: fakeModule{}})
		coreplugin.Register(coreplugin.Plugin{ID: hotID, Version: "1.0.0", Edition: coreplugin.Pro, Module: httpPlugin{}})
		coreplugin.Mount(nil, coreplugin.Options{
			Root: root, Keys: []coreplugin.Key{{KID: coreplugin.KeyID(pub), Pub: pub}}, Hosts: []string{},
		})
	})
	return coreplugin.Default()
}

func signLicense(t *testing.T, plugin string, expires time.Time) string {
	t.Helper()
	raw, err := coreplugin.Sign(priv, coreplugin.License{
		Plugin: plugin, Customer: "acme", IssuedAt: time.Now().Format(time.RFC3339), ExpiresAt: expires.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func post(h gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/license", h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/license", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func msgOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var b response.Body
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("响应不是 JSON: %s", w.Body.String())
	}
	return b.Message
}

func TestImportLicenseErrorMapping(t *testing.T) {
	m := mount(t)
	content := func(v string) string { b, _ := json.Marshal(map[string]string{"content": v}); return string(b) }

	cases := []struct {
		name   string
		id     string
		body   string
		status int
		msgHas string
	}{
		{"缺少 content", proID, `{}`, http.StatusBadRequest, ""},
		{"非法 JSON", proID, `{`, http.StatusBadRequest, ""},
		{"插件不存在", noPlugin, content("x"), http.StatusNotFound, "插件不存在"},
		{"免费插件无需授权", freeID, content("x"), http.StatusBadRequest, "免费插件无需授权"},
		{"授权文件格式错误", proID, content("not a license"), http.StatusBadRequest, ""},
		{"授权属于别的插件", proID, content(signLicense(t, otherID, time.Now().Add(time.Hour))), http.StatusBadRequest, otherID},
		{"授权已过期", proID, content(signLicense(t, proID, time.Now().Add(-time.Hour))), http.StatusBadRequest, "到期"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := post(importLicense(m, tc.id), tc.body)
			if w.Code != tc.status {
				t.Fatalf("状态码 %d，期望 %d：%s", w.Code, tc.status, w.Body.String())
			}
			if tc.msgHas != "" && !strings.Contains(msgOf(t, w), tc.msgHas) {
				t.Errorf("提示 %q 应包含 %q", msgOf(t, w), tc.msgHas)
			}
		})
	}
}

// 校验失败的授权不能落盘，插件状态保持未授权。
func TestRejectedLicenseDoesNotActivate(t *testing.T) {
	m := mount(t)
	b, _ := json.Marshal(map[string]string{"content": signLicense(t, otherID, time.Now().Add(time.Hour))})
	if w := post(importLicense(m, proID), string(b)); w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
	for _, info := range m.List() {
		if info.ID == proID && info.Status == coreplugin.StatusActive {
			t.Fatal("被拒绝的授权不应使插件生效")
		}
	}
}

func api(t *testing.T, cl *auth.Claims, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mount(t)
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/api/v1")
	if cl != nil {
		g.Use(func(c *gin.Context) { auth.SetClaims(c, cl) })
	}
	module{}.Register(g.Group("", middleware.RequireLogin()), &core.Deps{Public: g})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(w, req)
	return w
}

func TestPermissionBoundaries(t *testing.T) {
	super := &auth.Claims{UserID: 1, Super: true}
	listOnly := &auth.Claims{Principal: auth.PrincipalApp, Perms: []string{PermList}}
	none := &auth.Claims{Principal: auth.PrincipalApp}
	licenseURL := "/plugins/" + proID + "/license"

	t.Run("未登录：列表与导入都是 401", func(t *testing.T) {
		if w := api(t, nil, http.MethodGet, "/plugins", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("列表 %d", w.Code)
		}
		if w := api(t, nil, http.MethodPost, licenseURL, `{"content":"x"}`); w.Code != http.StatusUnauthorized {
			t.Errorf("导入 %d", w.Code)
		}
	})
	t.Run("无权限：列表与导入都是 403", func(t *testing.T) {
		if w := api(t, none, http.MethodGet, "/plugins", ""); w.Code != http.StatusForbidden {
			t.Errorf("列表 %d", w.Code)
		}
		if w := api(t, none, http.MethodPost, licenseURL, `{"content":"x"}`); w.Code != http.StatusForbidden {
			t.Errorf("导入 %d", w.Code)
		}
	})
	t.Run("应用即使有 plugin:list 也不能读取授权详情", func(t *testing.T) {
		if w := api(t, listOnly, http.MethodGet, "/plugins", ""); w.Code != http.StatusForbidden {
			t.Errorf("列表 %d", w.Code)
		}
		if w := api(t, listOnly, http.MethodPost, licenseURL, `{"content":"x"}`); w.Code != http.StatusForbidden {
			t.Errorf("导入应 403，得到 %d", w.Code)
		}
	})
	t.Run("超管：列表含已注册插件，导入走到授权校验", func(t *testing.T) {
		w := api(t, super, http.MethodGet, "/plugins", "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), proID) {
			t.Errorf("列表 %d %s", w.Code, w.Body.String())
		}
		if w := api(t, super, http.MethodPost, licenseURL, `{"content":"garbage"}`); w.Code != http.StatusBadRequest {
			t.Errorf("垃圾授权应 400（权限已通过），得到 %d", w.Code)
		}
	})
	t.Run("未启用插件的其它路径：未登录不能被转发", func(t *testing.T) {
		// 休眠插件走 /plugins/<id>/* 转发；授权导入路径之外的请求不能绕过插件自身的启用检查
		if w := api(t, nil, http.MethodGet, "/plugins/"+proID+"/anything", ""); w.Code == http.StatusOK {
			t.Errorf("未启用插件不应返回 200")
		}
	})
}
