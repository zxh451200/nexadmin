// Package language 是平台语言清单的管理接口。清单本身由 core 持有（表 sys_language），
// 这里只做读写——不在模块里再存一份语言表，那正是这次统一要消掉的东西。
package language

import (
	"sort"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "language" }

// Register 注册语言清单读写。权限码：language:list | language:update。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &handler{deps: d}
	g := r.Group("/languages")
	g.GET("", core.Perm("language:list"), h.list)
	g.PUT("", core.Perm("language:update"), core.Desc("保存语言清单"), h.save)
}

type handler struct{ deps *core.Deps }

// list 返回当前生效的清单：与界面文案协商、公开站路由用的是同一份。
func (h *handler) list(c *gin.Context) {
	response.OK(c, core.Languages())
}

type languageForm struct {
	Languages []core.Language `json:"languages"`
}

// save 整表替换。校验规则、落库、重建文案 bundle 都在 core 里，这里不重复实现一份。
//
// needRestart 只在「路由形状变了」时为真：公开站按语言前缀注册路由发生在引擎构建期，
// 运行中加不了路由；只改名称/日期格式这类展示字段不需要重启，别让用户白重启一次。
func (h *handler) save(c *gin.Context) {
	var f languageForm
	if err := c.ShouldBindJSON(&f); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	before := core.Languages()
	list, err := core.SaveLanguageList(h.deps.DB, h.deps.Config, f.Languages)
	if err != nil {
		// 校验原因（哪个码非法、哪个前缀撞了）只进日志，响应给通用文案。
		response.Fail(c, response.BadRequest("language.save_failed").Wrap(err))
		return
	}
	response.OK(c, gin.H{"languages": list, "needRestart": routesChanged(before, list)})
}

// routesChanged 比较两版清单的「码 + 前缀」集合；顺序与展示字段不算变化。
func routesChanged(before, after []core.Language) bool {
	key := func(list []core.Language) []string {
		out := make([]string, 0, len(list))
		for _, l := range list {
			out = append(out, l.Code+"|"+l.Prefix)
		}
		sort.Strings(out)
		return out
	}
	a, b := key(before), key(after)
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}
