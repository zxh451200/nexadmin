package website

// product_io.go 是产品中心的「进出」两条通道：
//   - 导出：POST /products/export-async 提交 website.product-export 异步任务（与 posts / messages
//     的导出同一套机制），进度与下载在导出中心。
//   - 导入：实现 importspec.Provider 把商品接进通用导入中心（plugins/imports），
//     文件解析、模板下载、逐行报错与导入台账都由导入中心负责，这里只写业务映射。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/i18n"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/importspec"
)

// SourceProducts 是产品导入源在导入中心台账里的标识。
const SourceProducts = "website_products"

// productImportColumns 是导入模板的列定义（键/标签/示例）：导入中心与异步导出共用同一份，
// 这样导出的 .xlsx 表头天然就是导入模板的表头，导出的文件可以直接改完再导回去。
func productImportColumns() []importspec.Column {
	return []importspec.Column{
		{Key: "title", Label: "商品名称", LabelKey: "website.import_col_title", Required: true, Example: "18V 无刷冲击扳手"},
		{Key: "slug", Label: "Slug", LabelKey: "website.import_col_slug", Example: "18v-brushless-impact-wrench"},
		{Key: "categories", Label: "产品类目", LabelKey: "website.import_col_categories", Example: "电动工具|冲击扳手"},
		{Key: "status", Label: "状态", LabelKey: "website.import_col_status", Example: "draft"},
		{Key: "keywords", Label: "关键词", LabelKey: "website.import_col_keywords", Example: "impact wrench 18V brushless"},
		{Key: "summary", Label: "摘要", LabelKey: "website.import_col_summary", Example: "18V 无刷冲击扳手，最大扭矩 300N·m"},
		{Key: "content", Label: "详情", LabelKey: "website.import_col_content", Example: "<p>详情富文本 HTML</p>"},
		{Key: "coverUrl", Label: "主图", LabelKey: "website.import_col_cover", Example: "https://example.com/cover.jpg"},
		{Key: "images", Label: "图集", LabelKey: "website.import_col_images", Example: "https://example.com/1.jpg|https://example.com/2.jpg"},
		{Key: "videoUrl", Label: "视频地址", LabelKey: "website.import_col_video", Example: "https://example.com/demo.mp4"},
		{Key: "priceMin", Label: "价格（起）", LabelKey: "website.import_col_price_min", Example: "49.90"},
		{Key: "priceMax", Label: "价格（止）", LabelKey: "website.import_col_price_max", Example: "59.90"},
		{Key: "currency", Label: "币种", LabelKey: "website.import_col_currency", Example: "USD"},
		{Key: "priceUnit", Label: "计量单位", LabelKey: "website.import_col_unit", Example: "piece"},
		{Key: "moq", Label: "最小起订量", LabelKey: "website.import_col_moq", Example: "100"},
		{Key: "leadTimeDays", Label: "发货天数", LabelKey: "website.import_col_lead_time", Example: "15"},
		{Key: "port", Label: "装运港口", LabelKey: "website.import_col_port", Example: "Shanghai"},
		{Key: "paymentTerms", Label: "付款方式", LabelKey: "website.import_col_payment", Example: "T/T, L/C"},
		{Key: "specs", Label: "动态属性", LabelKey: "website.import_col_specs", Example: `[{"label":"Voltage","value":"18V"}] 或 电压:18V|品牌:NexTool`},
		{Key: "seoTitle", Label: "SEO 标题", LabelKey: "website.import_col_seo_title"},
		{Key: "seoDesc", Label: "SEO 描述", LabelKey: "website.import_col_seo_desc"},
		{Key: "seoKeywords", Label: "SEO 关键词", LabelKey: "website.import_col_seo_keywords"},
		{Key: "author", Label: "作者", LabelKey: "website.import_col_author", Example: "外贸部"},
		{Key: "i18n", Label: "多语言", LabelKey: "website.import_col_i18n", Example: `{"en":{"title":"18V Brushless Impact Wrench"}}`},
	}
}

// ImportSources 实现 importspec.Provider：导入中心启动时自动收集，无需第二处接线。
func (m *module) ImportSources(db *gorm.DB) []importspec.Source {
	svc := NewService(db)
	return []importspec.Source{{
		Name:     SourceProducts,
		Title:    "产品导入",
		TitleKey: "website.import_products_title",
		Columns:  productImportColumns(),
		Handler: func(ctx context.Context, _ int, row map[string]string) error {
			return importProductRow(ctx, svc, row)
		},
	}}
}

