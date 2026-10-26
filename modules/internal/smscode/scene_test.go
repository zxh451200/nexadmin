package smscode

import (
	"context"
	"testing"
	"time"

	"nexadmin/core/notify"
)

// 场景隔离：一个场景发的验证码不能在另一个场景校验通过；同场景正常。
// 冷却仍按手机号共享：换场景立即重发也会被 60 秒冷却挡住。
func TestSceneIsolatedCodes(t *testing.T) {
	s, rec, clk := newSvc()
	login := context.Background() // 默认 verify_code（登录）
	forgot := WithScene(context.Background(), notify.SceneForgot)
	bind := WithScene(context.Background(), notify.SceneBindPhone)

	if err := s.Send(forgot, phone, "1.1.1.1", true); err != nil {
		t.Fatal(err)
	}
	code := rec.code

	// 登录场景校验忘记密码场景的验证码：必须失败
	if err := s.Verify(login, phone, code); err == nil {
		t.Fatal("forgot code accepted in login scene")
	}
	// 绑定场景同样必须失败
	if err := s.Verify(bind, phone, code); err == nil {
		t.Fatal("forgot code accepted in bind scene")
	}
	// 原场景校验成功（且一次性）
	if err := s.Verify(forgot, phone, code); err != nil {
		t.Fatalf("correct scene rejected: %v", err)
	}
	if err := s.Verify(forgot, phone, code); err == nil {
		t.Fatal("code must be single use")
	}

	// 冷却跨场景共享：换场景重发也撞 60 秒冷却。
	// （测试用 fakeCache 走 Incr 回落路径会重置 TTL：被拒的那次把冷却延到 t+90，
	// 所以这里推到 t+92 再验证可重发；真实驱动的 Incr 不会延长 TTL。）
	clk.Advance(30 * time.Second)
	if err := s.Send(bind, phone, "1.1.1.1", true); err == nil || !isStatus(err, 429) {
		t.Fatalf("cross-scene resend must hit shared cooldown, got %v", err)
	}
	clk.Advance(62 * time.Second)
	if err := s.Send(bind, phone, "1.1.1.1", true); err != nil {
		t.Fatalf("send after cooldown: %v", err)
	}
	if err := s.Verify(bind, phone, rec.code); err != nil {
		t.Fatalf("bind scene code rejected: %v", err)
	}
}
