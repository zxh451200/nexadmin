package website

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
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
	// MessageNotifyEmail 是新留言是否抄送一封到站点联系邮箱（ContactEmail）。
	// 站内信始终发（零配置），这一项只管邮件；SMTP 未配置时发送自然跳过并记日志。
	MessageNotifyEmail bool `gorm:"column:message_notify_email;not null;default:true" json:"messageNotifyEmail"`
	// ContactConsent 打开后，官网留言必须先勾选「已阅读并同意隐私政策」。
	// 默认关：升级不改存量站点的公开表单；交付客户时在站点设置里打开。
	ContactConsent bool `gorm:"column:contact_consent;not null;default:false" json:"contactConsent"`
	// ContactConsentURL 是被同意的政策地址：站内 /p/xxx 或完整 https:// 地址，开启时必填。
	ContactConsentURL string `gorm:"column:contact_consent_url;size:255;not null" json:"contactConsentUrl"`
	// I18nJSON 是非默认语言的字段译文：{"en-US":{"homeTitle":"…"}}；空 = 全部回退原文。
	I18nJSON string `gorm:"type:text" json:"i18nJson"`
	// FaviconURL 是浏览器标签图标；OgImageURL 是站点默认分享图（og:image 兜底）。
	// 空分别回退模板内置 favicon.svg 与 og.png。
	FaviconURL string `gorm:"column:favicon_url;size:255;not null;default:''" json:"faviconUrl"`
	OgImageURL string `gorm:"column:og_image_url;size:255;not null;default:''" json:"ogImageUrl"`
	// HeadCode 是自定义 <head> 代码（统计脚本、站点验证等）：原样注入 <head>，仅 website:setting
	// 可写，属可信管理员配置；服务层限长，注入经 template.HTML 显式标记。
	HeadCode string `gorm:"column:head_code;type:text" json:"headCode"`
	// TitleSuffix 拼在 <title> 的「标题」之后，替换默认的「 - 站点名」；空 = 沿用内置组合。
	TitleSuffix string `gorm:"column:title_suffix;size:100;not null;default:''" json:"titleSuffix"`
	// HomeSlug 是用作首页的自定义单页 slug：非空时首页渲染该页内容（替换内置 Hero 页）；空 = 内置首页。
	// 页面本身仍可通过 /p/:slug 访问，不冲突。
	HomeSlug string `gorm:"column:home_slug;size:64;not null;default:''" json:"homeSlug"`
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

// 留言处理状态：线性流转 new → read → handled。
// 用状态而不是「读没读」一个布尔：读过了但还没回复是官网询盘的常态，
// 只记已读的话，收件箱看不出还剩几条要跟进。
const (
	MsgNew     = "new"     // 未读
	MsgRead    = "read"    // 已读，待跟进
	MsgHandled = "handled" // 已处理
)

// messageStatuses 是合法状态集；列表筛选与写入都按它校验。
var messageStatuses = map[string]bool{MsgNew: true, MsgRead: true, MsgHandled: true}

// ValidMessageStatus 报告状态名是否合法（管理端入参校验用）。
func ValidMessageStatus(s string) bool { return messageStatuses[s] }

