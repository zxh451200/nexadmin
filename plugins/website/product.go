package website

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/htmlsafe"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
)

// 产品中心：对标阿里国际站的商品发布与商品管理。字段、状态机与多语言口径见 model.go；
// 本文件只放服务层——管理端 CRUD 与公开侧查询，路由在 plugin.go 注册。

const (
	// maxImagesPerProduct 是图集上限：对标阿里主图位（主图 1 张 + 图集），堆上百张等于没有。
	maxImagesPerProduct = 10
	// maxSpecsPerProduct 是动态属性条数上限。
	maxSpecsPerProduct = 50
	// maxKeywordsPerProduct 是搜索关键词个数上限：对标阿里的 1-3 个关键词。
	maxKeywordsPerProduct = 3
	// productPageCap 是公开产品列表一次参与排序的行数上限，语义同 newsPageCap。
	productPageCap = 2000
	// productsPageSize 是公开产品列表每页条数。
	productsPageSize = 24
)

// productTx 是产品列表的筛选条件，复用页面/新闻的 ContentQuery：
// Keyword 匹配标题与 slug；Statuses 取产品状态集（空 = 全部，多值 IN）；CategoryIDs 收窄到类目（多选并集）。
func productTx(tx *gorm.DB, q ContentQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("LOWER(title) LIKE LOWER(?) OR LOWER(slug) LIKE LOWER(?)", "%"+kw+"%", "%"+kw+"%")
	}
	sts := make([]string, 0, len(q.Statuses))
	for _, st := range q.Statuses {
		if productStatuses[st] {
			sts = append(sts, st)
		}
	}
	if len(sts) > 0 {
		tx = tx.Where("status IN ?", sts)
	}
	// 类目筛选只对产品生效（关联表固定，无用户输入拼接）。
	if len(q.CategoryIDs) > 0 {
		ids := make([]int64, 0, len(q.CategoryIDs))
		for _, id := range q.CategoryIDs {
			ids = append(ids, int64(id))
		}
		tx = tx.Where("id IN (SELECT product_id FROM plg_website_product_category_rel WHERE category_id IN ?)", ids)
	}
	tx = missingTx(tx, q.Missings)
	return tx
}

