package website

import (
	"time"

	"nexadmin/core/database"
)

// 官网内容全部是平台级（官网代表整个部署主体，不随租户隔离）；表前缀 plg_website_。

// Setting 是站点设置，单行表（首次启动 seed 建行，之后一直更新这一行）。
type Setting struct {
	database.PlatformModel
	SiteName      string `gorm:"size:64;not null" json:"siteName"`
	SiteDesc      string `gorm:"size:300;not null" json:"siteDesc"`     // SEO 描述 / 副标题
	LogoURL       string `gorm:"size:255;not null" json:"logoUrl"`      // 空 = 用内置标志
	HomeTitle     string `gorm:"size:200;not null" json:"homeTitle"`    // 首页主标题
	HomeSubtitle  string `gorm:"size:500;not null" json:"homeSubtitle"` // 首页副文
	HomeCTALabel  string `gorm:"size:64;not null" json:"homeCtaLabel"`  // 主按钮文字，空 = 不显示
	HomeCTAHref   string `gorm:"size:255;not null" json:"homeCtaHref"`
	SeoKeywords   string `gorm:"size:300;not null" json:"seoKeywords"`
	ContactEmail  string `gorm:"size:128;not null" json:"contactEmail"`
	ICP           string `gorm:"size:128;not null" json:"icp"` // 备案号，空 = 页脚不显示
	FooterNote    string `gorm:"size:300;not null" json:"footerNote"`
	MarketEnabled bool   `gorm:"not null;default:true" json:"marketEnabled"` // 官网插件市场入口开关
	// I18nJSON 是非默认语言的字段译文：{"en-US":{"homeTitle":"…"}}；空 = 全部回退原文。
	I18nJSON string `gorm:"type:text" json:"i18nJson"`
}

func (Setting) TableName() string { return "plg_website_setting" }

// Nav 是顶部导航项。
type Nav struct {
	database.PlatformModel
	Label    string `gorm:"size:64;not null" json:"label"`
	Href     string `gorm:"size:255;not null" json:"href"`
	Sort     int    `json:"sort"`
	Enabled  bool   `json:"enabled"`
	I18nJSON string `gorm:"type:text" json:"i18nJson"`
}

func (Nav) TableName() string { return "plg_website_nav" }

// Feature 是首页特性卡片；Icon 是模板内置的图标名（lucide 风格，见 render.go 的 icon 集）。
type Feature struct {
	database.PlatformModel
	Icon        string `gorm:"size:64;not null" json:"icon"`
	Title       string `gorm:"size:128;not null" json:"title"`
	Description string `gorm:"size:500;not null" json:"description"`
	Sort        int    `json:"sort"`
	Enabled     bool   `json:"enabled"`
	I18nJSON    string `gorm:"type:text" json:"i18nJson"`
}

func (Feature) TableName() string { return "plg_website_feature" }

// Page 是自定义单页（关于我们、联系方式等），路径 /p/:slug，内容为富文本 HTML（落库前已 Sanitize）。
type Page struct {
	database.PlatformModel
	Slug      string `gorm:"size:64;not null;uniqueIndex:uk_plg_website_page_slug" json:"slug"`
	Title     string `gorm:"size:200;not null" json:"title"`
	SeoDesc   string `gorm:"size:300;not null" json:"seoDesc"`
	Content   string `gorm:"type:mediumtext" json:"content"`
	Published bool   `json:"published"`
	I18nJSON  string `gorm:"type:text" json:"i18nJson"`
}

func (Page) TableName() string { return "plg_website_page" }

// Post 是新闻 / 动态，路径 /news/:slug，内容为富文本 HTML（落库前已 Sanitize）。
type Post struct {
	database.PlatformModel
	Slug        string     `gorm:"size:64;not null;uniqueIndex:uk_plg_website_post_slug" json:"slug"`
	Title       string     `gorm:"size:200;not null" json:"title"`
	CoverURL    string     `gorm:"size:255;not null" json:"coverUrl"`
	Summary     string     `gorm:"size:500;not null" json:"summary"`
	Content     string     `gorm:"type:mediumtext" json:"content"`
	Published   bool       `json:"published"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	I18nJSON    string     `gorm:"type:text" json:"i18nJson"`
}

func (Post) TableName() string { return "plg_website_post" }

// Media 是官网自管的小型媒体文件（logo / 封面），平台级、按 id 不可变。
// 官网代表整个部署主体，不能放租户隔离的文件模块，所以插件自己存（只存小图）。
// 只按主键取单个，不存在列表场景，blob 字段不会批量加载。
type Media struct {
	database.PlatformModel
	Mime string `gorm:"size:64;not null" json:"mime"`
	Size int    `json:"size"`
	Data []byte `json:"-"`
}

func (Media) TableName() string { return "plg_website_media" }

// MarketStat 是官网插件市场的公开统计（下载次数与评分汇总），按插件 id 一行。
type MarketStat struct {
	database.PlatformModel
	PluginID    string `gorm:"size:48;not null;uniqueIndex:uk_plg_website_market_stat" json:"pluginId"`
	Downloads   int64  `gorm:"not null;default:0" json:"downloads"`
	RatingSum   int64  `gorm:"not null;default:0" json:"ratingSum"`
	RatingCount int64  `gorm:"not null;default:0" json:"ratingCount"`
}

func (MarketStat) TableName() string { return "plg_website_market_stat" }

// MarketRating 是匿名评分：voter 是 sha256(pluginID|IP)，同一 IP 对同一插件只有一行。
type MarketRating struct {
	database.PlatformModel
	PluginID string `gorm:"size:48;not null;uniqueIndex:uk_plg_website_market_rating" json:"pluginId"`
	Voter    string `gorm:"size:64;not null;uniqueIndex:uk_plg_website_market_rating" json:"-"`
	Score    int    `gorm:"not null" json:"score"`
}

func (MarketRating) TableName() string { return "plg_website_market_rating" }
