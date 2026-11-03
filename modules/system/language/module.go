// Package language 是平台语言清单的管理接口。清单本身由 core 持有（表 sys_language），
// 这里只做读写——不在模块里再存一份语言表，那正是这次统一要消掉的东西。
package language

import (
	"sort"
	"strings"

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

// list 返回全量清单（含停用的语言）：管理页要能再启用停用行；
// 生效集（只含启用的）用 core.Languages()，协商与路由消费的正是它。
func (h *handler) list(c *gin.Context) {
	response.OK(c, core.AllLanguages())
}

type languageForm struct {
	Languages []core.Language `json:"languages"`
	// Base 是客户端编辑时的语言码集合（排序后逗号拼接）：非空时与服务端当前集合比对，
	// 不一致说明清单刚被别人改过（乐观锁）。空串 = 旧客户端，跳过比对。
	Base string `json:"base"`
}

// save 整表替换。校验规则、落库、重建文案 bundle 都在 core 里，这里不重复实现一份。
//
// needRestart 只在「生效集的路由形状变了」时为真：公开站按语言前缀注册路由发生在
// 引擎构建期，运行中加不了路由；只改名称/日期格式/启停这类不需要重启，
// 启停不重启是因为停用语言的接口本来就不在运行中的引擎上。
func (h *handler) save(c *gin.Context) {
	var f languageForm
	if err := c.ShouldBindJSON(&f); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	// 乐观锁：整表替换无法按行合并，两个管理员同时保存是互相覆盖——
	// 用「编辑时的语言码集合」守住结构性冲突，字段级编辑的小窗口冲突接受。
	if base := codesKey(core.AllLanguages()); f.Base != "" && f.Base != base {
		response.Fail(c, response.Conflict("language.conflict"))
		return
	}
	before := core.Languages()
	list, err := core.SaveLanguageList(h.deps.DB, h.deps.Config, f.Languages)
	if err != nil {
		// 校验原因（哪个码非法、哪个前缀撞了）只进日志，响应给通用文案。
		response.Fail(c, response.BadRequest("language.save_failed").Wrap(err))
		return
	}
	// needRestart 比较的是保存前后「生效集」的形状：停用/启用语言不改运行中引擎的路由。
	response.OK(c, gin.H{"languages": list, "needRestart": routesChanged(before, core.Languages())})
}

// routesChanged 比较两版清单的「码 + 前缀」集合；顺序与展示字段不算变化。
func routesChanged(before, after []core.Language) bool {
	a, b := codesAndPrefixes(before), codesAndPrefixes(after)
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

func codesAndPrefixes(list []core.Language) []string {
	out := make([]string, 0, len(list))
	for _, l := range list {
		out = append(out, l.Code+"|"+l.Prefix)
	}
	sort.Strings(out)
	return out
}

// codesKey 是乐观锁的指纹：语言码集合（排序逗号拼接）。不含启停与展示字段——
// 那些是字段级编辑，用同一个指纹会误伤「他只改了个名称」的正常保存。
func codesKey(list []core.Language) string {
	codes := make([]string, 0, len(list))
	for _, l := range list {
		codes = append(codes, l.Code)
	}
	sort.Strings(codes)
	return strings.Join(codes, ",")
}
