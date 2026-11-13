package website

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/response"
	pi18n "nexadmin/pkg/i18n"
)

// 主题机制：website 自带 default 一套模板（tpl/、static/），主题插件在 init() 里
// 调 RegisterTheme 注册额外的模板包。站点设置选了哪个主题，公开站就用哪套模板与
// 静态资源渲染；未注册（主题插件被卸载）时回退 default，站点永远可访问。
//
// 注册发生在进程启动期（主题插件包 init → 空导入顺序保证晚于 website），受
// 「插件改动需重启」语义保护：模板集启动期构建一次，请求路径零解析开销。

// pageNames 是公开站模板全集：主题只需要覆盖自己关心的页面，缺失的页面回退
// default（/docs /plugins 等框架自带页通常不必换肤）。
var pageNames = []string{
	"docs", "docs-index", "home", "plugins", "plugin",
	"news", "post", "products", "product", "page", "contact", "notfound", "error",
}

var themeIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,47}$`)

// Theme 是一套注册进来的模板包。
type Theme struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Version string `json:"version"`
	// FS 根下是 tpl/*.tmpl 与 static/**；模板引用自身静态资源用
	// /website-assets/theme/<id>/<file>（assetV 做内容哈希缓存键）。
	FS fs.FS `json:"-"`
	// seed 可空：导入该主题的演示内容（见 DemoData），设置页按钮触发。
	seed func(ctx context.Context, s *Service, log *zap.Logger) (DemoReport, error)
}

var (
	themeMu  sync.Mutex
	themeIDs []string // 注册顺序，default 恒在最前
	themeReg = map[string]*Theme{}
)

// RegisterTheme 注册一套主题。fsys 根下必须有 tpl/layout.tmpl；缺页面模板的页面
// 渲染时回退 default。重复 ID 或模板解析失败直接报错（插件作者在开发期就该发现）。
func RegisterTheme(id, label, version string, fsys fs.FS, seed func(ctx context.Context, s *Service, log *zap.Logger) (DemoReport, error)) error {
	id = strings.TrimSpace(id)
	if !themeIDRe.MatchString(id) {
		return fmt.Errorf("theme id %q 非法（小写字母开头，字母/数字/连字符，2-48 位）", id)
	}
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("theme %s 缺少 label", id)
	}
	if fsys == nil {
		return fmt.Errorf("theme %s 缺少模板文件系统", id)
	}
	themeMu.Lock()
	defer themeMu.Unlock()
	if _, dup := themeReg[id]; dup {
		return fmt.Errorf("theme %s 重复注册", id)
	}
	t := &Theme{ID: id, Label: label, Version: version, FS: fsys, seed: seed}
	pack, err := buildThemePack(t)
	if err != nil {
		return err
	}
	themeReg[id] = t
	themePacks[id] = pack
	themeIDs = append(themeIDs, id)
	return nil
}

// Themes 返回已注册主题清单（default 在最前，其余按注册顺序）。
func Themes() []Theme {
	themeMu.Lock()
	defer themeMu.Unlock()
	out := make([]Theme, 0, len(themeIDs))
	for _, id := range themeIDs {
		out = append(out, *themeReg[id])
	}
	return out
}

func themeRegistered(id string) bool {
	if id == "" {
		return true // 空 = default
	}
	themeMu.Lock()
	defer themeMu.Unlock()
	_, ok := themeReg[id]
	return ok
}

// lookupTheme 取单个主题；不存在返回 nil（资产服务与缓存键据此回 404/空版本）。
func lookupTheme(id string) *Theme {
	themeMu.Lock()
	defer themeMu.Unlock()
	return themeReg[id]
}

// themeActive 报告站点是否启用了品牌主题（setting.theme 非空且已注册）。
// 激活时官网出厂营销内容（导航/特性/新闻/页面）不参与启动补种——内容归主题与管理员。
// 读不到设置时按未激活处理，维持出厂行为。
func themeActive(ctx context.Context, s *Service) bool {
	set, err := s.Settings(ctx)
	if err != nil {
		return false
	}
	return set.Theme != ""
}

// themePackFor 取主题的已编译模板集；主题不存在（被卸载/设置残留）回退 default。
func themePackFor(id string) map[string]*template.Template {
	if id == "" {
		return pageTmpl
	}
	themeMu.Lock()
	defer themeMu.Unlock()
	if pack := themePacks[id]; pack != nil {
		return pack.tmpl
	}
	return pageTmpl
}

// ---- 模板集构建（buildThemePack 在 render.go，紧挨 default 的构建逻辑） ----

// ---- 演示内容 seed ----

// DemoSettings 是主题演示数据里对站点设置的覆盖。逐字段应用，且只在
// 「当前值为空或仍等于出厂默认」时生效——用户已经填过的内容一概不碰。
type DemoSettings struct {
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
	ICP          string `json:"icp"`
	FooterNote   string `json:"footerNote"`
	TitleSuffix  string `json:"titleSuffix"`
	FaviconURL   string `json:"faviconUrl"`
	OgImageURL   string `json:"ogImageUrl"`
	// Socials 是页脚社交列表；仅在当前 socials 列表为空时整体写入。
	Socials []SocialLink `json:"socials"`
	// I18nJSON 是品牌文案的非默认语言译文（{"en-US":{...}}），按 settingI18nFields 清洗后入库。
	I18nJSON string `json:"i18nJson"`
}

// DemoData 是一套主题的演示内容。条目全部 insert-only：按 slug/href/title 判重，
// 已存在的跳过，重复导入不产生重复行，用户改过的内容一概不动。
type DemoData struct {
	Settings          *DemoSettings     `json:"settings"`
	Navs              []Nav             `json:"navs"`
	Features          []Feature         `json:"features"`
	Categories        []Category        `json:"categories"`
	Pages             []Page            `json:"pages"`
	Posts             []Post            `json:"posts"`
	ProductCategories []ProductCategory `json:"productCategories"`
	Products          []Product         `json:"products"`
}

// DemoReport 是导入结果的计数：告诉管理员哪些实体种了几条。
type DemoReport struct {
	Settings          int `json:"settings"`
	Navs              int `json:"navs"`
	Features          int `json:"features"`
	Categories        int `json:"categories"`
	Pages             int `json:"pages"`
	Posts             int `json:"posts"`
	ProductCategories int `json:"productCategories"`
	Products          int `json:"products"`
	Skipped           int `json:"skipped"`
}

// SeedDemo 把演示内容种进库：insert-only，可重复调用；整个导入在一个事务里，
// 任一步失败全部回滚，不会留下半套演示内容。
// 新闻/产品的类目引用（Categories[].Slug）在类目插入后按 slug 解析成关联；
// 未建过的类目引用静默丢弃（不阻断导入）。
// SeedDemo 把演示内容种进库：整个导入在一个事务里，任一步失败全部回滚。
// 语义（见各步注释）：品牌门面（设置/导航/特性）按主题整体落笔；内容条目 insert-only，
// 且带 demo_of 标记——导入主题 T 时先清掉其它主题的演示行，用户自建内容永远不动。
// 新闻/产品的类目引用（Categories[].Slug）在类目插入后按 slug 解析成关联；
// 未建过的类目引用静默丢弃（不阻断导入）。
func SeedDemo(ctx context.Context, s *Service, themeID string, data DemoData, log *zap.Logger) (rep DemoReport, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return seedDemoTx(ctx, tx, themeID, data, &rep)
	})
	if err != nil && log != nil {
		log.Warn("website: theme demo seed failed", zap.Error(err))
	}
	return rep, err
}

func seedDemoTx(ctx context.Context, tx *gorm.DB, themeID string, data DemoData, rep *DemoReport) error {
	// 0. 清掉全部演示行（含本主题自己的旧版本——重新导入就是刷新演示内容到最新，
	// 译文/文案的更新才能落库）；demo_of 为空的行是用户自建/出厂内容，永远不动。
	// 表名是包内常量，主题 id 走占位符。
	if err := tx.Exec("DELETE FROM plg_website_product_category WHERE demo_of <> ''").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM plg_website_category WHERE demo_of <> ''").Error; err != nil {
		return err
	}
	// 商品的类目关联先删，再删商品行（关联表没有 demo_of，靠商品表的标记圈定范围）。
	if err := tx.Exec("DELETE FROM plg_website_product_category_rel WHERE product_id IN (SELECT id FROM plg_website_product WHERE demo_of <> '')").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM plg_website_product WHERE demo_of <> ''").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM plg_website_post WHERE demo_of <> ''").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM plg_website_page WHERE demo_of <> ''").Error; err != nil {
		return err
	}

	// 1. 站点设置：演示导入是显式按钮操作，品牌字段（站点名/首页文案/联系方式等）
	// 按主题演示值整体落笔——切主题就是要换品牌门面；空值字段不写入。
	if data.Settings != nil {
		var row Setting
		if err := tx.Order("id").First(&row).Error; err != nil {
			return err
		}
		cur := row
		changed := 0
		apply := func(get func(*Setting) string, set func(*Setting, string), v string) {
			if v == "" || get(&cur) == v {
				return
			}
			set(&cur, v)
			changed++
		}
		apply(func(x *Setting) string { return x.SiteName }, func(x *Setting, v string) { x.SiteName = v }, data.Settings.SiteName)
		apply(func(x *Setting) string { return x.SiteDesc }, func(x *Setting, v string) { x.SiteDesc = v }, data.Settings.SiteDesc)
		apply(func(x *Setting) string { return x.LogoURL }, func(x *Setting, v string) { x.LogoURL = v }, data.Settings.LogoURL)
		apply(func(x *Setting) string { return x.HomeTitle }, func(x *Setting, v string) { x.HomeTitle = v }, data.Settings.HomeTitle)
		apply(func(x *Setting) string { return x.HomeSubtitle }, func(x *Setting, v string) { x.HomeSubtitle = v }, data.Settings.HomeSubtitle)
		apply(func(x *Setting) string { return x.HomeCTALabel }, func(x *Setting, v string) { x.HomeCTALabel = v }, data.Settings.HomeCTALabel)
		apply(func(x *Setting) string { return x.HomeCTAHref }, func(x *Setting, v string) { x.HomeCTAHref = v }, data.Settings.HomeCTAHref)
		apply(func(x *Setting) string { return x.SeoKeywords }, func(x *Setting, v string) { x.SeoKeywords = v }, data.Settings.SeoKeywords)
		apply(func(x *Setting) string { return x.ContactEmail }, func(x *Setting, v string) { x.ContactEmail = v }, data.Settings.ContactEmail)
		apply(func(x *Setting) string { return x.ContactPhone }, func(x *Setting, v string) { x.ContactPhone = v }, data.Settings.ContactPhone)
		apply(func(x *Setting) string { return x.ICP }, func(x *Setting, v string) { x.ICP = v }, data.Settings.ICP)
		apply(func(x *Setting) string { return x.FooterNote }, func(x *Setting, v string) { x.FooterNote = v }, data.Settings.FooterNote)
		apply(func(x *Setting) string { return x.TitleSuffix }, func(x *Setting, v string) { x.TitleSuffix = v }, data.Settings.TitleSuffix)
		apply(func(x *Setting) string { return x.FaviconURL }, func(x *Setting, v string) { x.FaviconURL = v }, data.Settings.FaviconURL)
		apply(func(x *Setting) string { return x.OgImageURL }, func(x *Setting, v string) { x.OgImageURL = v }, data.Settings.OgImageURL)
		if len(data.Settings.Socials) > 0 {
			if b, err := json.Marshal(data.Settings.Socials); err == nil && cur.SocialsJSON != string(b) {
				cur.SocialsJSON = string(b)
				changed++
			}
		}
		if strings.TrimSpace(data.Settings.I18nJSON) != "" {
			if norm, err := pi18n.Normalize(data.Settings.I18nJSON, siteLocales{}, settingI18nFields); err == nil && norm != "" {
				if cur.I18nJSON != norm {
					cur.I18nJSON = norm
					changed++
				}
			} else if err != nil {
				return err
			}
		}
		if changed > 0 {
			if err := tx.Save(&cur).Error; err != nil {
				return err
			}
			rep.Settings = changed
		}
	}

	// 2. 导航：品牌门面的一部分，随演示整体替换（清空后按演示清单重建）。
	if len(data.Navs) > 0 {
		if err := tx.Exec("DELETE FROM plg_website_nav").Error; err != nil {
			return err
		}
		for i := range data.Navs {
			n := data.Navs[i]
			n.Enabled = true
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
			rep.Navs++
		}
	}

	// 3. 特性卡片：同样是品牌门面，整体替换。
	if len(data.Features) > 0 {
		if err := tx.Exec("DELETE FROM plg_website_feature").Error; err != nil {
			return err
		}
		for i := range data.Features {
			f := data.Features[i]
			f.Enabled = true
			if err := tx.Create(&f).Error; err != nil {
				return err
			}
			rep.Features++
		}
	}

	// 4. 新闻栏目 / 产品类目：按 slug 判重，先建好索引供后面的内容关联。
	catBySlug := map[string]Category{}
	for i := range data.Categories {
		c := data.Categories[i]
		exists, err := demoRowExists(ctx, tx, &Category{}, c.Slug)
		if err != nil {
			return err
		}
		if exists {
			rep.Skipped++
		} else {
			c.DemoOf = themeID
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			rep.Categories++
		}
		var row Category
		if err := tx.Where("slug = ?", c.Slug).First(&row).Error; err == nil {
			catBySlug[c.Slug] = row
		}
	}
	pcBySlug := map[string]ProductCategory{}
	for i := range data.ProductCategories {
		c := data.ProductCategories[i]
		exists, err := demoRowExists(ctx, tx, &ProductCategory{}, c.Slug)
		if err != nil {
			return err
		}
		if exists {
			rep.Skipped++
		} else {
			c.DemoOf = themeID
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			rep.ProductCategories++
		}
		var row ProductCategory
		if err := tx.Where("slug = ?", c.Slug).First(&row).Error; err == nil {
			pcBySlug[c.Slug] = row
		}
	}

	// 5. 单页：按 slug 判重。
	for i := range data.Pages {
		p := data.Pages[i]
		exists, err := demoRowExists(ctx, tx, &Page{}, p.Slug)
		if err != nil {
			return err
		}
		if exists {
			rep.Skipped++
			continue
		}
		p.DemoOf = themeID
		if err := tx.Create(&p).Error; err != nil {
			return err
		}
		rep.Pages++
	}

	// 6. 新闻：按 slug 判重；Categories 按 slug 解析关联，未建过的引用静默丢弃。
	for i := range data.Posts {
		p := data.Posts[i]
		exists, err := demoRowExists(ctx, tx, &Post{}, p.Slug)
		if err != nil {
			return err
		}
		if exists {
			rep.Skipped++
			continue
		}
		p.Published = true
		now := time.Now().UTC()
		p.PublishedAt = &now
		p.Views = 0
		p.DemoOf = themeID
		p.Categories = resolveDemoCats(p.Categories, catBySlug)
		if err := tx.Create(&p).Error; err != nil {
			return err
		}
		rep.Posts++
	}

	// 7. 产品：按 slug 判重；上架态 + 类目按 slug 解析关联。
	for i := range data.Products {
		p := data.Products[i]
		exists, err := demoRowExists(ctx, tx, &Product{}, p.Slug)
		if err != nil {
			return err
		}
		if exists {
			rep.Skipped++
			continue
		}
		p.Status = ProductOnline
		p.Views = 0
		p.DemoOf = themeID
		p.Categories = resolveDemoProductCats(p.Categories, pcBySlug)
		if err := tx.Create(&p).Error; err != nil {
			return err
		}
		rep.Products++
	}
	return nil
}

// resolveDemoCats / resolveDemoProductCats 把 JSON 里的 slug 引用解析成已建好的分类行。
func resolveDemoCats(refs []Category, bySlug map[string]Category) []Category {
	var out []Category
	for _, r := range refs {
		if row, ok := bySlug[r.Slug]; ok {
			out = append(out, row)
		}
	}
	return out
}

func resolveDemoProductCats(refs []ProductCategory, bySlug map[string]ProductCategory) []ProductCategory {
	var out []ProductCategory
	for _, r := range refs {
		if row, ok := bySlug[r.Slug]; ok {
			out = append(out, row)
		}
	}
	return out
}

func demoRowExists(ctx context.Context, db *gorm.DB, model any, slug string) (bool, error) {
	var c int64
	if err := db.WithContext(ctx).Model(model).Where("slug = ?", slug).Count(&c).Error; err != nil {
		return false, err
	}
	return c > 0, nil
}

// SeedDemoByID 跑某个已注册主题的演示导入；无 seed 数据的主题返回 400。
func SeedDemoByID(ctx context.Context, id string, s *Service, log *zap.Logger) (DemoReport, error) {
	themeMu.Lock()
	t := themeReg[id]
	themeMu.Unlock()
	if t == nil {
		return DemoReport{}, response.NotFound("website.theme_not_found")
	}
	if t.seed == nil {
		return DemoReport{}, response.BadRequest("website.theme_no_demo")
	}
	return t.seed(ctx, s, log)
}