// ListProducts 是管理端产品列表：带类目徽标，按更新时间倒序。
func (s *Service) ListProducts(ctx context.Context, q ContentQuery) ([]Product, int64, error) {
	ids, err := s.categorySubtreeFilter(ctx, q.CategoryIDs)
	if err != nil {
		return nil, 0, err
	}
	q.CategoryIDs = ids
	tx := productTx(s.db.WithContext(ctx).Model(&Product{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Product
	// 列表要展示类目徽标，整页一次带出；关联行数远小于产品行数，不构成 N+1。
	// 表头排序（标题/状态/浏览量/价格/时间）命中白名单时覆盖默认「最近更新在前」。
	if err := tx.Preload("Categories").Order(contentOrder(q.Sort, q.Desc, map[string]string{
		"title":      "title",
		"status":     "status",
		"views":      "views",
		"price":      "price_min",
		"updated_at": "updated_at",
		"created_at": "created_at",
	}, "updated_at DESC, id DESC")).
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ProductByID 按主键取产品（含草稿）；管理端签发预览链接用。
func (s *Service) ProductByID(ctx context.Context, id idgen.ID) (*Product, error) {
	var row Product
	if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ProductBySlug 按 slug 取产品（不看发布态）并按语言落译文。预览链路用：
// 发布校验由调用方决定（PublishedProductBy 管公开访问，预览令牌管例外放行）。
func (s *Service) ProductBySlug(ctx context.Context, slug, lang string) (*Product, error) {
	var row Product
	if err := s.db.WithContext(ctx).Preload("Categories").Where("slug = ?", slug).First(&row).Error; err != nil {
		return nil, err
	}
	row.localize(lang)
	return &row, nil
}

// normalizeProductKeywords 把关键词压成「最多 3 个、逗号分隔」的规范形态。
// 只按逗号/顿号/分号/换行拆：关键词是**词组**（阿里官方示例 "Red 8GB mp3 for sports"），
// 按空格拆会把一个词组拆成若干垃圾词——"energy storage cabinet, 215kwh battery" 之前就是这样
// 被截成三个单词、第二个词组整条丢掉的。词内多余空白压成一个空格，忽略大小写去重。
func normalizeProductKeywords(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '\n' || r == '\r'
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, maxKeywordsPerProduct)
	for _, f := range fields {
		f = strings.Join(strings.Fields(f), " ")
		if f == "" {
			continue
		}
		key := strings.ToLower(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
		if len(out) == maxKeywordsPerProduct {
			break
		}
	}
	return trimRunes(strings.Join(out, ", "), 300)
}

// SaveProduct 新建或更新一条商品。Status 只接受 draft / online——offline 由列表的
// 上下架动作写入，本地表单不该直接设。
func (s *Service) SaveProduct(ctx context.Context, id idgen.ID, f ProductForm) (*Product, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = trimLen(f.Title, 200)
	f.Summary = trimLen(f.Summary, 500)
	f.SeoTitle = trimLen(f.SeoTitle, 200)
	f.SeoDesc = trimLen(f.SeoDesc, 300)
	f.SeoKeywords = trimLen(f.SeoKeywords, 300)
	f.CoverURL = trimLen(f.CoverURL, 255)
	f.VideoURL = trimLen(f.VideoURL, 255)
	f.Port = trimLen(f.Port, 100)
	f.PaymentTerms = trimLen(f.PaymentTerms, 100)
	f.PriceUnit = trimLen(f.PriceUnit, 32)
	f.Author = trimLen(f.Author, 64)
	if f.Title == "" {
		return nil, response.BadRequest("website.product_title_required")
	}
	slugOut, err := s.resolveSlug(ctx, &Product{}, f.Slug, f.Title, id)
	if err != nil {
		return nil, err
	}
	if f.Status != ProductOnline {
		f.Status = ProductDraft
	}
	// 贸易字段：负数一律按 0 处理；价格区间写反了就取较小值，别让详情页画出反向区间。
	if f.PriceMin < 0 {
		f.PriceMin = 0
	}
	if f.PriceMax < 0 {
		f.PriceMax = 0
	}
	if f.PriceMax > 0 && f.PriceMax < f.PriceMin {
		f.PriceMax = f.PriceMin
	}
	if f.Moq < 0 {
		f.Moq = 0
	}
	if f.LeadTimeDays < 0 {
		f.LeadTimeDays = 0
	}
	if f.FreightAmount < 0 {
		f.FreightAmount = 0
	}
	if f.Currency = strings.TrimSpace(strings.ToUpper(f.Currency)); f.Currency == "" {
		f.Currency = "USD"
	}
	keywords := normalizeProductKeywords(f.Keywords)
	// 图集与动态属性都是 JSON 文本列：在这里洗成规范形态，三库存同一份字节。
	images := make([]string, 0, len(f.Images))
	for _, img := range f.Images {
		if img = trimLen(strings.TrimSpace(img), 255); img != "" {
			images = append(images, img)
		}
		if len(images) == maxImagesPerProduct {
			break
		}
	}
	imagesJSON := ""
	if len(images) > 0 {
		b, err := json.Marshal(images)
		if err != nil {
			return nil, err
		}
		imagesJSON = string(b)
	}
	specs := make([]SpecItem, 0, len(f.Specs))
	for _, sp := range f.Specs {
		label, value := trimRunes(strings.TrimSpace(sp.Label), 100), trimRunes(strings.TrimSpace(sp.Value), 500)
		if label == "" && value == "" {
			continue
		}
		specs = append(specs, SpecItem{Label: label, Value: value})
		if len(specs) == maxSpecsPerProduct {
			break
		}
	}
	specsJSON := ""
	if len(specs) > 0 {
		b, err := json.Marshal(specs)
		if err != nil {
			return nil, err
		}
		specsJSON = string(b)
	}
	// 类目只认已存在的：并发下被删掉的类目静默剔除，保存后回读的真实关联就是界面的下一帧。
	var cats []ProductCategory
	if len(f.CategoryIDs) > 0 {
		ids := make([]int64, 0, len(f.CategoryIDs))
		for _, c := range f.CategoryIDs {
			if c > 0 {
				ids = append(ids, int64(c))
			}
		}
		if len(ids) > 0 {
			if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&cats).Error; err != nil {
				return nil, err
			}
		}
	}
	var row Product
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
		// 下架的商品保存后仍是下架（表单只有上架/草稿两档）：重新上架走列表的上架动作，
		// 别让一次内容修改悄悄把商品顶回草稿态。
		if row.Status == ProductOffline && f.Status == ProductDraft {
			f.Status = ProductOffline
		}
	} else {
		row = Product{}
	}
	// 类目挂在有子类目的父级上要拦住：见 assertLeafProductCategories 的说明（存量放行）。
	if err := s.assertLeafProductCategories(ctx, cats, id); err != nil {
		return nil, err
	}
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, productI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	if slugOut != "" {
		row.Slug = slugOut
	}
	row.Title, row.Keywords, row.Summary = f.Title, keywords, f.Summary
	row.SeoTitle, row.SeoDesc, row.SeoKeywords, row.Noindex = f.SeoTitle, f.SeoDesc, f.SeoKeywords, f.NoIndex
	row.CoverURL, row.ImagesJSON, row.VideoURL = f.CoverURL, imagesJSON, f.VideoURL
	row.PriceMin, row.PriceMax, row.Currency, row.PriceUnit = f.PriceMin, f.PriceMax, f.Currency, f.PriceUnit
	row.Moq, row.LeadTimeDays, row.Port, row.PaymentTerms = f.Moq, f.LeadTimeDays, f.Port, f.PaymentTerms
	row.FreightAmount = f.FreightAmount
	row.SpecsJSON, row.Author = specsJSON, f.Author
	// 认证清单：nil = 本次不改（老客户端不带该字段），空数组 = 运营清空。
	if f.Certifications != nil {
		row.CertificationsJSON = normalizeCertifications(f.Certifications)
	}
	row.Content, row.Status, row.I18nJSON = htmlsafe.Sanitize(f.Content), f.Status, i18n
	pub, err := pi18n.NormalizePublish(f.PublishedJSON, row.Published(), pi18n.ParseDoc(i18n),
		map[string]string{"title": row.Title, "keywords": row.Keywords, "summary": row.Summary,
			"seoTitle": row.SeoTitle, "seoDesc": row.SeoDesc, "content": row.Content},
		siteLocales{}, productI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.PublishedJSON = pub
	// 正文与类目关联一起写：类目挂一半算保存失败，界面上重新来一遍比解释一半成功容易。
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return tx.Model(&row).Association("Categories").Replace(cats)
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteProduct 删除商品并级联清掉类目关联：留着悬空关联，类目的「商品数」计数会永久虚高。
func (s *Service) DeleteProduct(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Product{}, "id = ?", int64(id)).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM plg_website_product_category_rel WHERE product_id = ?", int64(id)).Error
	})
}

// DeleteProducts 批量删除商品并级联清掉类目关联，返回实际删除条数。
func (s *Service) DeleteProducts(ctx context.Context, ids []idgen.ID) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.product_ids_empty")
	}
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&Product{}, "id IN ?", int64IDs(ids))
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		if deleted == 0 {
			return nil
		}
		return tx.Exec("DELETE FROM plg_website_product_category_rel WHERE product_id IN ?", int64IDs(ids)).Error
	})
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, response.NotFound("website.product_not_found")
	}
	return deleted, nil
}

// SetProductsStatus 批量上架/下线商品，返回实际改动条数。草稿可以直接下线成 offline，
// 上架则把任何状态都顶到 online。
func (s *Service) SetProductsStatus(ctx context.Context, ids []idgen.ID, status string) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.product_ids_empty")
	}
	tx := s.db.WithContext(ctx).Model(&Product{}).Where("id IN ?", int64IDs(ids))
	switch status {
	case ProductOnline:
		tx = tx.Where("status <> ?", ProductOnline).Update("status", ProductOnline)
	case ProductOffline:
		tx = tx.Where("status IN ?", []string{ProductOnline, ProductDraft}).Update("status", ProductOffline)
	default:
		return 0, response.BadRequest("website.product_status_invalid")
	}
	return tx.RowsAffected, tx.Error
}

