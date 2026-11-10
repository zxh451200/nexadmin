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
	// MessageNotifyEmail 同样是指针：不传保持原值（设置页分区块保存时不误改）。
	MessageNotifyEmail *bool `json:"messageNotifyEmail"`
	// ContactConsent / ContactConsentURL 是留言的隐私同意勾选：默认关，开启时地址必填。
	ContactConsent    *bool  `json:"contactConsent"`
	ContactConsentURL string `json:"contactConsentUrl"`
	// I18nJSON 是非默认语言译文；服务层按白名单语言/字段清洗后入库。
	I18nJSON string `json:"i18nJson"`
	// FaviconURL / OgImageURL 是站点资产：浏览器标签图标与默认分享图（og:image 兜底），空 = 模板内置。
	FaviconURL string `json:"faviconUrl"`
	OgImageURL string `json:"ogImageUrl"`
	// HeadCode 是自定义 <head> 代码（统计脚本等），原样注入；限长见服务层。
	HeadCode string `json:"headCode"`
	// TitleSuffix 拼在 <title> 的「标题」之后；空 = 沿用「 - 站点名」。
	TitleSuffix string `json:"titleSuffix"`
	// HomeSlug 是用作首页的自定义单页 slug；空 = 内置首页。服务层校验页面存在。
	HomeSlug string `json:"homeSlug"`
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
	// Consent 是「已阅读并同意隐私政策」勾选：站点开启该要求时必须为 true。
	Consent bool `json:"consent" form:"consent"`
}

// MessageQuery 是后台留言列表的筛选。
type MessageQuery struct {
	Page     int
	PageSize int
	Keyword  string
	// Status 为空 = 全部状态；非空时必须是 MsgNew / MsgRead / MsgHandled 之一。
	Status string
}

// MessageStatusForm 是批量改状态：一次把 ids 里的留言置为 status。
type MessageStatusForm struct {
	IDs    []idgen.ID `json:"ids"`
	Status string     `json:"status"`
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
	SeoTitle  string `json:"seoTitle"`
	SeoDesc   string `json:"seoDesc"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
	I18nJSON  string `json:"i18nJson"`
	// PublishedJSON 是非默认语言发布态；服务层会剔除缺译语言的 true。
	PublishedJSON string `json:"publishedJson"`
	// OgImage 是社交分享图；Noindex 关闭收录（<meta robots> 与 sitemap 同步）。
	OgImage string `json:"ogImage"`
	NoIndex bool   `json:"noindex"`
	// Sort 是管理端列表排序：越小越靠前；0 = 未设置。
	Sort int `json:"sort"`
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
	// PublishedAt 只在发布时生效：给了就按它记（回填历史文章、修正误发、定时发布），
	// 不给则沿用「首次发布写当前时间」的默认行为。设为未来时间即定时发布：
	// 到点之前公开端按未上线处理。
	PublishedAt *time.Time `json:"publishedAt"`
	// SeoKeywords 是新闻级 SEO 关键词，非空时详情页优先于站点设置生成 meta keywords。
	SeoKeywords string `json:"seoKeywords"`
	// CategoryIDs 是所属分类的完整列表，保存时整体替换；空 = 清空分类。
	CategoryIDs []idgen.ID `json:"categoryIds"`
	// Tags 是标签名的完整列表，保存时整体替换；不存在的名字当场建标签。
	// 用名字而不是 id：标签是打字即建的自由词，让编辑器先建后勾只是把成本推给界面。
	Tags     []string `json:"tags"`
	I18nJSON string   `json:"i18nJson"`
	// PublishedJSON 是非默认语言发布态；服务层会剔除缺译语言的 true。
	PublishedJSON string `json:"publishedJson"`
	// SeoTitle 是独立的 meta 标题；Noindex 关闭收录（<meta robots> 与 sitemap 同步）。
	SeoTitle string `json:"seoTitle"`
	NoIndex  bool   `json:"noindex"`
	// Author 是展示名（不关联账号）；Pinned 置顶：公开列表置顶优先于发布时间。
	Author string `json:"author"`
	Pinned bool   `json:"pinned"`
}

// CategoryForm 是新闻分类表单。
type CategoryForm struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
	Sort  int    `json:"sort"`
}

// TagForm 是标签管理表单：Slug 留空由 Name 派生（改名不换 URL 标识，避免把已分享的链接打断）。
type TagForm struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// PostStatusForm 是新闻批量发布/下线：一次把 ids 里的新闻置为 status。
type PostStatusForm struct {
	IDs    []idgen.ID `json:"ids"`
	Status string     `json:"status"`
}

// SpecItem 是商品的一条动态属性（对标阿里的类目属性）：各行业属性不同，
// 本地不建属性字典，存成有序键值对最贴近同步语义。
type SpecItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ProductForm 是产品表单；Content 为富文本 HTML，落库前 Sanitize。
type ProductForm struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Keywords    string `json:"keywords"`
	Summary     string `json:"summary"`
	SeoTitle    string `json:"seoTitle"`
	SeoDesc     string `json:"seoDesc"`
	SeoKeywords string `json:"seoKeywords"`
	Content     string `json:"content"`
	// Status 只接受 draft / online（上架）；offline 及审核态由列表动作与同步回写维护。
	Status string `json:"status"`
	// CoverURL 是主图；Images 是图集 URL 列表（保存时整体替换，上限 10 张）。
	CoverURL string   `json:"coverUrl"`
	Images   []string `json:"images"`
	VideoURL string   `json:"videoUrl"`
	// 贸易字段：负数一律按 0 处理；PriceMax 小于 PriceMin 时取 PriceMin。
	PriceMin     float64 `json:"priceMin"`
	PriceMax     float64 `json:"priceMax"`
	Currency     string  `json:"currency"`
	PriceUnit    string  `json:"priceUnit"`
	Moq          int     `json:"moq"`
	LeadTimeDays int     `json:"leadTimeDays"`
	Port         string  `json:"port"`
	// PaymentTerms 是付款方式：如 "T/T, L/C"，详情页「交易信息」块展示。
	PaymentTerms string  `json:"paymentTerms"`
	// Specs 是动态属性键值对（上限 50 条）；CategoryIDs 是所属类目的完整列表，保存时整体替换。
	Specs       []SpecItem `json:"specs"`
	CategoryIDs []idgen.ID `json:"categoryIds"`
	I18nJSON    string     `json:"i18nJson"`
	// PublishedJSON 是非默认语言发布态；服务层会剔除缺译语言的 true。
	PublishedJSON string `json:"publishedJson"`
	NoIndex       bool   `json:"noindex"`
	Author        string `json:"author"`
}

// ProductCategoryForm 是产品类目表单。
type ProductCategoryForm struct {
	// ParentID 为上级类目 ID，0 = 顶级；最多 3 级（与导航同一套层级校验）。
	ParentID NavParentID `json:"parentId"`
	Title    string      `json:"title"`
	Slug     string      `json:"slug"`
	Sort     int         `json:"sort"`
}

// ProductCategoryOrderItem 是一条类目拖拽排序指令：把 ID 挂到 ParentID 下并写入 Sort。
type ProductCategoryOrderItem struct {
	ID       idgen.ID    `json:"id"`
	ParentID NavParentID `json:"parentId"`
	Sort     int         `json:"sort"`
}

// ProductCategoryOrderForm 是一次批量排序请求。
type ProductCategoryOrderForm struct {
	Items []ProductCategoryOrderItem `json:"items"`
}
