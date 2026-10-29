package website

import "nexadmin/core"

// 语言清单是平台级配置（表 sys_language，见 core/languages.go）：官网不再自己存一份。
//
// 之前是两份互不可见的清单——框架那份在 config.yaml，官网那份在 plg_website_language。
// 加一门语言只加在其中一边时，另一边静默缺：i18n-editor 会拒绝官网加的语言，
// 官网的机翻又会翻出一个根本没有路由的地址。
//
// 公开站按语言前缀注册路由发生在引擎构建期，运行中加不了路由，所以改完清单仍需重启才生效。

// LoadLanguages 用平台清单重建官网运行期语言表与字典。
// 传入的是 core 在迁移之后从 sys_language 读回的那一份（见 App.reloadLanguages）；
// 空清单不动现状，避免热挂载或测试装配时把官网语言清空。
func LoadLanguages(list []core.Language) error {
	if len(list) == 0 {
		return nil
	}
	return applyLanguages(fromPlatform(list))
}

// fromPlatform 把平台语言换成官网视角：官网的展示字段叫 Label，其余同名。
func fromPlatform(list []core.Language) []Language {
	out := make([]Language, 0, len(list))
	for _, l := range list {
		out = append(out, Language{
			Code:       l.Code,
			Label:      l.Name,
			Short:      l.Short,
			HTMLLang:   l.HTMLLang,
			OGLocale:   l.OGLocale,
			Prefix:     l.Prefix,
			DateFormat: l.DateFormat,
			Default:    l.Default,
		})
	}
	return out
}
