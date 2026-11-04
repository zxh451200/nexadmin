package codegen

import "nexadmin/core/database"

// CodegenConfig 是一份保存下来的生成配置：向导里存，下次直接载入继续改。
// 只存配置不存生成物，代码永远现场渲染；同名即更新（配置跟着人改，不做版本历史）。
type CodegenConfig struct {
	database.PlatformModel
	Name   string `gorm:"size:64;not null;uniqueIndex:uk_plg_codegen_config_name" json:"name"`
	Module string `gorm:"size:32;not null" json:"module"`
	// GoModule 留空表示用所在 Go 项目的模块路径（见 detectGoModule）。
	GoModule string `gorm:"size:100;not null;default:''" json:"goModule"`
	// Table 是来源表；新建表模式为空，定义在 DefineJSON 里。
	Table string `gorm:"column:table_name;size:64;not null;default:''" json:"tableName"`
	// DefineJSON / ColumnsJSON / SubJSON 是 TableDef / []ColumnConfig / SubTableSpec 的 JSON 文本；空 = 没有对应部分。
	DefineJSON  string `gorm:"column:define_json;type:text" json:"-"`
	ColumnsJSON string `gorm:"column:columns_json;type:text" json:"-"`
	SubJSON     string `gorm:"column:sub_json;type:text" json:"-"`
}

func (CodegenConfig) TableName() string { return "plg_codegen_config" }