// importProductRow 把导入中心解析好的一行映射成 ProductForm 落库。
// 认行键是 slug：非空且已存在则更新该商品，否则新建（slug 留空按标题自动生成）。
// 状态默认草稿；SaveProduct 的语义照常生效——下架商品保存后仍是下架。
func importProductRow(ctx context.Context, svc *Service, row map[string]string) error {
	status := ProductDraft
	if raw := strings.TrimSpace(row["status"]); raw != "" {
		v, ok := productStatusAliases[strings.ToLower(raw)]
		if !ok {
			return rowError(ctx, response.BadRequest("website.import_status_invalid"))
		}
		status = v
	}
	specs, err := parseSpecsImport(row["specs"])
	if err != nil {
		return rowError(ctx, err)
	}
	priceMin, err := parseImportFloat(row["priceMin"])
	if err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	}
	priceMax, err := parseImportFloat(row["priceMax"])
	if err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	}
	moq, err := parseImportInt(row["moq"])
	if err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	}
	leadTimeDays, err := parseImportInt(row["leadTimeDays"])
	if err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	}
	catIDs, err := svc.importCategoryIDs(ctx, row["categories"])
	if err != nil {
		return rowError(ctx, err)
	}
	f := ProductForm{
		Slug:        row["slug"],
		Title:       row["title"],
		Keywords:    row["keywords"],
		Summary:     row["summary"],
		SeoTitle:    row["seoTitle"],
		SeoDesc:     row["seoDesc"],
		SeoKeywords: row["seoKeywords"],
		Content:     row["content"],
		Status:      status,
		CoverURL:    row["coverUrl"],
		Images:      splitImportList(row["images"]),
		VideoURL:    row["videoUrl"],
		PriceMin:    priceMin,
		PriceMax:    priceMax,
		Currency:    row["currency"],
		PriceUnit:   row["priceUnit"],
		Moq:         moq,
		LeadTimeDays: leadTimeDays,
		Port:         row["port"],
		PaymentTerms: row["paymentTerms"],
		Specs:        specs,
		CategoryIDs:  catIDs,
		I18nJSON:     row["i18n"],
		Author:       row["author"],
	}
	id, err := svc.productIDBySlug(ctx, f.Slug)
	if err != nil {
		return rowError(ctx, err)
	}
	_, err = svc.SaveProduct(ctx, id, f)
	return rowError(ctx, err)
}

// productStatusAliases 把导入表里的状态别名收敛到状态码：英文码为主，中文顺手认。
var productStatusAliases = map[string]string{
	"draft": ProductDraft, "草稿": ProductDraft,
	"online": ProductOnline, "上架": ProductOnline,
	"offline": ProductOffline, "下架": ProductOffline,
}

// splitImportList 拆竖线/分号/顿号连接的列表（图集、类目共用一套分隔符）。
func splitImportList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == '|' || r == ';' || r == '；' || r == '，' || r == '、' || r == '\n'
	})
}

// parseSpecsImport 解析动态属性列：首选 JSON 数组（导出/程序生成），也认「名称:值」
// 用竖线分隔的人写法。两种格式都解析不出来时整行失败，不猜。
func parseSpecsImport(raw string) ([]SpecItem, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "[") {
		var specs []SpecItem
		if err := json.Unmarshal([]byte(raw), &specs); err != nil {
			return nil, response.BadRequest("website.import_specs_invalid")
		}
		return specs, nil
	}
	var specs []SpecItem
	for _, part := range splitImportList(raw) {
		label, value, ok := strings.Cut(part, ":")
		if !ok {
			if label, value, ok = strings.Cut(part, "："); !ok {
				return nil, response.BadRequest("website.import_specs_invalid")
			}
		}
		specs = append(specs, SpecItem{Label: strings.TrimSpace(label), Value: strings.TrimSpace(value)})
	}
	return specs, nil
}

