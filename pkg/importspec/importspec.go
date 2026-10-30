// Package importspec 是「可导入数据源」的中立契约：插件声明自己有哪些数据能被
// 通用导入中心读取，导入中心按接口自动收集。契约放在 pkg/ 而不是导入中心插件里，
// 是为了让提供方（业务插件）只依赖中立包，而不是反向依赖导入中心插件本身。
package importspec

import (
	"context"

	"gorm.io/gorm"
)

// Column 是导入模板的一列。json 标签必须小写：前端 ImportColumn 按 key/label/required/example 取值。
type Column struct {
	Key      string `json:"key"`      // 列标识(表头用 Label,数据行按 Key 映射)
	Label    string `json:"label"`    // 模板表头(给用户看的名字)
	LabelKey string `json:"-"`        // 可选 i18n key:有值时按请求语言取文案,Label 作兜底
	Required bool   `json:"required"` // 缺失/空白即该行失败
	Example  string `json:"example"`  // 模板里的示例值
}

// Source 是一个可导入的数据源。
// Handler 逐行回调:row 是 key→值(已去空白),返回 error 即该行失败(错误进报告,不影响其它行)。
// Handler 内部自行处理落库(建议逐行独立提交,避免一行失败整批回滚)。
type Source struct {
	Name     string // 唯一标识(如 "crm_customers")
	Title    string // 显示名(如 "客户导入")
	TitleKey string // 可选 i18n key:有值时按请求语言取文案,Title 作兜底
	Columns  []Column
	Handler  func(ctx context.Context, rowIndex int, row map[string]string) error
}

// Provider 由业务插件实现：导入中心启动时扫描已启用模块，自动收集它们导出的源。
// 与注册表的手工注册等价，区别是来源随模块启停自动出现，不需要第二处接线。
type Provider interface {
	ImportSources(db *gorm.DB) []Source
}
