package website

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	pi18n "nexadmin/pkg/i18n"
)

// 管理端翻译表单的元数据。字段白名单与长度上限在 i18n.go，这里只补「怎么渲染」和
// 表单顺序（Go 的 map 无序，顺序必须显式声明），让前端不必再抄一份会漂移的清单。

// 控件类型，与前端 FieldWidget 取值一致。
const (
	widgetText     = "text"
	widgetTextarea = "textarea"
	widgetRich     = "rich"
)

// localizableField 是下发给管理端表单的一个可翻译字段。
type localizableField struct {
	Key      string `json:"key"`
	LabelKey string `json:"labelKey"`
	Widget   string `json:"widget"`
	// Max 直接取白名单，前端据此设 maxLength，避免后端静默截断。
	Max int `json:"max,omitempty"`
	// Rows 只是渲染偏好，不参与校验。
	Rows int `json:"rows,omitempty"`
}

// entitySchema 是一种内容的翻译面。
// Publishable 只对带 published 裸列的内容成立：导航项与特性卡片没有可下线的 URL，
// 缺译靠逐字段回退原文就够了，给它们一个「按语言发布」开关只会让人误以为能分开上下线。
type entitySchema struct {
	Entity       string             `json:"entity"`
	Translatable []localizableField `json:"translatable"`
	Publishable  bool               `json:"publishable"`
}

// fieldView 是字段的渲染说明；长度与白名单资格仍由 pkg/i18n 的 Fields 说了算。
type fieldView struct {
	key    string
	label  string
	widget string
	rows   int
}

// entityDef 把「用哪张白名单 + 表单顺序 + 控件」绑在一起。
// 新增可翻译字段时必须同时改 i18n.go 的白名单和这里，i18nSchemaFor 会拒绝
// 出现在视图里却不在白名单中的字段，配套测试再反向查一遍漏项。
type entityDef struct {
	fields      pi18n.Fields
	views       []fieldView
	publishable bool
}

var entityViews = map[string]entityDef{
	"setting": {
		fields: settingI18nFields,
		views: []fieldView{
			{"siteDesc", "plugin_website.fields.siteDesc", widgetTextarea, 2},
			{"homeTitle", "plugin_website.fields.homeTitle", widgetTextarea, 2},
			{"homeSubtitle", "plugin_website.fields.homeSubtitle", widgetTextarea, 3},
			{"homeCtaLabel", "plugin_website.fields.homeCtaLabel", widgetText, 0},
			{"seoKeywords", "plugin_website.fields.seoKeywords", widgetTextarea, 2},
			{"footerNote", "plugin_website.fields.footerNote", widgetTextarea, 2},
		},
	},
	"nav": {
		fields: navI18nFields,
		views:  []fieldView{{"label", "plugin_website.fields.navLabel", widgetText, 0}},
	},
	"feature": {
		fields: featureI18nFields,
		views: []fieldView{
			{"title", "plugin_website.fields.featureTitle", widgetText, 0},
			{"description", "plugin_website.fields.featureDesc", widgetTextarea, 3},
		},
	},
	"page": {
		fields: pageI18nFields,
		views: []fieldView{
			{"title", "plugin_website.fields.pageTitle", widgetText, 0},
			{"seoDesc", "plugin_website.fields.seoDesc", widgetText, 0},
			{"content", "plugin_website.fields.content", widgetRich, 0},
		},
		publishable: true,
	},
	"post": {
		fields: postI18nFields,
		views: []fieldView{
			{"title", "plugin_website.fields.postTitle", widgetText, 0},
			{"summary", "plugin_website.fields.summary", widgetTextarea, 2},
			{"seoDesc", "plugin_website.fields.seoDesc", widgetTextarea, 2},
			{"content", "plugin_website.fields.content", widgetRich, 0},
		},
		publishable: true,
	},
}

// i18nSchemaFor 生成某实体的表单元数据；实体未知返回 false。
func i18nSchemaFor(entity string) (entitySchema, bool) {
	spec, ok := entityViews[entity]
	if !ok {
		return entitySchema{}, false
	}
	out := entitySchema{Entity: entity, Publishable: spec.publishable}
	for _, v := range spec.views {
		f, ok := spec.fields[v.key]
		if !ok {
			// 白名单里没有的字段存不进库，宁可不给前端渲染，也别做出一个「填了不保存」的输入框。
			continue
		}
		out.Translatable = append(out.Translatable, localizableField{
			Key: v.key, LabelKey: v.label, Widget: v.widget, Max: f.Max, Rows: v.rows,
		})
	}
	return out, true
}

// i18nSchema 下发某实体翻译表单的字段元数据。
// 与 /languages 一样不设权限码：内容里只有字段名，而 5 个弹窗各自挂在不同权限后面。
func (h *handler) i18nSchema(c *gin.Context) {
	schema, ok := i18nSchemaFor(c.Param("entity"))
	if !ok {
		respond(c, nil, response.BadRequest("error.bad_request"))
		return
	}
	respond(c, schema, nil)
}
