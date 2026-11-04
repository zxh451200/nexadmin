package openapi

import (
	"strings"
	"testing"
)

// 开放平台表单的长度闸按字符计（utf8.RuneCountInString），前端三个表单的 100/500 以此为准。
// 成对边界：恰好 N 与短值的判定必须一致（长度不是触发原因），N+1 必须命中长度错误键。
// 用「与基线同结果」比断言放行，能避免被角色/事件等其它必填项干扰。
func TestFormLengthGates(t *testing.T) {
	appErrKey := func(t *testing.T, name, desc string) string {
		t.Helper()
		f := &appForm{Name: name, Description: desc}
		if e := f.normalize(); e != nil {
			return e.Key
		}
		return ""
	}
	hookErrKey := func(t *testing.T, name string) string {
		t.Helper()
		f := &hookForm{Name: name, URL: "https://acme.example/hook"}
		if e := f.normalize(); e != nil {
			return e.Key
		}
		return ""
	}
	hookURLKey := func(t *testing.T, url string) string {
		t.Helper()
		f := &hookForm{Name: "n", URL: url}
		if e := f.normalize(); e != nil {
			return e.Key
		}
		return ""
	}

	t.Run("应用名 100 与短名同判 / 101 命中长度键", func(t *testing.T) {
		base := appErrKey(t, "n", "")
		if got := appErrKey(t, strings.Repeat("a", 100), ""); got != base {
			t.Errorf("恰好 100 字符的名称判定 = %q，与短名称 %q 不同，说明长度闸误触发", got, base)
		}
		if got := appErrKey(t, strings.Repeat("a", 101), ""); got != "openapi.name_required" {
			t.Errorf("101 字符名称：报错 = %q，想要 openapi.name_required", got)
		}
	})

	t.Run("应用描述 500 个汉字与短描述同判 / 501 命中长度键", func(t *testing.T) {
		base := appErrKey(t, "n", "短描述")
		if got := appErrKey(t, "n", strings.Repeat("说", 500)); got != base {
			t.Errorf("500 个汉字的描述判定 = %q，与短描述 %q 不同（字节口径回归？）", got, base)
		}
		if got := appErrKey(t, "n", strings.Repeat("说", 501)); got != "openapi.description_too_long" {
			t.Errorf("501 字符描述：报错 = %q，想要 openapi.description_too_long", got)
		}
	})

	t.Run("Webhook 名 100 与短名同判 / 101 命中长度键", func(t *testing.T) {
		base := hookErrKey(t, "n")
		if got := hookErrKey(t, strings.Repeat("a", 100)); got != base {
			t.Errorf("恰好 100 字符的 Webhook 名判定 = %q，与短名 %q 不同", got, base)
		}
		if got := hookErrKey(t, strings.Repeat("a", 101)); got != "openapi.name_required" {
			t.Errorf("101 字符 Webhook 名：报错 = %q，想要 openapi.name_required", got)
		}
	})

	t.Run("Webhook 地址按字符不按字节", func(t *testing.T) {
		// 498 个字符但 1494 字节：按 len() 计数会误报「地址过长」，中文/emoji 地址存不进库。
		longMultibyte := "https://a.example/" + strings.Repeat("路", 480)
		if got := hookURLKey(t, longMultibyte); got == "openapi.url_too_long" {
			t.Errorf("498 字符（%d 字节）的地址被长度闸拒了：口径回到字节了？", len(longMultibyte))
		}
		// 501 个字符必须命中长度键（闸在 URL 合法性校验之前，别让它漂到别的错误上）。
		if got := hookURLKey(t, "https://a.example/"+strings.Repeat("a", 483)); got != "openapi.url_too_long" {
			t.Errorf("501 字符地址：报错 = %q，想要 openapi.url_too_long", got)
		}
	})
}
