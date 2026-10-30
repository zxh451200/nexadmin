package website

import (
	"time"

	"nexadmin/pkg/idgen"
)

// 管理端表单（JSON 绑定）。

// SettingForm 是站点设置表单。
type SettingForm struct {
	SiteName     string `json:"siteName"`
	SiteDesc     string `json:"siteDesc"`
	LogoURL      string `json:"logoUrl"`
	HomeTitle    string `json:"homeTitle"`
	HomeSubtitle string `json:"homeSubtitle"`
	HomeCTALabel string `json:"homeCtaLabel"`
	HomeCTAHref  string `json:"homeCtaHref"`
	SeoKeywords  string `json:"seoKeywords"`
	ContactEmail string `json:"contactEmail"`
	ContactPhone string `json:"contactPhone"`
	QRCodeURL    string `json:"qrCodeUrl"`
	// QRCodeCaption 是二维码下方的一行说明（如「微信公众号」）。
	QRCodeCaption string `json:"qrCodeCaption"`
	// Socials / FriendLinks 是页脚列表：写库前由服务层做白名单与长度校验。
	Socials     []SocialLink `json:"socials"`
	FriendLinks []FriendLink `json:"friendLinks"`
	ICP         string       `json:"icp"`
	FooterNote  string       `json:"footerNote"`
	// 指针：旧客户端不传该字段时保持原值，避免误关市场。
	MarketEnabled *bool `json:"marketEnabled"`
	// I18nJSON 是非默认语言译文；服务层按白名单语言/字段清洗后入库。
	I18nJSON string `json:"i18nJson"`
}

// ItemForm 是导航项表单。
type ItemForm struct {
	// ParentID 为上级导航 ID，0 = 顶级。
	ParentID NavParentID `json:"parentId"`
	Label    string      `json:"label"`
	Href     string      `json:"href"`
	Sort     int         `json:"sort"`
	Enabled  bool        `json:"enabled"`
	I18nJSON string      `json:"i18nJson"`
}

// NavOrderItem 是一条拖拽排序指令：把 ID 挂到 ParentID 下并写入 Sort。
type NavOrderItem struct {
	ID       idgen.ID    `json:"id"`
	ParentID NavParentID `json:"parentId"`
	Sort     int         `json:"sort"`
}

// NavOrderForm 是一次批量排序请求。
type NavOrderForm struct {
	Items []NavOrderItem `json:"items"`
}

// ContactForm 是公开站「在线留言」的投稿表单（JSON 与表单编码两种提交都绑到它）。
type ContactForm struct {
	Name    string `json:"name" form:"name"`
	Email   string `json:"email" form:"email"`
	Phone   string `json:"phone" form:"phone"`
	Content string `json:"content" form:"content"`
	// Company 是蜜罐：真人看不到这个字段，填了就是机器人（静默丢弃，不告诉对方）。
	Company string `json:"company" form:"company"`
}

// MessageQuery 是后台留言列表的筛选。
type MessageQuery struct {
	Page     int
	PageSize int
	Keyword  string
	Unread   bool
}

// FeatureForm 是特性卡片表单。
type FeatureForm struct {
	Icon        string `json:"icon"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Sort        int    `json:"sort"`
	Enabled     bool   `json:"enabled"`
	I18nJSON    string `json:"i18nJson"`
}

// PageForm 是自定义页表单；Content 为富文本 HTML，落库前 Sanitize。
type PageForm struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	SeoDesc   string `json:"seoDesc"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
	I18nJSON  string `json:"i18nJson"`
	// PublishedJSON 是非默认语言发布态；服务层会剔除缺译语言的 true。
	PublishedJSON string `json:"publishedJson"`
}

// PostForm 是新闻表单；Content 为富文本 HTML，落库前 Sanitize。
type PostForm struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	CoverURL  string `json:"coverUrl"`
	Summary   string `json:"summary"`
	SeoDesc   string `json:"seoDesc"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
	// PublishedAt 只在发布时生效：给了就按它记（回填历史文章、修正误发），
	// 不给则沿用「首次发布写当前时间」的默认行为。
	PublishedAt *time.Time `json:"publishedAt"`
	I18nJSON    string     `json:"i18nJson"`
	// PublishedJSON 是非默认语言发布态；服务层会剔除缺译语言的 true。
	PublishedJSON string `json:"publishedJson"`
}