func parseImportFloat(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// parseImportInt 兼容 Excel 里 100.0 这类浮点形态：能无损转整就收，否则报错。
func parseImportInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if v, err := strconv.Atoi(raw); err == nil {
		return v, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f != float64(int(f)) {
		return 0, errors.New("not an integer")
	}
	return int(f), nil
}

// importCategoryIDs 把「类目名|类目名」映射成类目 ID：按名称大小写不敏感地认已有类目，
// 没有的当新类目建在顶级（导入是攒数据的主入口，替用户建类比整行失败友好）。
func (s *Service) importCategoryIDs(ctx context.Context, raw string) ([]idgen.ID, error) {
	names := splitImportList(raw)
	if len(names) == 0 {
		return nil, nil
	}
	var rows []ProductCategory
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	byName := make(map[string]idgen.ID, len(rows))
	for _, r := range rows {
		byName[strings.ToLower(strings.TrimSpace(r.Title))] = r.ID
	}
	ids := make([]idgen.ID, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if id, ok := byName[strings.ToLower(name)]; ok {
			ids = append(ids, id)
			continue
		}
		cat, err := s.SaveProductCategory(ctx, 0, ProductCategoryForm{Title: name})
		if err != nil {
			return nil, err
		}
		byName[strings.ToLower(name)] = cat.ID
		ids = append(ids, cat.ID)
	}
	return ids, nil
}

// productIDBySlug 按 slug 找已有商品：slug 列是导入的认行键。查库出错原样上抛，
// 没查到返回 0（新建）。
func (s *Service) productIDBySlug(ctx context.Context, slug string) (idgen.ID, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return 0, nil
	}
	var row Product
	if err := s.db.WithContext(ctx).Select("id").Where("slug = ?", slug).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.ID, nil
}

// rowError 把业务错误的 i18n key 翻成当前语言的句子——导入报告是给人看的，
// 不能把 website.product_title_required 这种 key 直接写进去（与 crm 的 import.go 同款）。
func rowError(ctx context.Context, err error) error {
	var re *response.Error
	if errors.As(err, &re) && re.Msg == "" && re.Key != "" {
		return errors.New(i18n.T(ctx, re.Key, re.Args...))
	}
	return err
}

// ---- 异步导出：大数据量不再同步阻塞请求，而是登记 core/job 任务，worker 流式写 xlsx 到存储，
// 用户在「导出中心」看进度与下载（与 system/user 的 user.export 同一套机制）。 ----

const (
	jobTypeProductExport = "website.product-export"
	// productExportMax 是单次异步导出的行数上限（任务内上限：xlsx 单表上限约 104 万行，留足余量）。
	productExportMax = 500000
	// productExportBatch 是每批读取行数。
	productExportBatch = 500
	// maxContentCellRunes 是详情列写进单元格的最大字符数（xlsx 单元格上限 32767，留出富余防写坏文件）。
	maxContentCellRunes = 20000
)

const productXlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// productExportParams 是任务参数：只含筛选条件与操作者，不含凭据。
// Status / CategoryIds 是逗号拼接多值（与 query 参数同一契约）；CategoryID 是旧单值，兼容在途任务。
type productExportParams struct {
	Keyword     string   `json:"keyword,omitempty"`
	Status      string   `json:"status,omitempty"`
	CategoryID  idgen.ID `json:"categoryId,omitempty"`
	CategoryIds string   `json:"categoryIds,omitempty"`
	IDs         []int64  `json:"ids,omitempty"`
	ActorID     int64    `json:"actorId"`
}

// query 把任务参数还原成列表查询：多值按 CSV 解析，旧单值兜底。
func (p productExportParams) query() ContentQuery {
	q := ContentQuery{Keyword: p.Keyword, Statuses: response.ParseList(p.Status, response.MaxFilterValues)}
	catRaw := p.CategoryIds
	if strings.TrimSpace(catRaw) == "" && p.CategoryID > 0 {
		catRaw = p.CategoryID.String()
	}
	if ids := response.ParseFilterIDs(catRaw, response.MaxFilterValues); len(ids) > 0 {
		q.CategoryIDs = make([]idgen.ID, 0, len(ids))
		for _, id := range ids {
			q.CategoryIDs = append(q.CategoryIDs, idgen.ID(id))
		}
	}
	return q
}

// productExportBody 是 POST /products/export-async 的请求体（与查询参数同形）。
type productExportBody struct {
	Keyword     string `json:"keyword"`
	Status      string `json:"status"`      // 逗号拼接多值
	CategoryId  string `json:"categoryId"`  // 兼容旧单值
	CategoryIds string `json:"categoryIds"` // 逗号拼接多值
	IDs         []string `json:"ids"`
}

