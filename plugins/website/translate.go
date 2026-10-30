package website

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	moduleai "nexadmin/modules/ai"
)

// entityPerm 把一个内容实体映射到它自己的编辑权限码。
//
// 机翻接口按实体各注册一条、各挂一份权限，而不是往 core 的 translatePerms 里加
// "website:post"——那等于让内核认识某个插件。
type entityPerm struct {
	entity string
	perm   string
}

var translateRoutes = []entityPerm{
	{"setting", "website:setting"},
	{"nav", "website:nav"},
	{"feature", "website:feature"},
	{"page", "website:page"},
	{"post", "website:post"},
}

// newAIGateway 装配翻译用的 AI 网关。AI 未配置时调用会失败，接口回 503 而不是静默无响应。
func newAIGateway(d *core.Deps) *ai.Gateway {
	loc, err := time.LoadLocation(d.Config.App.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return moduleai.NewGateway(d, loc)
}

// translateForm 是一次批量机翻请求。同一批必须是同一种格式——Format 是请求级的，
// 所以前端把纯文本字段与富文本字段分两批发。
type translateForm struct {
	Texts  []string `json:"texts"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Format string   `json:"format"`
}

// makeTranslator 给某个内容实体生成一个机翻处理函数。
// 只回译文、不落库：写库仍走各自的 save 接口，由 normalizeI18nJSON 统一清洗与裁剪。
func (h *handler) makeTranslator(entity string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var f translateForm
		if err := c.ShouldBindJSON(&f); err != nil {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		f.From, f.To = strings.TrimSpace(f.From), strings.TrimSpace(f.To)
		if len(f.Texts) == 0 || len(f.Texts) > ai.MaxBatchItems {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		if f.Format != "html" {
			f.Format = "text"
		}
		// from 必须是本站默认语言、to 必须是已配置的非默认语言：
		// 否则会翻出一个根本没有路由的地址（两个语言清单互不可见，这里只认官网那份）。
		def := DefaultLanguage()
		if f.From != def.Code {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		if l, ok := LanguageByCode(f.To); !ok || l.Default {
			response.Fail(c, response.BadRequest("error.bad_request"))
			return
		}
		// 参数校验排在网关之前：坏请求不该占用限流次数，AI 未配置也要能报出 400/503 的区别。
		if h.gw == nil {
			response.Fail(c, response.Unavailable("error.unavailable"))
			return
		}
		out, err := h.gw.Translate(c.Request.Context(), aiCaller(c), ai.TranslateRequest{
			Texts: f.Texts, From: f.From, To: f.To, Format: f.Format,
		})
		if err != nil {
			// 上游原因（配额、模型报错）只进日志：响应给调用方的是可读的通用文案。
			response.Fail(c, response.New(502, "website.translate_failed").Wrap(err))
			return
		}
		response.OK(c, gin.H{"entity": entity, "texts": out})
	}
}

// aiCaller 与内置 AI 接口用同一套调用者身份：配额与租户开关按解析出的租户计。
func aiCaller(c *gin.Context) ai.Caller {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return ai.Caller{}
	}
	tid := cl.TenantID
	if id, ok := tenant.From(c.Request.Context()); ok {
		tid = id
	}
	return ai.Caller{
		UserID:   strconv.FormatInt(int64(cl.UserID), 10),
		TenantID: strconv.FormatInt(int64(tid), 10),
		Exempt:   cl.Super || tid == tenant.DefaultID,
	}
}
