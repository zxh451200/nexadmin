package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func newCaptchaSvc(t *testing.T) *Service {
	t.Helper()
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return &Service{cache: ch}
}

// answerOf 直接从缓存读答案（测试专用）；缓存里存的是 "ip|answer"。
func answerOf(t *testing.T, s *Service, key string) string {
	t.Helper()
	v, err := s.cache.Get(context.Background(), captchaKey(key))
	if err != nil {
		t.Fatal(err)
	}
	_, text, _ := strings.Cut(v, "|")
	return text
}

// testIP 是测试统一使用的签发/校验 IP。
const testIP = "10.0.0.9"

func TestCaptchaImageIsPNG(t *testing.T) {
	s := newCaptchaSvc(t)
	key, img, err := s.NewCaptcha(context.Background(), testIP)
	if err != nil || key == "" {
		t.Fatalf("NewCaptcha: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(img, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != capW || cfg.Height != capH {
		t.Fatalf("bad png: %v %+v", err, cfg)
	}
}

func TestCaptchaOneTime(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _ := s.NewCaptcha(ctx, testIP)
	ans := answerOf(t, s, key)
	if ok, _ := s.VerifyCaptcha(ctx, key, ans, testIP); !ok {
		t.Fatal("correct answer must pass once")
	}
	if ok, _ := s.VerifyCaptcha(ctx, key, ans, testIP); ok {
		t.Fatal("the same captcha must not verify twice")
	}
	// 答错同样作废：不能对同一张图反复试
	key2, _, _ := s.NewCaptcha(ctx, testIP)
	ans2 := answerOf(t, s, key2)
	if ok, _ := s.VerifyCaptcha(ctx, key2, "x"+ans2, testIP); ok {
		t.Fatal("wrong answer must fail")
	}
	if ok, _ := s.VerifyCaptcha(ctx, key2, ans2, testIP); ok {
		t.Fatal("captcha must be consumed by a failed attempt")
	}
	if ok, _ := s.VerifyCaptcha(ctx, "", ans, testIP); ok {
		t.Fatal("empty key must fail")
	}
}

// 验证码绑定签发时的客户端 IP：其他来源即使拿到 key+答案也无效。
func TestCaptchaBoundToIP(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _ := s.NewCaptcha(ctx, "10.0.0.9")
	ans := answerOf(t, s, key)
	if ok, _ := s.VerifyCaptcha(ctx, key, ans, "10.0.0.9"); !ok {
		t.Fatal("same IP must pass")
	}
	key2, _, _ := s.NewCaptcha(ctx, "10.0.0.9")
	if ok, _ := s.VerifyCaptcha(ctx, key2, answerOf(t, s, key2), "10.0.0.10"); ok {
		t.Fatal("captcha issued to one IP must not verify from another")
	}
	key3, _, _ := s.NewCaptcha(ctx, "10.0.0.9")
	if ok, _ := s.VerifyCaptcha(ctx, key3, answerOf(t, s, key3), ""); ok {
		t.Fatal("empty IP must not verify an IP-bound captcha")
	}
}

func TestCaptchaExpires(t *testing.T) {
	old := captchaTTL
	captchaTTL = 40 * time.Millisecond
	defer func() { captchaTTL = old }()
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _ := s.NewCaptcha(ctx, testIP)
	ans := answerOf(t, s, key)
	time.Sleep(90 * time.Millisecond)
	if ok, _ := s.VerifyCaptcha(ctx, key, ans, testIP); ok {
		t.Fatal("expired captcha must fail")
	}
}

type fakeProvider struct{ calls int }

func (f *fakeProvider) Login(_ context.Context, req LoginRequest) (*User, error) {
	f.calls++
	if req.Password != "right" {
		return nil, response.BadRequest("auth.invalid_credentials")
	}
	return &User{ID: idgen.ID(1), Username: req.Username}, nil
}
func (f *fakeProvider) Profile(context.Context, idgen.ID) (*User, error) { return nil, nil }

func loginEngine(t *testing.T) (*gin.Engine, *Service, *fakeProvider) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s := newCaptchaSvc(t)
	s.secret, s.accessTTL, s.refreshTTL = []byte(strings.Repeat("k", 32)), time.Minute, time.Hour
	fp := &fakeProvider{}
	s.SetProvider(fp)
	h := &Handler{Svc: s, Cfg: &config.Config{}}
	e := gin.New()
	e.POST("/login", h.Login)
	e.GET("/captcha", h.Captcha)
	return e, s, fp
}

type loginResp struct {
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

func doLogin(t *testing.T, e *gin.Engine, user, pw, key, captcha string) loginResp {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"type": "password", "username": user, "password": pw, "captchaKey": key, "captcha": captcha})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.9:1234"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	var out loginResp
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func TestLoginRequiresCaptchaAfterThreeFailures(t *testing.T) {
	e, s, fp := loginEngine(t)
	// 前 3 次错误（账号存在与否对外表现一致）：第 3 次响应开始带 CAPTCHA_REQUIRED
	for i := 1; i <= 3; i++ {
		r := doLogin(t, e, "ghost", "bad", "", "")
		if r.Code != 400 {
			t.Fatalf("attempt %d: want 400 got %d", i, r.Code)
		}
		if (r.Reason == ReasonCaptchaRequired) != (i == 3) {
			t.Fatalf("attempt %d: unexpected reason %q", i, r.Reason)
		}
	}
	calls := fp.calls
	// 第 4 次不带验证码：直接拒绝，且不会去校验密码
	if r := doLogin(t, e, "ghost", "right", "", ""); r.Code != 400 || r.Reason != ReasonCaptchaRequired {
		t.Fatalf("missing captcha must be rejected, got %+v", r)
	}
	if fp.calls != calls {
		t.Fatal("provider must not be called without a valid captcha")
	}
	// 验证码错误同样被拒，并且该验证码作废（签发/校验都走 10.0.0.9，与 doLogin 的 RemoteAddr 一致）
	key, _, _ := s.NewCaptcha(context.Background(), testIP)
	ans := answerOf(t, s, key)
	if r := doLogin(t, e, "ghost", "right", key, "x"+ans); r.Code != 400 {
		t.Fatalf("wrong captcha must be rejected, got %+v", r)
	}
	if r := doLogin(t, e, "ghost", "right", key, ans); r.Code != 400 {
		t.Fatalf("consumed captcha must be rejected, got %+v", r)
	}
	// 正确的验证码 + 正确的密码：登录成功并清除该用户名的计数
	key, _, _ = s.NewCaptcha(context.Background(), testIP)
	if r := doLogin(t, e, "ghost", "right", key, answerOf(t, s, key)); r.Code != 0 {
		t.Fatalf("valid captcha + password must succeed, got %+v", r)
	}
	if need, _ := s.LoginNeedsCaptcha(context.Background(), "ghost", "other-ip"); need {
		t.Fatal("success must clear the username counter")
	}
}