// CopyProduct 复制一件商品：内容、译文、图片、贸易字段与类目原样带走，状态归草稿，
// slug 取 <slug>-copy，撞车递增为 -copy-2、-copy-3…。
func (s *Service) CopyProduct(ctx context.Context, id idgen.ID) (*Product, error) {
	var src Product
	if err := s.db.WithContext(ctx).Preload("Categories").First(&src, "id = ?", int64(id)).Error; err != nil {
		return nil, err
	}
	slug, err := s.freeProductSlug(ctx, src.Slug)
	if err != nil {
		return nil, err
	}
	row := Product{
		Slug: slug, Title: src.Title, Keywords: src.Keywords, Summary: src.Summary,
		SeoTitle: src.SeoTitle, SeoDesc: src.SeoDesc, SeoKeywords: src.SeoKeywords,
		Content: src.Content, I18nJSON: src.I18nJSON,
		CoverURL: src.CoverURL, ImagesJSON: src.ImagesJSON, VideoURL: src.VideoURL,
		PriceMin: src.PriceMin, PriceMax: src.PriceMax, Currency: src.Currency, PriceUnit: src.PriceUnit,
		Moq: src.Moq, LeadTimeDays: src.LeadTimeDays, Port: src.Port, SpecsJSON: src.SpecsJSON,
		CertificationsJSON: src.CertificationsJSON, Status: ProductDraft,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Model(&row).Association("Categories").Replace(src.Categories)
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// freeProductSlug 给副本挑一个没被占用的 slug；slug 列上限 64，放不下后缀就先截短原文
// （base 本身只含合法字符，截短不破坏 slug 规则）。
func (s *Service) freeProductSlug(ctx context.Context, base string) (string, error) {
	candidate := truncateSlug(base+"-copy", "product")
	for i := 2; ; i++ {
		var n int64
		if err := s.db.WithContext(ctx).Model(&Product{}).Where("slug = ?", candidate).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return candidate, nil
		}
		candidate = truncateSlug(fmt.Sprintf("%s-copy-%d", base, i), "product")
	}
}

// ---- 公开侧 ----

// liveProductTx 是公开端商品的上线条件：状态为 online。
func liveProductTx(tx *gorm.DB) *gorm.DB {
	return tx.Where("status = ?", ProductOnline)
}

// PublishedProductBy 取一件该语言已上架的商品；该语言未发布就当作不存在（404），
// 不把一篇中文详情挂在 /en/products/ 下。浏览量自增：非致命，计数失败不影响渲染。
func (s *Service) PublishedProductBy(ctx context.Context, slug, lang string) (*Product, error) {
	var row Product
	err := liveProductTx(s.db.WithContext(ctx)).Preload("Categories").Where("slug = ?", slug).First(&row).Error
	if err != nil {
		return &row, err
	}
	if len(Languages()) > 1 && !pi18n.Published(row.Published(), row.PublishedJSON, lang, siteLocales{}) {
		return nil, gorm.ErrRecordNotFound
	}
	row.localize(lang)
	_ = s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(row.ID)).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
	return &row, nil
}

// productSortOrder 把列表排序参数翻译成 SQL 排序（白名单，杜绝参数拼进 SQL）。
// 未定价商品（price_min = 0）在价格升序里排到最后，不会冒充「最低价」。
func productSortOrder(sort string) string {
	switch sort {
	case "price_asc":
		return "CASE WHEN price_min = 0 THEN 1 ELSE 0 END ASC, price_min ASC, id DESC"
	case "price_desc":
		return "price_min DESC, id DESC"
	default:
		return "updated_at DESC, id DESC"
	}
}

// PublishedProductsPage 是公开产品列表的分页查询：返回该语言已上架的商品与总数。
// 窗口内先按语言筛出真实集合再切片取页，总数是筛完的准确值；term 按类目 slug 收窄，
// keyword 是站内检索词（空 = 不检索）。两者可叠加，且都在分页前生效，翻页不会掉筛选。
func (s *Service) PublishedProductsPage(ctx context.Context, lang string, term termFilter, keyword string, page, pageSize int) ([]Product, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = productsPageSize
	}
	windowTx := liveProductTx(s.db.WithContext(ctx))
	if term.Kind == "category" {
		slugs := term.Slugs
		if len(slugs) == 0 {
			slugs = []string{term.Slug}
		}
		// 类目树：选父类时连同所有后代类目一起命中，父节点不会因为没有直挂商品而空列表。
		windowTx = windowTx.Where("id IN (SELECT pr.product_id FROM plg_website_product_category_rel pr JOIN plg_website_product_category pc ON pc.id = pr.category_id WHERE pc.slug IN ?)", slugs)
	}
	// 检索范围覆盖「名称 / 型号(slug) / 摘要 / 搜索关键词 / 规格属性 / 译文」：
	// 两侧 LOWER 折叠大小写，PG 上大写词也能命中；全部走占位参数，不拼用户输入进 SQL。
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		windowTx = windowTx.Where(
			"LOWER(title) LIKE LOWER(?) OR LOWER(slug) LIKE LOWER(?) OR LOWER(summary) LIKE LOWER(?) OR LOWER(keywords) LIKE LOWER(?) OR LOWER(specs) LIKE LOWER(?) OR LOWER(i18n_json) LIKE LOWER(?)",
			like, like, like, like, like, like)
	}
	order := productSortOrder(term.Sort)
	var window []Product
	if err := windowTx.
		Select("id", "published_json").Order(order).
		Limit(productPageCap).Find(&window).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(window))
	for _, p := range window {
		if len(Languages()) > 1 && !pi18n.Published(true, p.PublishedJSON, lang, siteLocales{}) {
			continue
		}
		ids = append(ids, int64(p.ID))
	}
	total := int64(len(ids))
	start := (page - 1) * pageSize
	if start >= len(ids) {
		return nil, total, nil
	}
	end := start + pageSize
	if end > len(ids) {
		end = len(ids)
	}
	pageIDs := ids[start:end]
	var rows []Product
	if err := s.db.WithContext(ctx).Preload("Categories").Where("id IN ?", pageIDs).
		Order(order).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].localize(lang)
	}
	return rows, total, nil
}

