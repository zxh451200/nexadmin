package website

import (
	"context"
	"encoding/json"
	"fmt"
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
// Keyword 匹配标题与 slug；Status 取产品状态集（空 = 全部）；CategoryID 收窄到类目。
func productTx(tx *gorm.DB, q ContentQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		tx = tx.Where("LOWER(title) LIKE LOWER(?) OR LOWER(slug) LIKE LOWER(?)", "%"+kw+"%", "%"+kw+"%")
	}
	if productStatuses[q.Status] {
		tx = tx.Where("status = ?", q.Status)
	}
	// 类目筛选只对产品生效（关联表固定，无用户输入拼接）。
	if q.CategoryID > 0 {
		tx = tx.Where("id IN (SELECT product_id FROM plg_website_product_category_rel WHERE category_id = ?)", int64(q.CategoryID))
	}
	if code := strings.TrimSpace(q.Missing); code != "" {
		if l, ok := LanguageByCode(code); ok {
			// nexadmin:like-exempt 非关键词筛选：匹配的是配置里的语言 code，两侧同源。
			tx = tx.Where("i18n_json IS NULL OR i18n_json = '' OR i18n_json NOT LIKE ?", "%\""+l.Code+"\":%")
		}
	}
	return tx
}

// ListProducts 是管理端产品列表：带类目徽标，按更新时间倒序。
func (s *Service) ListProducts(ctx context.Context, q ContentQuery) ([]Product, int64, error) {
	tx := productTx(s.db.WithContext(ctx).Model(&Product{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Product
	// 列表要展示类目徽标，整页一次带出；关联行数远小于产品行数，不构成 N+1。
	if err := tx.Preload("Categories").Order("updated_at DESC, id DESC").
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

// normalizeProductKeywords 把关键词压成「最多 3 个、空格分隔」的规范形态：
// 输入按空白与中英文逗号拆分，忽略大小写去重（Alibaba / alibaba 是同一个词）。
func normalizeProductKeywords(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '，' || r == '、'
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, maxKeywordsPerProduct)
	for _, f := range fields {
		f = strings.TrimSpace(f)
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
	return trimRunes(strings.Join(out, " "), 300)
}

// SaveProduct 新建或更新一条商品。Status 只接受 draft / online——offline 由列表的
// 上下架动作写入，pending / rejected 属于阿里的审核流转，本地表单不该直接设。
func (s *Service) SaveProduct(ctx context.Context, id idgen.ID, f ProductForm) (*Product, error) {
	f.Slug = strings.TrimSpace(f.Slug)
	f.Title = trimLen(f.Title, 200)
	f.Summary = trimLen(f.Summary, 500)
	f.SeoTitle = trimLen(f.SeoTitle, 200)
	f.SeoDesc = trimLen(f.SeoDesc, 300)
	f.SeoKeywords = trimLen(f.SeoKeywords, 300)
	f.CoverURL = trimLen(f.CoverURL, 255)
	f.VideoURL = trimLen(f.VideoURL, 255)
	f.Port = trimLen(f.Port, 64)
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
	row.SpecsJSON, row.Author = specsJSON, f.Author
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

// SetProductsStatus 批量上架/下线商品，返回实际改动条数。草稿可以直接下线成 offline
//（阿里的「下架」页签就是这么算的），上架则把任何状态都顶到 online。
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

// CopyProduct 复制一件商品：内容、译文、图片、贸易字段与类目原样带走，状态归草稿、
// 同步信息清零（副本还没同步过阿里），slug 取 <slug>-copy，撞车递增为 -copy-2、-copy-3…。
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
		Status: ProductDraft,
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

// PublishedProductsPage 是公开产品列表的分页查询：返回该语言已上架的商品与总数。
// 窗口内先按语言筛出真实集合再切片取页，总数是筛完的准确值；term 按类目 slug 收窄。
func (s *Service) PublishedProductsPage(ctx context.Context, lang string, term termFilter, page, pageSize int) ([]Product, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = productsPageSize
	}
	windowTx := liveProductTx(s.db.WithContext(ctx))
	if term.Kind == "category" {
		windowTx = windowTx.Where("id IN (SELECT pr.product_id FROM plg_website_product_category_rel pr JOIN plg_website_product_category pc ON pc.id = pr.category_id WHERE pc.slug = ?)", term.Slug)
	}
	var window []Product
	if err := windowTx.
		Select("id", "published_json").Order("updated_at DESC, id DESC").
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
		Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
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

// productCategoryCountRow 是「某个类目下有多少商品」的一行聚合结果。
type productCategoryCountRow struct {
	CategoryID int64 `gorm:"column:category_id"`
	Count      int64 `gorm:"column:c"`
}

// ListProductCategories 是类目的平铺清单（前端自己组树），带每个类目的商品数。
func (s *Service) ListProductCategories(ctx context.Context) ([]ProductCategory, error) {
	var rows []ProductCategory
	if err := s.db.WithContext(ctx).Order("sort, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	var counts []productCategoryCountRow
	if err := s.db.WithContext(ctx).Table("plg_website_product_category_rel").
		Select("category_id, COUNT(*) AS c").Group("category_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]int64, len(counts))
	for _, r := range counts {
		byID[r.CategoryID] = r.Count
	}
	for i := range rows {
		rows[i].ProductCount = byID[int64(rows[i].ID)]
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
	for i := range rows {
		rows[i].localize(lang)
	}
	return rows, nil
}

// IsSynced 报告商品已同步到阿里（列表同步徽标的展示语义；一期只读，二期由同步任务写入）。
func (p Product) IsSynced() bool { return p.SyncStatus == ProductSyncSynced }

// ProductCategoryLinks 把产品类目集合转成模板可渲染的导航项，并标出当前归档的那一项。
func ProductCategoryLinks(rows []ProductCategory, active string) []termLink {
	out := make([]termLink, 0, len(rows))
	for _, c := range rows {
		out = append(out, termLink{Name: c.Title, Slug: c.Slug, Active: c.Slug == active})
	}
	return out
}
