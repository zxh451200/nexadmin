package identity

import (
	"os"
	"testing"

	"nexadmin/core/config"
	corei18n "nexadmin/core/i18n"
)

// TestMain 装载全局词条目录：生产里由 core 启动时装，测试里自己装一份，
// 这样「测试连接」的诊断文案断言到的才是真实文案而不是 key。
func TestMain(m *testing.M) {
	if b, err := corei18n.New([]config.Language{{Code: "zh-CN", Default: true}, {Code: "en-US"}}, "zh-CN", "none"); err == nil {
		corei18n.SetDefault(b)
	}
	os.Exit(m.Run())
}
