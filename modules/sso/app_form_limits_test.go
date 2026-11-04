package sso

import (
	"strings"
	"testing"
)

// 长度闸按字符计：与列宽 varchar(500) 与报错文案「最多 500 个字符」同一口径。
// 成对边界（恰好 N 放行 / N+1 拒）；描述那条同时是回归探针——改回 len() 量字节，500 个汉字（1500 字节）就会被拒。
func TestAppFormLengths(t *testing.T) {
	valid := func() *appForm {
		return &appForm{Name: "n", RedirectURIs: []string{"https://acme.example/cb"}}
	}
	normKey := func(t *testing.T, f *appForm) string {
		t.Helper()
		e := f.normalize()
		if e == nil {
			return ""
		}
		return e.Key
	}

	t.Run("名称 100 字符放行 / 101 拒", func(t *testing.T) {
		f := valid()
		f.Name = strings.Repeat("a", 100)
		if got := normKey(t, f); got != "" {
			t.Errorf("恰好 100 字符的名称被拒：%s", got)
		}
		f = valid()
		f.Name = strings.Repeat("a", 101)
		if got := normKey(t, f); got != "sso.name_required" {
			t.Errorf("101 字符的名称：报错 = %s，想要 sso.name_required", got)
		}
	})

	t.Run("描述 500 个汉字放行 / 501 拒", func(t *testing.T) {
		f := valid()
		f.Description = strings.Repeat("说", 500) // 1500 字节：按字节判会误拒
		if got := normKey(t, f); got != "" {
			t.Errorf("500 个汉字的描述被拒：%s（这是字节口径的回归）", got)
		}
		f = valid()
		f.Description = strings.Repeat("说", 501)
		if got := normKey(t, f); got != "sso.logo_description_too_long" {
			t.Errorf("501 个汉字的描述：报错 = %s，想要 sso.logo_description_too_long", got)
		}
	})

	t.Run("Logo 同档", func(t *testing.T) {
		f := valid()
		f.Logo = strings.Repeat("图", 500)
		if got := normKey(t, f); got != "" {
			t.Errorf("500 个汉字的 Logo 地址被拒：%s", got)
		}
		f = valid()
		f.Logo = "https://cdn.example.com/" + strings.Repeat("x", 476) // 前缀 24 字符 + 476 = 恰好 500
		if got := normKey(t, f); got != "" {
			t.Errorf("恰好 500 字符的 Logo 被拒：%s", got)
		}
		f = valid()
		f.Logo = "https://cdn.example.com/" + strings.Repeat("x", 477) // 501 字符
		if got := normKey(t, f); got != "sso.logo_description_too_long" {
			t.Errorf("501 字符的 Logo：报错 = %s", got)
		}
	})
}
