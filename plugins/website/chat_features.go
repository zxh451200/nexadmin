package website

import (
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/ai"
	"nexadmin/core/response"
)

// visitorTranslateForm 是匿名聊天翻译请求：前端只给文本和方向，语言码一律要在
// 站点语言清单里才算数，避免访客伪造语言把 AI 翻到站点未配置的目标上。
type visitorTranslateForm struct {
	Text  string   `json:"text"`  // 旧单条入口，texts 为空时收编成一条
	Texts []string `json:"texts"` // 批量入口：历史消息一次翻整屏，省限流额度
	Dir   string   `json:"dir"`   // out = 访客发出前；in = 收到客服消息
	Lang  string   `json:"lang"`  // 当前页面语言，必须在站点语言清单里
	From  string   `json:"from"`  // 可选：翻译设置里访客选的源语言
	To    string   `json:"to"`    // 可选：翻译设置里访客选的目标语言
}

// visitorTranslateMaxItems 是一次批量机翻的条数上限：面板一次最多拉 50 条历史，
// 分两批就够了；再大单请求耗时会明显拉长。
const visitorTranslateMaxItems = 20

// visitorTranslate 给官网客服挂件提供消息机翻（单条或批量，同一接口）。
// 方向由「站点默认语言（客服侧）」与「当前页面语言（访客侧）」推导；
// 翻译设置开启后，访客显式选的 from/to（仍须在站点语言清单里）覆盖默认方向。
// 同语言整批原样返回，不浪费一次模型调用。
// 匿名入口有独立 IP 限流，Caller 也按 IP 分散，避免所有访客共用一个 AI 配额桶。
func (h *siteHandler) visitorTranslate(c *gin.Context) {
	var f visitorTranslateForm
	if err := c.ShouldBindJSON(&f); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	texts := f.Texts
	if len(texts) == 0 && strings.TrimSpace(f.Text) != "" {
		texts = []string{f.Text}
	}
	if len(texts) == 0 || len(texts) > visitorTranslateMaxItems {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	for i, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" || utf8.RuneCountInString(t) > 1000 {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		texts[i] = t
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
	// 翻译设置的显式选择优先：选了才覆盖，没选/非法保持推导值，口径与 lang 一致。
	if l, ok := LanguageByCode(f.From); ok {
		from = l.Code
	}
	if l, ok := LanguageByCode(f.To); ok {
		to = l.Code
	}
	if strings.EqualFold(from, to) {
		response.OK(c, gin.H{"texts": texts, "text": texts[0], "from": from, "to": to, "skipped": true})
		return
	}
	if h.gw == nil {
		response.Fail(c, response.Unavailable("error.unavailable"))
		return
	}
	who := ai.Caller{UserID: "visitor", TenantID: c.ClientIP()}
	out, err := h.gw.Translate(c.Request.Context(), who, ai.TranslateRequest{
		Texts: texts, From: from, To: to, Format: "text",
	})
	if err != nil {
		if h.log != nil {
			h.log.Warn("website: 访客消息翻译失败", zap.Error(err))
		}
		response.Fail(c, response.New(502, "website.translate_failed").Wrap(err))
		return
	}
	response.OK(c, gin.H{"texts": out, "text": out[0], "from": from, "to": to})
}