// publishedProductSitemapEntries 取已上架且未关闭收录的商品（sitemap 用）：只取渲染条目需要的列。
func (s *Service) publishedProductSitemapEntries(ctx context.Context) ([]Product, error) {
	var rows []Product
	err := liveProductTx(s.db.WithContext(ctx)).Where("noindex = ?", false).
		Select("slug", "updated_at", "published_json").Order("updated_at DESC").Limit(1000).Find(&rows).Error
	return rows, err
}

// ---- 产品类目 ----

// assertLeafProductCategories 拦住「把商品挂到还有子类目的类目上」。
// 行业口径（Amazon 的 item type、Google 商品分类、Shopify taxonomy）商品只落末级节点：
// 父级是分组与归档用的，挂上去「这件商品到底属于哪个类目」就二义了——详情页的类目标签、
// 归档链接、面包屑都会同时出现父和子。既有存量挂载一律放行（老数据照常保存、可以取消），
// 只拒绝本次新加的父级挂载：判断依据是「该商品当前已经挂着的类目 id」白名单。
func (s *Service) assertLeafProductCategories(ctx context.Context, cats []ProductCategory, id idgen.ID) error {
	if len(cats) == 0 {
		return nil
	}
	kept := make(map[int64]bool, len(cats))
	if id != 0 {
		var cur []struct {
			CategoryID int64 `gorm:"column:category_id"`
		}
		if err := s.db.WithContext(ctx).Table("plg_website_product_category_rel").
			Select("category_id").Where("product_id = ?", int64(id)).Scan(&cur).Error; err != nil {
			return err
		}
		for _, c := range cur {
			kept[c.CategoryID] = true
		}
	}
	ids := make([]int64, 0, len(cats))
	for _, c := range cats {
		if !kept[int64(c.ID)] {
			ids = append(ids, int64(c.ID))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&ProductCategory{}).Where("parent_id IN ?", ids).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return response.BadRequest("website.product_category_not_leaf")
	}
	return nil
}