// Message 是官网「在线留言」：匿名投稿，平台级保存，后台只读不改正文。
type Message struct {
	database.PlatformModel
	Name    string `gorm:"size:64;not null" json:"name"`
	Email   string `gorm:"size:128;not null" json:"email"`
	Phone   string `gorm:"size:32;not null" json:"phone"`
	Content string `gorm:"size:2000;not null" json:"content"`
	IP      string `gorm:"size:64;not null" json:"ip"`
	UA      string `gorm:"size:255;not null" json:"ua"`
	// Status 取 MsgNew / MsgRead / MsgHandled 之一，投稿落库时恒为 MsgNew。
	Status string `gorm:"size:16;not null;default:'new'" json:"status"`
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
	SeoTitle  string `gorm:"size:200;not null;default:''" json:"seoTitle"`
	SeoDesc   string `gorm:"size:300;not null" json:"seoDesc"`
	Content   string `gorm:"type:mediumtext" json:"content"`
	Published bool   `json:"published"`
	I18nJSON  string `gorm:"type:text" json:"i18nJson"`
	// PublishedJSON 是非默认语言的发布态：{"en-US":true,"ja-JP":false}；
	// 空 = 该行的 published 对所有语言生效（升级前保存的内容不会因新规则突然下线）。
	PublishedJSON string `gorm:"type:text" json:"publishedJson"`
	// Sort 是管理端列表排序：越小越靠前；0 = 未设置，排在已设置的后面。
	Sort int `gorm:"not null;default:0" json:"sort"`
	// SeoTitle 是独立的 meta 标题：留空回退「标题 - 站点名」；非默认语言存 i18nJson。
	// OgImage 是社交分享图（og:image / twitter:image），空 = 站点默认分享图。
	OgImage string `gorm:"size:255;not null;default:''" json:"ogImage"`
	// Noindex 关闭搜索引擎收录：<meta robots> 与 sitemap 同步生效，站内列表照常展示。
	Noindex bool `gorm:"not null;default:false" json:"noindex"`
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
	// SeoKeywords 是新闻级 SEO 关键词：详情页非空时优先于站点设置生成 meta keywords；只存默认语言。
	SeoKeywords string `gorm:"size:300;not null;default:''" json:"seoKeywords"`
	// SeoTitle 是独立的 meta 标题：留空回退「标题 - 站点名」；非默认语言存 i18nJson。
	SeoTitle string `gorm:"size:200;not null;default:''" json:"seoTitle"`
	// Noindex 关闭搜索引擎收录：<meta robots> 与 sitemap 同步生效，站内列表照常展示。
	Noindex bool `gorm:"not null;default:false" json:"noindex"`
	// Categories 是新闻所属分类；关联表 plg_website_post_category，保存时整体替换。
	Categories []Category `gorm:"many2many:plg_website_post_category" json:"categories,omitempty"`
	// Tags 是新闻标签；关联表 plg_website_post_tag，保存时整体替换。
	Tags []Tag `gorm:"many2many:plg_website_post_tag" json:"tags,omitempty"`
	// Views 是公开详情的浏览量：公开详情命中自增，只增不清（要清零走 SQL）。
	Views int64 `gorm:"not null;default:0" json:"views"`
	// Pinned 置顶：公开列表（含首页动态）置顶优先于发布时间排序。
	Pinned bool `gorm:"not null;default:false" json:"pinned"`
	// Author 是展示名（如「市场部」），不关联后台账号。
	Author string `gorm:"size:64;not null;default:''" json:"author"`
}

func (Post) TableName() string { return "plg_website_post" }

// PostVersion 是新闻的历史版本快照（表 plg_website_post_version）：当前内容始终在
// plg_website_post 上；每次覆盖保存前把旧行快照落到这里（正文没变不重复存），
// 改坏了可回看、可回滚。正文与译文不外泄（json:"-"）：列表接口按 PostVersionView
// 只回元数据，整篇 HTML 只在单版本详情接口按需取，不进列表响应。
type PostVersion struct {
	database.PlatformModel
	PostID      idgen.ID   `gorm:"column:post_id;not null;index:idx_plg_website_post_version,priority:1" json:"postId"`
	Version     int        `gorm:"column:version;not null;index:idx_plg_website_post_version,priority:2" json:"version"`
	Title       string     `gorm:"size:200;not null;default:''" json:"title"`
	Summary     string     `gorm:"size:500;not null;default:''" json:"summary"`
	CoverURL    string     `gorm:"column:cover_url;size:255;not null;default:''" json:"coverUrl"`
	Content     string     `gorm:"column:content;type:mediumtext" json:"-"`
	I18nJSON    string     `gorm:"column:i18n_json;type:text" json:"-"`
	SeoTitle    string     `gorm:"column:seo_title;size:200;not null;default:''" json:"seoTitle"`
	SeoDesc     string     `gorm:"column:seo_desc;size:300;not null;default:''" json:"seoDesc"`
	Noindex     bool       `gorm:"column:noindex;not null;default:false" json:"noindex"`
	Published   bool       `gorm:"column:published;not null;default:false" json:"published"`
	PublishedAt *time.Time `gorm:"column:published_at" json:"publishedAt,omitempty"`
	// CreatedBy 是触发这次归档的编辑展示名（用户名；取不到登录态时为空，如种子与后台任务）。
	CreatedBy string `gorm:"column:created_by;size:64;not null;default:''" json:"createdBy"`
}

func (PostVersion) TableName() string { return "plg_website_post_version" }

// Category 是新闻分类，即编辑部的栏目：后台单独维护、集合有限、公开侧按栏目导航。
// 标题只存默认语言：栏目是组织维度，跨语言展示时直接落原文即可。
type Category struct {
	database.PlatformModel
	Title string `gorm:"size:100;not null" json:"title"`
	Slug  string `gorm:"size:64;not null;uniqueIndex:uk_plg_website_category_slug" json:"slug"`
	Sort  int    `json:"sort"`
	// PostCount 是栏目下的新闻数：保存时回填，不落库。界面用它排栏目，
	// 也让人在删栏目之前先看见「这里面有 12 篇」。
	PostCount int64 `gorm:"-" json:"postCount"`
}

func (Category) TableName() string { return "plg_website_category" }

