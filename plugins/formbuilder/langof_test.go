package formbuilder

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	corei18n "nexadmin/core/i18n"
)

// 导出表头的语言必须来自框架协商结果，而不是「Accept-Language 是不是 en 开头」的二元判断。
// 这条用例的靶子正是第三种语言：协商成 ja-JP 时，旧写法会返回 zh-CN。
func TestLangOfUsesNegotiatedLanguage(t *testing.T) {
	for _, lang := range []string{"ja-JP", "en-US", "zh-CN"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/x", nil).WithContext(corei18n.WithLang(context.Background(), lang))
		c.Request = req
		if got := langOf(c); got != lang {
			t.Errorf("协商语言 %s → langOf = %q, want %q", lang, got, lang)
		}
	}
}

// 头里写着 en 也不能作数：协商结果才是唯一口径（它按平台清单挑选，可能落到默认语言）。
// 旧实现只看头，这条会红。
func TestLangOfIgnoresRawHeaderWhenNegotiated(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("GET", "/x", nil).WithContext(corei18n.WithLang(context.Background(), "ja-JP"))
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	c.Request = req
	if got := langOf(c); got != "ja-JP" {
		t.Errorf("langOf = %q, want ja-JP（协商结果优先于原始头）", got)
	}
}