// categorySubtreeFilter 把筛选用的类目 id 展开成「自身 + 全部后代」。// 产品类目是树，选父类目就该把子类目下的商品一起查出来——公开站（descendantSlugs → term.Slugs）
// 一直是这个口径，管理端此前只命中直挂：同一个父类目点下去，前后台数出来的条数不一样，
// 商品只挂子类时后台甚至一条都查不到。展开用全表父子关系（类目是十几到几百条的小表），
// 不写递归 SQL，MySQL/MariaDB/PG 三套库行为一致。
// 传进来的 id 若已被删（脏 URL/旧书签），保留原值让 IN 命中空集，绝不能退化成「不过滤」。
func (s *Service) categorySubtreeFilter(ctx context.Context, ids []idgen.ID) ([]idgen.ID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []ProductCategory
	if err := s.db.WithContext(ctx).Select("id", "parent_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	parents := makeParentMap(rows)
	seen := make(map[uint64]bool, len(ids)*2)
	out := make([]idgen.ID, 0, len(ids))
	for _, id := range ids {
		for _, sub := range navSubtreeIDs(parents, uint64(id)) {
			if seen[sub] {
				continue
			}
			seen[sub] = true
			out = append(out, idgen.ID(sub))
		}
	}
	if len(out) == 0 {
		return ids, nil
	}
	return out, nil
}

// ListProductCategories 是类目的平铺清单（前端自己组树），带每个类目的商品数。
// 计数是「子树」口径：父类目 = 自身直挂 + 所有后代，同一商品在多处挂载只算一次。
// 业界归档类目（Amazon browse node / Magento 锚点类目）都是这么算的，也是唯一能和
// 筛选对上的算法——否则父类目显示 0、点进去却有一串子类商品。GROUP BY 只能给直挂数，
// 去重求和得靠集合：把每个 (商品, 类目) 沿祖先链往上记一次，祖先拿到的天然是并集。
func (s *Service) ListProductCategories(ctx context.Context) ([]ProductCategory, error) {
	var rows []ProductCategory
	if err := s.db.WithContext(ctx).Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	var rels []struct {
		ProductID  int64 `gorm:"column:product_id"`
		CategoryID int64 `gorm:"column:category_id"`
	}
	if err := s.db.WithContext(ctx).Table("plg_website_product_category_rel").
		Select("product_id, category_id").Scan(&rels).Error; err != nil {
		return nil, err
	}
	parents := makeParentMap(rows)
	seen := make(map[uint64]map[int64]struct{}, len(rows))
	for _, rel := range rels {
		id := uint64(rel.CategoryID)
		visited := make(map[uint64]bool, 4)
		for id != 0 && !visited[id] { // visited 防环：脏数据把父级指成环时别死循环
			visited[id] = true
			set := seen[id]
			if set == nil {
				set = make(map[int64]struct{}, 4)
				seen[id] = set
			}
			set[rel.ProductID] = struct{}{}
			id = parents[id]
		}
	}
	for i := range rows {
		rows[i].ProductCount = int64(len(seen[uint64(rows[i].ID)]))
	}
	return rows, nil
}

// ProductCategoryBySlug 按 slug 取类目：公开归档页用它区分「链接写错了」和「这个类目还没内容」。
func (s *Service) ProductCategoryBySlug(ctx context.Context, slug string) (*ProductCategory, error) {
	var row ProductCategory
	if err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveProductCategory 新建或更新类目；层级规则与导航同源（最多 3 级，复用 navDepth 的校验）。
func (s *Service) SaveProductCategory(ctx context.Context, id idgen.ID, f ProductCategoryForm) (*ProductCategory, error) {
	f.Title = trimLen(f.Title, 100)
	f.Slug = strings.TrimSpace(f.Slug)
	if f.Title == "" {
		return nil, response.BadRequest("website.product_category_title_required")
	}
	slugOut, err := s.resolveSlug(ctx, &ProductCategory{}, f.Slug, f.Title, id)
	if err != nil {
		return nil, err
	}
	var row ProductCategory
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
	} else {
		row = ProductCategory{}
	}
	// 层级校验与导航同一套纯函数：拒绝父级不存在、挂到自己或后代下、超过 3 级。
	var rows []ProductCategory
	if err := s.db.WithContext(ctx).Select("id", "parent_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	parents := make(map[uint64]uint64, len(rows))
	for _, r := range rows {
		if r.ID != row.ID {
			parents[uint64(r.ID)] = uint64(r.ParentID)
		}
	}
	level, err := navDepth(parents, uint64(row.ID), uint64(f.ParentID))
	if err != nil {
		return nil, err
	}
	// 挂到新父级后整棵子树会整体下移，深度上限要按子树层数一起判断。
	if id != 0 && level+navSubtreeHeight(parents, uint64(id))-1 > navMaxDepth {
		return nil, navBadRequest("website.nav_depth_exceeded", navMaxDepth)
	}
	row.ParentID, row.Title, row.Sort = f.ParentID, f.Title, f.Sort
	if slugOut != "" {
		row.Slug = slugOut
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteProductCategory 在类目子树（含自身）还有商品时拒绝删除——先让人把商品挪走，
// 静默解除关联会让「类目删了、商品还在列表里但分类徽标消失」这类事没人察觉。
// 没有商品时级联删除自身与全部后代，并清掉对应关联。
func (s *Service) DeleteProductCategory(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []ProductCategory
		if err := tx.Select("id", "parent_id").Find(&rows).Error; err != nil {
			return err
		}
		ids := navSubtreeIDs(makeParentMap(rows), uint64(id))
		if len(ids) == 0 {
			return nil
		}
		list := make([]int64, len(ids))
		for i, v := range ids {
			list[i] = int64(v)
		}
		var n int64
		if err := tx.Table("plg_website_product_category_rel").
			Where("category_id IN ?", list).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return response.BadRequest("website.product_category_in_use")
		}
		if err := tx.Delete(&ProductCategory{}, "id IN ?", list).Error; err != nil {
			return err
		}
		return nil
	})
}

// makeParentMap 把类目行压成 id → parentId 的映射（navParentMap 的类目版）。
func makeParentMap(rows []ProductCategory) map[uint64]uint64 {
	out := make(map[uint64]uint64, len(rows))
	for _, r := range rows {
		out[uint64(r.ID)] = uint64(r.ParentID)
	}
	return out
}

// ReorderProductCategories 在一个事务内批量写入 parentId / sort（拖拽排序与改上级共用），
// 层级校验与导航同一套；sort 为 0 时按 items 顺序补 10、20…。
func (s *Service) ReorderProductCategories(ctx context.Context, items []ProductCategoryOrderItem) error {
	if len(items) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []ProductCategory
		if err := tx.Select("id", "parent_id").Find(&rows).Error; err != nil {
			return err
		}
		// 孤儿行（父级已被并发删除/历史脏数据）先静默洗回顶级，再继续——口径同 ReorderNavs。
		known := make(map[uint64]bool, len(rows))
		for _, r := range rows {
			known[uint64(r.ID)] = true
		}
		for _, r := range rows {
			pid := uint64(r.ParentID)
			if pid == 0 || known[pid] {
				continue
			}
			if err := tx.Model(&ProductCategory{}).Where("id = ?", int64(r.ID)).Update("parent_id", 0).Error; err != nil {
				return err
			}
			r.ParentID = 0
		}
		parents := makeParentMap(rows)
		for i := range items {
			if _, ok := parents[uint64(items[i].ID)]; !ok {
				return navBadRequest("website.product_category_not_found")
			}
			if items[i].Sort == 0 {
				items[i].Sort = (i + 1) * 10
			}
			parents[uint64(items[i].ID)] = uint64(items[i].ParentID)
		}
		if err := validateNavParents(parents); err != nil {
			return err
		}
		for _, it := range items {
			if err := tx.Model(&ProductCategory{}).Where("id = ?", int64(it.ID)).
				Updates(map[string]any{"parent_id": it.ParentID, "sort": it.Sort}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ProductImages 把图集 JSON 解析成 URL 列表；脏数据静默丢弃，渲染别因一张图挂掉。
func (p Product) ProductImages() []string {
	if strings.TrimSpace(p.ImagesJSON) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(p.ImagesJSON), &out); err != nil {
		return nil
	}
	return out
}

// ProductSpecs 把动态属性 JSON 解析成键值对；脏数据静默丢弃。
func (p Product) ProductSpecs() []SpecItem {
	if strings.TrimSpace(p.SpecsJSON) == "" {
		return nil
	}
	var out []SpecItem
	if err := json.Unmarshal([]byte(p.SpecsJSON), &out); err != nil {
		return nil
	}
	return out
}

// relatedProductLimit 是详情页「同类产品」的条数：一行卡片刚好放满。
const relatedProductLimit = 4

// PublishedRelatedProducts 取同类产品推荐：与目标商品共享任一类目的已上架商品，
// 排除自身与重复，按更新时间倒序取前几条；目标商品没有类目时返回空（不做全站兜底，
// 「推荐」挂了就空着，比把不相干的产品硬塞进去好）。
func (s *Service) PublishedRelatedProducts(ctx context.Context, lang string, product *Product) ([]Product, error) {
	catIDs := make([]int64, 0, len(product.Categories))
	for _, c := range product.Categories {
		catIDs = append(catIDs, int64(c.ID))
	}
	if len(catIDs) == 0 {
		return nil, nil
	}
	var rows []Product
	err := liveProductTx(s.db.WithContext(ctx)).
		Where("id <> ?", int64(product.ID)).
		Where("id IN (SELECT product_id FROM plg_website_product_category_rel WHERE category_id IN ?)", catIDs).
		Preload("Categories").
		Order("updated_at DESC, id DESC").
		Limit(relatedProductLimit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	// 同类目不够就拿其他已上架商品补齐：一行 4 张卡的位置只挂 1 张，
	// 详情页底部会空掉一大块（库里新品少的时候最常见）。
	if len(rows) < relatedProductLimit {
		skip := make([]int64, 0, len(rows)+1)
		skip = append(skip, int64(product.ID))
		for _, r := range rows {
			skip = append(skip, int64(r.ID))
		}
		var extra []Product
		if eerr := liveProductTx(s.db.WithContext(ctx)).
			Where("id NOT IN ?", skip).
			Preload("Categories").
			Order("updated_at DESC, id DESC").
			Limit(relatedProductLimit - len(rows)).
			Find(&extra).Error; eerr != nil {
			// 补齐失败就用同类目结果，推荐区照常出，不影响详情页
			extra = nil
		}
		rows = append(rows, extra...)
	}
	for i := range rows {
		rows[i].localize(lang)
	}
	return rows, nil
}

// paymentChip 是详情页「安全支付」里的一枚支付方式徽标。
// Label 是给人看的名字，Kind 决定品牌样式（visa/mc/apple/paypal），Icon 是 uiIconPaths 里的图标名。
// 品牌项由模板按 Kind 输出 static/pay/*.svg（payrexx/payment-logos，CC0-1.0，官方彩色原图，
// 自带白底卡片）；商标仍归各品牌方，这里只作「受理该支付方式」的指示性使用，不改形、不暗示背书。
// 贸易术语（T/T、L/C…）不是品牌，仍走中性线框图标；未收录的取值退化成 plain 文本。
// 后台填什么就展示什么。
type paymentChip struct {
	Label string
	Kind  string
	Icon  string
}

// paymentAliases 把后台「付款方式」里的常见写法归一成徽标。
// 键统一小写、去空格；没收录的写法原样展示，不做猜测。
var paymentAliases = map[string]paymentChip{
	"t/t":                  {Label: "T/T", Kind: "plain", Icon: "bank"},
	"tt":                   {Label: "T/T", Kind: "plain", Icon: "bank"},
	"telegraphic transfer": {Label: "T/T", Kind: "plain", Icon: "bank"},
	"wire transfer":        {Label: "T/T", Kind: "plain", Icon: "bank"},
	"bank transfer":        {Label: "T/T", Kind: "plain", Icon: "bank"},
	"电汇":                   {Label: "T/T", Kind: "plain", Icon: "bank"},
	"l/c":                  {Label: "L/C", Kind: "plain", Icon: "file-check"},
	"lc":                   {Label: "L/C", Kind: "plain", Icon: "file-check"},
	"letter of credit":     {Label: "L/C", Kind: "plain", Icon: "file-check"},
	"信用证":                  {Label: "L/C", Kind: "plain", Icon: "file-check"},
	"d/p":                  {Label: "D/P", Kind: "plain", Icon: "file-text"},
	"dp":                   {Label: "D/P", Kind: "plain", Icon: "file-text"},
	"d/a":                  {Label: "D/A", Kind: "plain", Icon: "file-text"},
	"da":                   {Label: "D/A", Kind: "plain", Icon: "file-text"},
	"o/a":                  {Label: "O/A", Kind: "plain", Icon: "file-invoice"},
	"oa":                   {Label: "O/A", Kind: "plain", Icon: "file-invoice"},
	"open account":         {Label: "O/A", Kind: "plain", Icon: "file-invoice"},
	"paypal":               {Label: "PayPal", Kind: "paypal"},
	"visa":                 {Label: "VISA", Kind: "visa"},
	"mastercard":           {Label: "Mastercard", Kind: "mc"},
	"master card":          {Label: "Mastercard", Kind: "mc"},
	"apple pay":            {Label: "Apple Pay", Kind: "apple"},
	"applepay":             {Label: "Apple Pay", Kind: "apple"},
	"alipay":               {Label: "Alipay", Kind: "plain", Icon: "wallet"},
	"支付宝":                  {Label: "Alipay", Kind: "plain", Icon: "wallet"},
	"wechat pay":           {Label: "WeChat Pay", Kind: "plain", Icon: "wallet"},
	"wechatpay":            {Label: "WeChat Pay", Kind: "plain", Icon: "wallet"},
	"微信支付":                 {Label: "WeChat Pay", Kind: "plain", Icon: "wallet"},
	"western union":        {Label: "Western Union", Kind: "plain", Icon: "send"},
	"moneygram":            {Label: "MoneyGram", Kind: "plain", Icon: "send"},
	"escrow":               {Label: "Escrow", Kind: "plain", Icon: "lock"},
	"cash":                 {Label: "Cash", Kind: "plain", Icon: "cash"},
}

// PaymentChips 把「T/T, L/C」这类自由文本拆成支付方式徽标：先按逗号/分号/顿号分段
// （不能按斜杠拆，T/T、L/C、D/P 本身就带斜杠），再按别名表归一，未收录的照原文显示。
// 同一项多次出现只留一枚；空字符串返回 nil，模板据此整段不渲染。
func (p Product) PaymentChips() []paymentChip {
	terms := strings.TrimSpace(p.PaymentTerms)
	if terms == "" {
		return nil
	}
	fields := strings.FieldsFunc(terms, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '、', '|', '\n', '\r':
			return true
		}
		return false
	})
	out := make([]paymentChip, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, raw := range fields {
		label := strings.Join(strings.Fields(raw), " ")
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		chip, ok := paymentAliases[key]
		if !ok {
			// 后台写的是「30% deposit」这类描述，原样展示但要限长，避免把侧栏撑破。
			chip = paymentChip{Label: trimRunes(label, 18), Kind: "plain"}
		}
		if seen[chip.Label] {
			continue
		}
		seen[chip.Label] = true
		out = append(out, chip)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// productCert 是商详「认证」区的一枚徽章：名称来自后台填的认证清单，
// Icon 由名称归一得到（见 certIconKey），没有对应图标的仍出文字徽章。
type productCert struct {
	Name string
	Icon string
}

// certIconGroups 把常见认证按性质归到两种图标：安全/合规类走盾牌勾，体系类走证书。
// 键是去掉空格与标点后的大写前缀，前缀匹配即可（CE-EMC、UL 1973 都能命中）。
var certIconGroups = []struct {
	prefix string
	icon   string
}{
	{"ISO", "certificate"},
	{"IATF", "certificate"},
	{"CE", "shield-check"},
	{"UL", "shield-check"},
	{"UN38", "shield-check"},
	{"UN 38", "shield-check"},
	{"IEC", "shield-check"},
	{"ROHS", "shield-check"},
	{"REACH", "shield-check"},
	{"FCC", "shield-check"},
	{"MSDS", "shield-check"},
	{"TUV", "shield-check"},
	{"CSA", "shield-check"},
	{"PSE", "shield-check"},
	{"KC", "shield-check"},
	{"CB", "shield-check"},
}

// certIconKey 归一认证名称到图标键：先大写去空格，再按前缀匹配；
// 认不出就默认 shield-check（认证徽章本身语义就是「通过检验」，比无图标更整齐）。
func certIconKey(name string) string {
	up := strings.ToUpper(strings.Join(strings.Fields(name), ""))
	for _, g := range certIconGroups {
		if strings.HasPrefix(up, strings.ToUpper(strings.ReplaceAll(g.prefix, " ", ""))) {
			return g.icon
		}
	}
	return "shield-check"
}

// normalizeCertifications 把表单提交的认证名称清洗成入库的 JSON 数组文本：
// 去空白、去重（保持录入顺序）、每项限 24 字、最多 12 项；全空返回空串（列存空，不加 []）。
func normalizeCertifications(in []string) string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		name := strings.Join(strings.Fields(raw), " ")
		if name == "" {
			continue
		}
		name = trimRunes(name, 24)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= 12 {
			break
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// Certifications 解析后台填的认证清单。支持 JSON 数组（后台表单保存的形态）
// 与逗号/顿号/分号分隔的纯文本（导入或人工直填），两种都能吃；
// 去重、去空、限长 24 字，返回 nil 时模板整段不渲染。
func (p Product) Certifications() []productCert {
	raw := strings.TrimSpace(p.CertificationsJSON)
	if raw == "" {
		return nil
	}
	var names []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &names); err != nil {
			// 坏 JSON 不整块丢弃：退回按分隔符切，尽量把能认的显示出来。
			names = strings.FieldsFunc(raw, func(r rune) bool {
				switch r {
				case ',', '，', ';', '；', '、', '|', '\n', '\r', '[', ']', '"':
					return true
				}
				return false
			})
		}
	} else {
		names = strings.FieldsFunc(raw, func(r rune) bool {
			switch r {
			case ',', '，', ';', '；', '、', '|', '\n', '\r':
				return true
			}
			return false
		})
	}
	out := make([]productCert, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		name := strings.Join(strings.Fields(n), " ")
		if name == "" {
			continue
		}
		name = trimRunes(name, 24)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, productCert{Name: name, Icon: certIconKey(name)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// productVideo 是详情页能直接播的视频：Kind=file 交给原生 <video>，
// Kind=embed 交给第三方播放器 iframe。前端据此决定用哪种播放器。
type productVideo struct {
	Kind string
	Src  string
}

// videoFileExt 是浏览器原生能播的容器扩展名。m3u8 不在内：那要额外加载 hls.js，
// 而且各家切片鉴权不一致，遇到就回退成「新窗口打开」链接，不假装能播。
var videoFileExt = map[string]bool{
	".mp4": true, ".webm": true, ".ogv": true, ".ogg": true, ".m4v": true, ".mov": true,
}

// youtubeHosts 是 YouTube 的各种落地域名（含无 cookie 版与短链 youtu.be）。
var youtubeHosts = map[string]bool{
	"youtube.com": true, "m.youtube.com": true, "music.youtube.com": true,
	"youtube-nocookie.com": true, "youtu.be": true,
}

// videoID 只接受字母数字与 -_，长度 6-24：防止把奇怪的路径拼进 iframe src。
func videoID(s string) string {
	if len(s) < 6 || len(s) > 24 {
		return ""
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return s
}

// Video 解析后台「产品视频」地址，给出可直接内嵌的播放形式：
//   - 直链文件（.mp4/.webm/…）→ file
//   - YouTube / Vimeo / Bilibili → embed（各自的嵌入播放器地址，自动播放）
//   - 其他（第三方落地页、m3u8、空值）→ nil，模板回退成「新窗口打开」链接，不假装能内嵌
//
// 只认域名白名单、不接受任意 URL 塞进 iframe：后台字段是自由文本，白名单是必要的边界。
func (p Product) Video() *productVideo {
	raw := strings.TrimSpace(p.VideoURL)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	if host == "" {
		// 站内相对路径（比如后台上传到本站媒体库的 mp4）
		if videoFileExt[strings.ToLower(path.Ext(u.Path))] {
			return &productVideo{Kind: "file", Src: raw}
		}
		return nil
	}
	if youtubeHosts[host] {
		id := ""
		if host == "youtu.be" {
			id = videoID(strings.Trim(u.Path, "/"))
		} else if v := u.Query().Get("v"); v != "" {
			id = videoID(v)
		} else {
			segs := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(segs) >= 2 {
				switch segs[0] {
				case "embed", "shorts", "live", "v":
					id = videoID(segs[1])
				}
			}
		}
		if id == "" {
			return nil
		}
		return &productVideo{Kind: "embed", Src: "https://www.youtube-nocookie.com/embed/" + id + "?autoplay=1&rel=0&playsinline=1"}
	}
	if host == "vimeo.com" || host == "player.vimeo.com" {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i := len(segs) - 1; i >= 0; i-- {
			if segs[i] == "" {
				continue
			}
			if _, err := strconv.Atoi(segs[i]); err == nil {
				return &productVideo{Kind: "embed", Src: "https://player.vimeo.com/video/" + segs[i] + "?autoplay=1"}
			}
			break
		}
		return nil
	}
	if host == "bilibili.com" || host == "m.bilibili.com" || host == "player.bilibili.com" {
		// 播放器嵌入页把 BV 号放在 ?bvid= 而不是路径里：从 B 站「嵌入代码」直接粘过来的
		// 链接也要能用，否则后台填了地址、详情页却没有视频。
		if bv := u.Query().Get("bvid"); len(bv) > 2 && strings.EqualFold(bv[:2], "bv") {
			if id := videoID(bv[2:]); id != "" {
				return &productVideo{Kind: "embed", Src: "https://player.bilibili.com/player.html?bvid=BV" + id + "&autoplay=1&high_quality=1&danmaku=0"}
			}
			return nil
		}
		for _, seg := range strings.Split(strings.Trim(u.Path, "/"), "/") {
			if !strings.HasPrefix(seg, "BV") {
				continue
			}
			if id := videoID(strings.TrimPrefix(seg, "BV")); id != "" {
				return &productVideo{Kind: "embed", Src: "https://player.bilibili.com/player.html?bvid=BV" + id + "&autoplay=1&high_quality=1&danmaku=0"}
			}
		}
		return nil
	}
	if videoFileExt[strings.ToLower(path.Ext(u.Path))] {
		return &productVideo{Kind: "file", Src: raw}
	}
	return nil
}

// productCatLink 是公开站产品中心左侧分类树的一行。
// ProductCategoryLinks 产出先序扁平列表（面包屑/芯片等按顺序消费），
// NestProductCategories 再把它折叠成树：一级平铺、二级 hover 浮层用 Children 渲染。
// ActiveTrail 标出当前分类的祖先链，模板据此给父级加高亮相。
type productCatLink struct {
	Name        string
	Slug        string
	Href        string
	Active      bool
	ActiveTrail bool
	Depth       int
	Parent      string
	HasChildren bool
	Count       int64
	Children    []productCatLink
}

// ProductCategoryLinks 把平铺类目装配成先序扁平列表（保留父子顺序），并标出当前归档项。
// 输出仍是列表而不是嵌套树：面包屑、首页芯片这类「按顺序取前 N 个」的消费方不必递归。
// 左栏分类树需要层级时再走 NestProductCategories 折一层。
func ProductCategoryLinks(rows []ProductCategory, active string) []productCatLink {
	byID := make(map[uint64]int, len(rows))
	for i := range rows {
		byID[uint64(rows[i].ID)] = i
	}
	children := make(map[uint64][]int, len(rows))
	top := make([]int, 0, len(rows))
	parentOf := make([]uint64, len(rows))
	for i := range rows {
		pid := uint64(rows[i].ParentID)
		if _, ok := byID[pid]; !ok {
			pid = 0 // 父类被删或悬空时降为顶级，和导航的降级口径一致
		}
		parentOf[i] = pid
		if pid == 0 {
			top = append(top, i)
		} else {
			children[pid] = append(children[pid], i)
		}
	}
	// 当前项往上的祖先链：父节点保持展开，视觉上也能看出当前分类挂在谁下面。
	trail := make(map[uint64]bool)
	var activeID uint64
	for i := range rows {
		if rows[i].Slug == active {
			activeID = uint64(rows[i].ID)
			break
		}
	}
	for id := activeID; id != 0; {
		idx, ok := byID[id]
		if !ok {
			break
		}
		p := parentOf[idx]
		if p == 0 {
			break
		}
		trail[p] = true
		id = p
	}
	out := make([]productCatLink, 0, len(rows))
	seen := make(map[int]bool, len(rows))
	// 整棵子树标记为已访问：空类目整枝不出现时，子节点不能被下面「成环孤儿」的兜底
	// 当成顶级再挂一遍（计数是子树口径，父类目 0 = 后代也 0）。
	var markSeen func(id uint64)
	markSeen = func(id uint64) {
		for _, child := range children[id] {
			if seen[child] {
				continue
			}
			seen[child] = true
			markSeen(uint64(rows[child].ID))
		}
	}
	var walk func(idx, depth int, parent string)
	walk = func(idx, depth int, parent string) {
		if seen[idx] || depth > 3 { // 防环 + 与后台一致的层级上限
			return
		}
		seen[idx] = true
		c := rows[idx]
		id := uint64(c.ID)
		// 没有商品的类目不进前台导航：点进去只有空归档页，行业站（Amazon browse node /
		// Magento 锚点类目）都不挂空节点。当前选中的那条例外——跟着链接进来的人要看得出自己在哪。
		if c.ProductCount == 0 && c.Slug != active && !trail[id] {
			markSeen(id)
			return
		}
		parentIdx := len(out)
		out = append(out, productCatLink{
			Name:        c.Title,
			Slug:        c.Slug,
			Active:      c.Slug == active,
			ActiveTrail: trail[id],
			Depth:       depth,
			Parent:      parent,
			// 先留空，等子节点走完再回填：有子类目记录 ≠ 有子类目露出。
			HasChildren: false,
			Count:       c.ProductCount,
		})
		for _, child := range children[id] {
			walk(child, depth+1, c.Slug)
		}
		// 子类目全是空类目时上面会整枝跳过（不挂空节点），此时若还标 HasChildren，
		// 前台就是「有个 › 箭头、点开/悬停是空的」——按实际渲染出的子节点数回填。
		if len(out) > parentIdx+1 {
			out[parentIdx].HasChildren = true
		}
	}
	for _, idx := range top {
		walk(idx, 0, "")
	}
	// 成环的孤儿节点也要露出来，别让类目凭空消失。
	for i := range rows {
		if !seen[i] {
			walk(i, 0, "")
		}
	}
	return out
}

// NestProductCategories 把 ProductCategoryLinks 的先序列出折成层级树（只保留一级为根）。
// 先序列表保证「一个节点的后代紧跟其后、深度大于它」，所以同一遍扫描就能递归收子树，
// 不需要再查一次父子表。深度跳级（父类被删导致 depth 不连续）的节点按最近的上层挂载，
// 不会被丢掉。
func NestProductCategories(flat []productCatLink) []productCatLink {
	var build func(start, depth int) ([]productCatLink, int)
	build = func(start, depth int) ([]productCatLink, int) {
		out := make([]productCatLink, 0, 4)
		i := start
		for i < len(flat) {
			row := flat[i]
			if row.Depth < depth {
				break
			}
			if row.Depth > depth {
				// 层级跳级：往上提一层挂载，保持可见。
				row.Depth = depth
			}
			i++
			if i < len(flat) && flat[i].Depth > depth {
				row.Children, i = build(i, depth+1)
			}
			out = append(out, row)
		}
		return out, i
	}
	tree, _ := build(0, 0)
	return tree
}

func descendantSlugs(rows []ProductCategory, active string) []string {
	if active == "" {
		return nil
	}
	byID := make(map[uint64]string, len(rows))
	for i := range rows {
		byID[uint64(rows[i].ID)] = rows[i].Slug
	}
	children := make(map[string][]string, len(rows))
	for i := range rows {
		if p, ok := byID[uint64(rows[i].ParentID)]; ok {
			children[p] = append(children[p], rows[i].Slug)
		}
	}
	out := make([]string, 0, 1)
	seen := make(map[string]bool)
	var walk func(string)
	walk = func(s string) {
		if seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
		for _, c := range children[s] {
			walk(c)
		}
	}
	walk(active)
	return out
}