// Tag 是新闻标签：与栏目分层的第二维度，编辑器里打字即建，主题词自由生长。
// Name 是展示与去重的依据（中文标签很常见），Slug 只负责出现在 URL 里。
type Tag struct {
	database.PlatformModel
	Name string `gorm:"size:64;not null" json:"name"`
	Slug string `gorm:"size:64;not null;uniqueIndex:uk_plg_website_tag_slug" json:"slug"`
	// PostCount 语义同 Category.PostCount。
	PostCount int64 `gorm:"-" json:"postCount"`
}

func (Tag) TableName() string { return "plg_website_tag" }

// ---- 产品中心 ----

// 产品状态：对齐阿里国际站的商品状态机。本地一期只用 draft / online / offline；
// pending / rejected 留给二期同步——商品提交到阿里审核后回写「审核中 / 审核不通过」。
const (
	ProductDraft    = "draft"    // 草稿
	ProductOnline   = "online"   // 上架：公开站可见
	ProductOffline  = "offline"  // 下架：曾上架后撤下，与草稿区分（对齐阿里的页签）
	ProductPending  = "pending"  // 审核中（预留：同步到阿里后回写）
	ProductRejected = "rejected" // 审核不通过（预留）
)

// productStatuses 是合法状态集；保存与批量操作都按它校验。
var productStatuses = map[string]bool{
	ProductDraft: true, ProductOnline: true, ProductOffline: true,
	ProductPending: true, ProductRejected: true,
}

// 产品同步状态：一期只落库展示，二期接阿里开放平台 API 后由同步任务写入。
const (
	ProductSyncNone    = "none"    // 从未同步
	ProductSyncPending = "pending" // 已提交同步，待确认
	ProductSyncSynced  = "synced"  // 已发布到阿里
	ProductSyncFailed  = "failed"  // 上次同步失败
)

// ProductCategory 是产品类目：对标阿里的「商品分组」，但支持最多 3 级树形层级。
// 标题只存默认语言：与新闻分类一样，类目是组织维度，跨语言展示时直接落原文。
type ProductCategory struct {
	database.PlatformModel
	// ParentID 是上级类目 ID，0 = 顶级；JSON 里按字符串传输（雪花 ID 超出 JS 安全整数）。
	ParentID NavParentID `gorm:"index:idx_plg_website_prod_cat_parent;not null;default:0" json:"parentId"`
	Title    string      `gorm:"size:100;not null" json:"title"`
	Slug     string      `gorm:"size:64;not null;uniqueIndex:uk_plg_website_product_category_slug" json:"slug"`
	Sort     int         `json:"sort"`
	// ProductCount 是该类目（不含子类目）下的商品数：保存时回填，不落库。
	ProductCount int64 `gorm:"-" json:"productCount"`
	// Children 只在树装配时填充，不入库。
	Children []ProductCategory `gorm:"-" json:"children,omitempty"`
}

func (ProductCategory) TableName() string { return "plg_website_product_category" }

