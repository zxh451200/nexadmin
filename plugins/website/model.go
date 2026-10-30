package website

import (
	"fmt"
	"strconv"
	"strings"
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
	ContactPhone  string `gorm:"size:64;not null" json:"contactPhone"`   // 页脚与联系页的电话，空 = 不显示
	QRCodeURL     string `gorm:"size:255;not null" json:"qrCodeUrl"`     // 页脚二维码（公众号/客服），空 = 不显示
	QRCodeCaption string `gorm:"size:128;not null" json:"qrCodeCaption"` // 二维码下的说明文字
	ICP           string `gorm:"size:128;not null" json:"icp"`           // 备案号，空 = 页脚不显示
	FooterNote    string `gorm:"size:300;not null" json:"footerNote"`
	// 社交图标与友情链接是页脚列表：JSON 数组文本（[]SocialLink / []FriendLink）。
	// 走文本列而不是 JSON 列，三库行为一致；对外由 settingView 展开成数组。
	SocialsJSON     string `gorm:"column:socials;type:text" json:"-"`
	FriendLinksJSON string `gorm:"column:friend_links;type:text" json:"-"`
	MarketEnabled   bool   `gorm:"not null;default:true" json:"marketEnabled"` // 官网插件市场入口开关
	// I18nJSON 是非默认语言的字段译文：{"en-US":{"homeTitle":"…"}}；空 = 全部回退原文。
	I18nJSON string `gorm:"type:text" json:"i18nJson"`
	// SeedVersion 记录出厂内容补种进度：升级后只补一次新增的出厂条目，不复活管理员删掉的内容。
	SeedVersion int `gorm:"not null;default:1" json:"-"`
}

func (Setting) TableName() string { return "plg_website_setting" }

// SocialLink 是页脚的社交图标（Platform 取自 render.go 的 socialIconPaths 键）与外部主页地址。
type SocialLink struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
}

// FriendLink 是页脚的友情链接。
type FriendLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Message 是官网「在线留言」：匿名投稿，平台级保存，后台只读不改正文。
type Message struct {
	database.PlatformModel
	Name    string `gorm:"size:64;not null" json:"name"`
	Email   string `gorm:"size:128;not null" json:"email"`
	Phone   string `gorm:"size:32;not null" json:"phone"`
	Content string `gorm:"size:2000;not null" json:"content"`
	IP      string `gorm:"size:64;not null" json:"ip"`
	UA      string `gorm:"size:255;not null" json:"ua"`
	// read 是 MySQL 保留字，列名写成 is_read，避免手写 SQL 到处加引号。
	Read bool `gorm:"column:is_read;not null;default:false" json:"read"`
}

func (Message) TableName() string { return "plg_website_message" }

// Nav 是顶部导航项，ParentID 支持最多 3 级。
type Nav struct {
	database.PlatformModel
	// ParentID 是上级导航 ID，0 = 顶级；JSON 里按字符串传输（雪花 ID 超出 JS 安全整数）。
	ParentID NavParentID `gorm:"index:idx_plg_website_nav_parent;not null;default:0" json:"parentId"`
	Label    string      `gorm:"size:64;not null" json:"label"`
	Href     string      `gorm:"size:255;not null" json:"href"`
	Sort     int         `json:"sort"`
	Enabled  bool        `json:"enabled"`
	I18nJSON string      `gorm:"type:text" json:"i18nJson"`
	// Children 只在公开页装配树时填充，不入库。
	Children []Nav `gorm:"-" json:"children,omitempty"`
}

// NavParentID 是导航上级引用：落库仍是 uint64，JSON 用字符串避免大 ID 在浏览器丢精度；
// 输入兼容 "0" / 0 / null。
type NavParentID uint64

func (p NavParentID) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(uint64(p), 10) + `"`), nil
}

func (p *NavParentID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*p = 0
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("导航上级 ID 非法：%s", s)
	}
	*p = NavParentID(v)
	return nil
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
	// PublishedJSON 是非默认语言的发布态：{"en-US":true,"ja-JP":false}；
	// 空 = 该行的 published 对所有语言生效（升级前保存的内容不会因新规则突然下线）。
	PublishedJSON string `gorm:"type:text" json:"publishedJson"`
}

func (Page) TableName() string { return "plg_website_page" }

// Post 是新闻 / 动态，路径 /news/:slug，内容为富文本 HTML（落库前已 Sanitize）。
type Post struct {
	database.PlatformModel
	Slug        string     `gorm:"size:64;not null;uniqueIndex:uk_plg_website_post_slug" json:"slug"`
	Title       string     `gorm:"size:200;not null" json:"title"`
	CoverURL    string     `gorm:"size:255;not null" json:"coverUrl"`
	Summary     string     `gorm:"size:500;not null" json:"summary"`
	SeoDesc     string     `gorm:"size:300;not null" json:"seoDesc"`
	Content     string     `gorm:"type:mediumtext" json:"content"`
	Published   bool       `json:"published"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	I18nJSON    string     `gorm:"type:text" json:"i18nJson"`
	// PublishedJSON 是非默认语言的发布态，语义同 Page；默认语言沿用 published 列。
	PublishedJSON string `gorm:"type:text" json:"publishedJson"`
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