// categoryIdCSV 把类目 id 列表转成任务参数里的字符串列表。
func categoryIdCSV(ids []idgen.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// exportProductTx 组出导出范围的查询：ids 优先（勾选行），否则按筛选条件。
func exportProductTx(tx *gorm.DB, q ContentQuery, ids []int64) *gorm.DB {
	if len(ids) > 0 {
		return tx.Where("id IN ?", ids)
	}
	return productTx(tx, q)
}

// productExportAsyncBody 把请求体解析成参数；ids 字符串转 int64。
func productExportAsyncBody(b productExportBody) (ContentQuery, []idgen.ID, error) {
	q := ContentQuery{
		Keyword:  strings.TrimSpace(b.Keyword),
		Statuses: response.ParseList(b.Status, response.MaxFilterValues),
	}
	catRaw := b.CategoryIds
	if strings.TrimSpace(catRaw) == "" {
		catRaw = b.CategoryId
	}
	if ids := response.ParseFilterIDs(catRaw, response.MaxFilterValues); len(ids) > 0 {
		q.CategoryIDs = make([]idgen.ID, 0, len(ids))
		for _, id := range ids {
			q.CategoryIDs = append(q.CategoryIDs, idgen.ID(id))
		}
	}
	var ids []idgen.ID
	for _, raw := range b.IDs {
		id, err := idgen.Parse(strings.TrimSpace(raw))
		if err != nil || id <= 0 {
			return q, nil, response.BadRequest("website.ids_invalid")
		}
		ids = append(ids, id)
	}
	return q, ids, nil
}

// productExportAsync POST /products/export-async：创建导出任务并立即返回任务 ID。
func (h *handler) productExportAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("website.export_center_disabled"))
		return
	}
	var b productExportBody
	if !response.BindJSON(c, &b) {
		return
	}
	q, ids, err := productExportAsyncBody(b)
	if err != nil {
		response.Fail(c, err)
		return
	}
	var owner idgen.ID
	if cl := auth.ClaimsFrom(c); cl != nil {
		owner = cl.UserID
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{
		Type:  jobTypeProductExport,
		Owner: owner,
		Params: productExportParams{
			Keyword: q.Keyword, Status: strings.Join(q.Statuses, ","), CategoryIds: strings.Join(categoryIdCSV(q.CategoryIDs), ","),
			IDs: int64IDs(ids), ActorID: int64(owner),
		},
	})
	switch {
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(429, "error.too_many"))
		return
	case err != nil:
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
}

// RunProductExportJob 是 website.product-export 的任务处理器：流式分批写 xlsx，逐批汇报进度。
// 数据范围以任务所有者在执行时的权限为准已由提交时的权限点把守（website:product）；这里只做数据装配。
func (s *Service) RunProductExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p productExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	q := p.query()
	tx := exportProductTx(s.db.WithContext(ctx).Model(&Product{}), q, p.IDs)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	total = min(total, productExportMax)
	report(0, fmt.Sprintf("共 %d 行", total))

	name := fmt.Sprintf("products-%s.xlsx", time.Now().Format("20060102"))
	return job.WriteResult(ctx, j, name, productXlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		cols := productImportColumns()
		headers := make([]string, len(cols))
		for i, c := range cols {
			headers[i] = c.Label
		}
		sh, err := x.NewSheet("产品", excel.SheetOptions{Header: headers})
		if err != nil {
			return err
		}
		var done int64
		last := int64(math.MaxInt64)
		for done < total {
			if err := ctx.Err(); err != nil {
				return err
			}
			var rows []Product
			if err := tx.Where("id < ?", last).Order("id DESC").
				Preload("Categories").Limit(int(min(productExportBatch, total-done))).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for i := range rows {
				if err := writeProductExportRow(sh, &rows[i]); err != nil {
					return err
				}
			}
			done += int64(len(rows))
			last = int64(rows[len(rows)-1].ID)
			report(int(done*100/max(total, 1)), fmt.Sprintf("已导出 %d / %d 行", done, total))
		}
		return x.Close()
	})
}

// writeProductExportRow 写一行导出数据：列序与 productImportColumns 一致，单元格格式
// （类目/图集竖线分隔、specs 与多语言 JSON）与导入模板同款，导出的文件可以直接改完再导回去。
func writeProductExportRow(sh *excel.Sheet, p *Product) error {
	specs := ""
	if p.SpecsJSON != "" {
		specs = p.SpecsJSON
	}
	i18n := p.I18nJSON
	cats := make([]string, 0, len(p.Categories))
	for _, c := range p.Categories {
		cats = append(cats, c.Title)
	}
	return sh.WriteRow(
		p.Title, p.Slug, strings.Join(cats, "|"), p.Status, p.Keywords, p.Summary,
		trimRunes(p.Content, maxContentCellRunes), p.CoverURL,
		strings.Join(safeImageList(p.ImagesJSON), "|"), p.VideoURL,
		p.PriceMin, p.PriceMax, p.Currency, p.PriceUnit, p.Moq, p.LeadTimeDays,
		p.Port, p.PaymentTerms, specs, p.SeoTitle, p.SeoDesc, p.SeoKeywords, p.Author, i18n,
	)
}

// safeImageList 解图集 JSON 文本列；坏 JSON 当空列表（导出不该因为一行脏数据失败）。
func safeImageList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