// Product 是产品中心的一条商品：字段对标阿里发品表单（名称/关键词/图集/价格区间/
// 起订量/发货期/港口/动态属性/详情富文本），多语言与发布机制与新闻同口径。
// AlibabaProductID / SyncStatus / SyncedAt 是二期「一键同步到阿里」的预留位，
// 一期只落库与展示，不发起任何对阿里的请求。
type Product struct {
	database.PlatformModel
	Slug     string `gorm:"size:64;not null;uniqueIndex:uk_plg_website_product_slug" json:"slug"`
	Title    string `gorm:"size:200;not null" json:"title"`
	// Keywords 是搜索关键词（对标阿里的 1-3 个关键词）：保存时按空白/逗号拆分、去重、取前 3 个。
	Keywords string `gorm:"size:300;not null;default:''" json:"keywords"`
	Summary  string `gorm:"size:500;not null;default:''" json:"summary"`
	Content  string `gorm:"type:mediumtext" json:"content"`
	// SeoTitle 是独立的 meta 标题：留空回退「标题 - 站点名」；非默认语言存 i18nJson。
	SeoTitle    string `gorm:"size:200;not null;default:''" json:"seoTitle"`
	SeoDesc     string `gorm:"size:300;not null;default:''" json:"seoDesc"`
	SeoKeywords string `gorm:"size:300;not null;default:''" json:"seoKeywords"`
	// Noindex 关闭搜索引擎收录：<meta robots> 与 sitemap 同步生效，站内列表照常展示。
	Noindex bool `gorm:"not null;default:false" json:"noindex"`
	I18nJSON      string `gorm:"type:text" json:"i18nJson"`
	// PublishedJSON 是非默认语言的发布态，语义同 Page；默认语言沿用 status 列（online 即发布）。
	PublishedJSON string `gorm:"type:text" json:"publishedJson"`
	CoverURL string `gorm:"size:255;not null;default:''" json:"coverUrl"`
	// ImagesJSON 是图集 URL 数组（[]string 的 JSON 文本）：走 text 列三库行为一致，前端自己解析。
	ImagesJSON string `gorm:"column:images;type:text" json:"imagesJson"`
	VideoURL   string `gorm:"size:255;not null;default:''" json:"videoUrl"`
	// 贸易字段：对标阿里发品的「价格与物流」。价格 0 = 未定价（只展示询盘）。
	PriceMin   float64 `gorm:"not null;default:0" json:"priceMin"`
	PriceMax   float64 `gorm:"not null;default:0" json:"priceMax"`
	Currency   string  `gorm:"size:8;not null;default:'USD'" json:"currency"`
	PriceUnit  string  `gorm:"size:32;not null;default:''" json:"priceUnit"` // 计量单位：piece / set…
	Moq        int     `gorm:"column:moq;not null;default:0" json:"moq"`     // 最小起订量
	LeadTimeDays int   `gorm:"column:lead_time_days;not null;default:0" json:"leadTimeDays"`
	Port       string  `gorm:"size:64;not null;default:''" json:"port"`
	// PaymentTerms 是付款方式（对标阿里商详「交易信息」的 Payment Terms）：如 "T/T, L/C"。
	PaymentTerms string `gorm:"column:payment_terms;size:100;not null;default:''" json:"paymentTerms"`
	// SpecsJSON 是动态属性键值对（[{"label":"Brand Name","value":"…"}] 的 JSON 文本）：
	// 阿里的类目属性各行业不同，本地不建属性字典，存成有序键值对最贴近同步语义。
	SpecsJSON string `gorm:"column:specs;type:text" json:"specsJson"`
	// Status 取 ProductDraft / ProductOnline / … 之一；online 即「上架」，公开站唯一可见状态。
	Status string `gorm:"size:16;not null;default:'draft';index" json:"status"`
	// 阿里同步预留：阿里侧商品 ID（回写后本地据此更新）、同步状态与最近同步时间。
	AlibabaProductID string     `gorm:"column:alibaba_product_id;size:64;not null;default:'';index" json:"alibabaProductId"`
	SyncStatus       string     `gorm:"column:sync_status;size:16;not null;default:'none'" json:"syncStatus"`
	SyncedAt         *time.Time `gorm:"column:synced_at" json:"syncedAt,omitempty"`
	// AlibabaCategoryID 是商品绑定的阿里叶子类目 ID（同步对话框选完回填，二期同步用）。
	AlibabaCategoryID string `gorm:"column:alibaba_category_id;size:32;not null;default:''" json:"alibabaCategoryId"`
	// SyncError 是最近一次同步失败的原因（code+文案，截 500）：列表悬停展示，成功同步后清空。
	SyncError string `gorm:"column:sync_error;size:500;not null;default:''" json:"syncError"`
	// Categories 是商品所属类目；关联表 plg_website_product_category_rel，保存时整体替换。
	Categories []ProductCategory `gorm:"many2many:plg_website_product_category_rel" json:"categories,omitempty"`
	// Views 是公开详情的浏览量：公开详情命中自增，只增不清（要清零走 SQL）。
	Views int64 `gorm:"not null;default:0" json:"views"`
	// Author 是展示名（如「外贸部」），不关联后台账号。
	Author string `gorm:"size:64;not null;default:''" json:"author"`
}

func (Product) TableName() string { return "plg_website_product" }

// AliConfig 是阿里国际站开放平台的接入凭据（单行表，二期同步用）。
// AppSecret 与 SessionKey 加密落库（core/secret，AES-256-GCM，密文列 json:"-" 不出接口）；
// 接口视图只回「已配置」布尔，参照 plugins/realname 的 ConfigView 模式。
type AliConfig struct {
	database.PlatformModel
	AppKey string `gorm:"size:64;not null;default:''" json:"appKey"`
	// AppSecretEnc / SessionKeyEnc 是 core/secret 密文（enc:v1:…）；明文永不落库、不出接口。
	AppSecretEnc string `gorm:"column:app_secret_enc;type:text" json:"-"`
	SessionKeyEnc string `gorm:"column:session_key_enc;type:text" json:"-"`
	// ApiGateway 是 TOP 网关地址：默认正式环境，留沙箱切换口。
	ApiGateway string `gorm:"column:api_gateway;size:100;not null;default:'https://gw.api.taobao.com/router/rest'" json:"apiGateway"`
}

func (AliConfig) TableName() string { return "plg_website_ali_config" }

// Published 报告商品的默认语言是否处于上架态：多语言发布机制（published_json 的
// NormalizePublish / Published 判定）需要一个「整篇是否发布」的布尔，产品由状态机推导。
func (p Product) Published() bool { return p.Status == ProductOnline }

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
