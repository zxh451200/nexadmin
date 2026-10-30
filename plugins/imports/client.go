package imports

import (
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/pkg/idgen"
)

// client.go 是宿主侧接入面。业务模块有两种接法,都不需要改导入中心或框架代码:
//
//  1. 约定式(推荐):模块实现 SourceProvider(即 pkg/importspec.Provider)。核心把本次启动
//     启用的模块交给插件,插件在挂载时自动收集——模块里写一个方法即可,没有第二处要记得改的接线。
//  2. 手工:在模块的 Register 里调 RegisterSource。适合来源随运行时条件变化(开关、
//     配置里出现某张表)的场景。
//
// 两种方式的来源重名都会报错,收集时记 warning 并跳过后者。
var _ = idgen.Next // 保持 idgen 依赖显性(当前插件内不直接发号,宿主 Handler 自行处理)

// collect 遍历本次启动启用的模块,把实现 SourceProvider 的模块注册进来。
// 插件模块在内核里是包装过的(core.Unwrapper),先拆包再断言,否则插件实现的来源看不见。
func collect(mods []core.Module, db *gorm.DB, warn func(string, error)) {
	for _, m := range mods {
		inner := core.UnwrapModule(m)
		p, ok := inner.(SourceProvider)
		if !ok {
			continue
		}
		for _, s := range p.ImportSources(db) {
			if err := RegisterSource(s); err != nil && warn != nil {
				warn("imports: 模块 "+inner.Name()+" 的导入源未注册", err)
			}
		}
	}
}
