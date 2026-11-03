package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
)

func audioEngine(t *testing.T) (*gin.Engine, *Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	s := newCaptchaSvc(t)
	h := &Handler{Svc: s, Cfg: &config.Config{}}
	e := gin.New()
	e.GET("/captcha/:key/audio", h.CaptchaAudio)
	return e, s
}

func TestCaptchaAudioIsWAV(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, err := s.NewCaptcha(ctx, testIP)
	if err != nil {
		t.Fatal(err)
	}
	wav, err := s.CaptchaAudio(ctx, key, testIP, "zh-CN")
	if err != nil {
		t.Fatalf("CaptchaAudio: %v", err)
	}
	if len(wav) < 1000 {
		t.Fatalf("音频只有 %d 字节，不像真的语音数据", len(wav))
	}
	if !bytes.HasPrefix(wav, []byte("RIFF")) || string(wav[8:12]) != "WAVE" {
		t.Fatalf("不是 WAV：前 12 字节 %q", wav[:12])
	}
}

// 听音频不能把验证码消费掉：听完还能再听，答案也仍然只能校验一次。
func TestCaptchaAudioDoesNotConsume(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _ := s.NewCaptcha(ctx, testIP)
	for i := 0; i < 2; i++ {
		if _, err := s.CaptchaAudio(ctx, key, testIP, "en-US"); err != nil {
			t.Fatalf("第 %d 次取音频失败：%v", i+1, err)
		}
	}
	ans := answerOf(t, s, key)
	if ok, _ := s.VerifyCaptcha(ctx, key, ans, testIP); !ok {
		t.Fatal("听过音频后，答案仍应能校验一次")
	}
	if ok, _ := s.VerifyCaptcha(ctx, key, ans, testIP); ok {
		t.Fatal("校验一次后必须作废")
	}
}

// 不存在 / 已消费 / 来源 IP 不符，对外是同一种失败，不能用来探测 key。
func TestCaptchaAudioRejectsUnknownConsumedAndForeignIP(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()

	if _, err := s.CaptchaAudio(ctx, "deadbeef", testIP, "zh"); !errors.Is(err, errCaptchaUnavailable) {
		t.Fatalf("未知 key 应报不可用，got %v", err)
	}
	if _, err := s.CaptchaAudio(ctx, "", testIP, "zh"); !errors.Is(err, errCaptchaUnavailable) {
		t.Fatalf("空 key 应报不可用，got %v", err)
	}

	key, _, _ := s.NewCaptcha(ctx, testIP)
	if _, err := s.CaptchaAudio(ctx, key, "10.0.0.10", "zh"); !errors.Is(err, errCaptchaUnavailable) {
		t.Fatalf("换 IP 取音频应报不可用，got %v", err)
	}
	if _, err := s.CaptchaAudio(ctx, key, testIP, "zh"); err != nil {
		t.Fatalf("同 IP 取音频不该失败：%v", err)
	}
	_, _ = s.VerifyCaptcha(ctx, key, answerOf(t, s, key), testIP)
	if _, err := s.CaptchaAudio(ctx, key, testIP, "zh"); !errors.Is(err, errCaptchaUnavailable) {
		t.Fatalf("已消费的 key 取音频应报不可用，got %v", err)
	}
}

func TestAudioLangMapping(t *testing.T) {
	for in, want := range map[string]string{
		"zh-CN": "zh", "zh": "zh", "en-US": "en", "fr-FR": "en", "": "en", "ar": "en",
	} {
		if got := audioLang(in); got != want {
			t.Errorf("audioLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCaptchaAudioHandler(t *testing.T) {
	e, s := audioEngine(t)
	key, _, _ := s.NewCaptcha(context.Background(), testIP)

	req := httptest.NewRequest(http.MethodGet, "/captcha/"+key+"/audio?lang=zh", nil)
	req.RemoteAddr = testIP + ":1234"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d: %s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			Audio string `json:"audio"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != 0 || !strings.HasPrefix(out.Data.Audio, "data:audio/wav;base64,") {
		t.Fatalf("响应不是音频 data URI：%+v", out)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(out.Data.Audio, "data:audio/wav;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("RIFF")) {
		t.Errorf("解出来不是 WAV：%q", raw[:min(12, len(raw))])
	}

	// 别的来源拿同一个 key 取音频：404，且不泄露原因
	req2 := httptest.NewRequest(http.MethodGet, "/captcha/"+key+"/audio?lang=en", nil)
	req2.RemoteAddr = "10.0.0.11:1234"
	w2 := httptest.NewRecorder()
	e.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("foreign IP: want 404 got %d", w2.Code)
	}
}
