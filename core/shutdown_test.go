package core

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/ctl"
)

func shutdownReq(a *App, remote, token string) int {
	e := gin.New()
	a.registerShutdown(e)
	req := httptest.NewRequest(http.MethodPost, ctl.ShutdownPath, nil)
	req.RemoteAddr = remote
	if token != "" {
		req.Header.Set(ctl.ShutdownHeader, token)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Code
}

func stopped(a *App) bool {
	select {
	case <-a.stopc:
		return true
	default:
		return false
	}
}

func TestShutdownOnlyLoopbackWithToken(t *testing.T) {
	a := &App{shutdownToken: "tok", stopc: make(chan struct{})}

	// 非回环：即使令牌正确也是 404，且不会停服务
	if code := shutdownReq(a, "10.0.0.5:4000", "tok"); code != http.StatusNotFound || stopped(a) {
		t.Fatalf("remote caller: code=%d stopped=%v", code, stopped(a))
	}
	// 回环但令牌缺失 / 错误：403
	for _, tok := range []string{"", "nope"} {
		if code := shutdownReq(a, "127.0.0.1:4000", tok); code != http.StatusForbidden || stopped(a) {
			t.Fatalf("token %q: code=%d stopped=%v", tok, code, stopped(a))
		}
	}
	// 回环 + 正确令牌（IPv4 / IPv6 都算）：200 并请求退出
	if code := shutdownReq(a, "[::1]:4000", "tok"); code != http.StatusOK {
		t.Fatalf("loopback with token: code=%d", code)
	}
	<-a.stopc // requestStop 在 goroutine 里执行，等它关闭通道
}

func TestShutdownNotRegisteredWithoutToken(t *testing.T) {
	a := &App{stopc: make(chan struct{})} // 前台运行：没有令牌
	if code := shutdownReq(a, "127.0.0.1:4000", "anything"); code != http.StatusNotFound || stopped(a) {
		t.Fatalf("code=%d stopped=%v", code, stopped(a))
	}
}
