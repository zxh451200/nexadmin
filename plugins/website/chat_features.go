package website

import (
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/ai"
	"nexadmin/core/response"
)

// visitorTranslateForm 是匿名聊天翻译请求：前端只给文本和方向，不给语言码，
// 避免访客伪造语言把 AI 翻到站点未配置的目标上。
type visitorTranslateForm struct {
	Text string `json:"text"`
	Dir  string `json:"dir"`  // out = 访客发出前；in = 收到客服消息
	Lang string `json:"lang"` // 当前页面语言，必须在站点语言清单里
}

// visitorTranslate 给官网客服挂件提供单条消息机翻。
// 方向由「站点默认语言（客服侧）」与「当前页面语言（访客侧）」推导：
// 默认语言站上 dir=out 时两者相同，直接原样返回，不浪费一次模型调用。
// 匿名入口有独立 IP 限流，Caller 也按 IP 分散，避免所有访客共用一个 AI 配额桶。
func (h *siteHandler) visitorTranslate(c *gin.Context) {
	var f visitorTranslateForm
	if err := c.ShouldBindJSON(&f); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	text := strings.TrimSpace(f.Text)
	if text == "" || utf8.RuneCountInString(text) > 1000 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	// 访客语言由页面显式上报（无前缀接口拿不到语言前缀），必须在已配置语言里；
	// 不传或非法才回落到中间件协商出的语言。
	visitorLang := langOf(c).Code
	if _, ok := LanguageByCode(f.Lang); ok {
		visitorLang = f.Lang
	}
	from, to := SiteDefaultLanguage().Code, visitorLang
	if f.Dir == "out" {
		from, to = to, from
	}
	if strings.EqualFold(from, to) {
		response.OK(c, gin.H{"text": text, "skipped": true})
		return
	}
	if h.gw == nil {
		response.Fail(c, response.Unavailable("error.unavailable"))
		return
	}
	who := ai.Caller{UserID: "visitor", TenantID: c.ClientIP()}
	out, err := h.gw.Translate(c.Request.Context(), who, ai.TranslateRequest{
		Texts: []string{text}, From: from, To: to, Format: "text",
	})
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 访客消息翻译失败", zap.Error(err))
		}
		response.Fail(c, response.New(502, "website.translate_failed").Wrap(err))
		return
	}
	response.OK(c, gin.H{"text": out[0], "from": from, "to": to})
}
