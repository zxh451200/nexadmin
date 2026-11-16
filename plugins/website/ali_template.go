package website

// ali_template.go 是「与阿里国际站批量模板互通」的两条通道：
//
//   - 导入：新增一个导入源「阿里模板导入」，表头按 plg_website_ali_column 的映射认（含别名），
//     映射表之外的列（阿里按类目下载模板时附带的一串类目属性列）整列收进动态属性 specs，
//     换类目不用改代码；
//   - 导出：website.ali-template-export 任务按同一张映射表出 xlsx，列名与列序就是阿里模板的形态，
//     人可以直接拿去后台批量上传，也能再导回本站。
//
// 映射表是**数据不是代码**：阿里模板的真实列名要拿你们账号里的两份文件（导出件 + 按类目下载的模板）
// 校对，改表里的行即可，不用发版。默认行的依据是官方《【新接口】商品发布接入文档》里各 field 的
// name 属性（Product name / Product Keyword / FOB Price / Unit / MOQ / Shipping / Shipping port /
// Payment Options / Product Photo / Custom Description），不是凭印象编的列名。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/importspec"
)

// AliColumn 是「阿里模板列名 ↔ 本站字段」的一行映射。
type AliColumn struct {
	database.PlatformModel
	// AliHeader 是阿里模板里的表头（导出时按它出列，导入时按它精确匹配）。
	AliHeader string `gorm:"column:ali_header;size:100;not null;uniqueIndex:uk_plg_website_ali_column_header" json:"aliHeader"`
	// OurKey 是本站字段键，取值见 aliOurKeys；attr 表示落到动态属性（一般不需要显式写，未映射列会自动落 specs）。
	OurKey string `gorm:"column:our_key;size:64;not null" json:"ourKey"`
	// Aliases 是同一列的其它写法（逗号分隔）：中英模板、历史版本表头都收在这里。
	Aliases string `gorm:"column:aliases;size:500;not null;default:''" json:"aliases"`
	// Required 只影响导出提示与导入预检，不改变落库规则。
	Required bool   `gorm:"column:required;not null;default:false" json:"required"`
	Sort     int    `gorm:"column:sort;not null;default:0" json:"sort"`
	Note     string `gorm:"size:200;not null;default:''" json:"note"`
}

func (AliColumn) TableName() string { return "plg_website_ali_column" }

// aliExtraKey 是未映射列（类目属性等）在导入行里的收纳键。
const aliExtraKey = "__ali_extra__"

// SourceProductsAli 是「阿里模板导入」在导入中心台账里的标识。
const SourceProductsAli = "website_products_ali"

// aliOurKeys 是映射表允许的本站字段键（写错键名保存会被拒，避免映射到一个不存在的列）。
var aliOurKeys = map[string]bool{
	"alibabaProductId": true, "alibabaCategoryId": true, "title": true, "keywords": true,
	"summary": true, "content": true, "categories": true, "status": true,
	"coverUrl": true, "images": true, "videoUrl": true, "priceMin": true, "priceMax": true,
	"currency": true, "priceUnit": true, "moq": true, "leadTimeDays": true, "port": true,
	"paymentTerms": true, "specs": true, "author": true,
}

// defaultAliColumns 是出厂映射（依据见文件头注释）。
func defaultAliColumns() []AliColumn {
	row := func(header, key, aliases string, required bool, note string) AliColumn {
		return AliColumn{AliHeader: header, OurKey: key, Aliases: aliases, Required: required, Note: note}
	}
	out := []AliColumn{
		row("Product ID", "alibabaProductId", "商品ID,Product Id,ali_product_id", false, "阿里侧商品 ID：有它再导入就是更新，不是新建"),
		row("Category ID", "alibabaCategoryId", "类目ID,Leaf Category Id", false, "阿里叶子类目 ID（同步/预览要用）"),
		row("Product name", "title", "商品名称,Product Name", true, "阿里限 128 字节"),
		row("Product Keyword", "keywords", "关键词,Keywords,Product Keywords", false, "逗号分隔的短语，最多 3 个（如 energy storage cabinet, 215kwh battery）"),
		row("Category", "categories", "商品类目,类目,Category Name", false, "本站类目名，多个用 | 分隔"),
		row("Status", "status", "状态,Product Status", false, "draft / online / offline"),
		row("Product Photo", "coverUrl", "主图,Main Image", false, "图片 URL；阿里侧发品要求图片银行地址"),
		row("More Photos", "images", "图集,Images,Image URLs", false, "多个用 | 分隔"),
		row("Video", "videoUrl", "视频,Product Video", false, ""),
		row("FOB Price", "priceMin", "价格（起）,Price Range,Price", false, "区间下界"),
		row("Max Price", "priceMax", "价格（止）,Price To", false, "区间上界"),
		row("Currency", "currency", "币种", false, "默认 USD"),
		row("Unit", "priceUnit", "计量单位,Unit of Measure,Price Unit", false, "piece / set …"),
		row("MOQ", "moq", "最小起订量,Min Order Quantity,Minimum Order", false, ""),
		row("Shipping", "leadTimeDays", "发货期,Delivery Time,发货天数", false, "天数"),
		row("Shipping port", "port", "装运港口,Port", false, ""),
		row("Payment Options", "paymentTerms", "付款方式,Payment Terms", false, "T/T, L/C …"),
		row("More Details", "specs", "动态属性,产品属性", false, "JSON 或「名称:值」；未映射的属性列也会自动落到这里"),
	}
	for i := range out {
		out[i].Sort = (i + 1) * 10
	}
	return out
}

// ensureAliColumns 幂等补齐出厂映射：只插缺的表头，已存在的行（含管理员改过的）一律不动。
func (s *Service) ensureAliColumns(ctx context.Context) error {
	var have []string
	if err := s.db.WithContext(ctx).Model(&AliColumn{}).Pluck("ali_header", &have).Error; err != nil {
		return err
	}
	known := make(map[string]bool, len(have))
	for _, h := range have {
		known[h] = true
	}
	for _, c := range defaultAliColumns() {
		if known[c.AliHeader] {
			continue
		}
		now := time.Now().UTC()
		c.CreatedAt = now
		c.UpdatedAt = now
		if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
			return err
		}
	}
	return nil
}

// aliColumns 读当前映射（按 Sort、表头排序）。
func (s *Service) aliColumns(ctx context.Context) ([]AliColumn, error) {
	var rows []AliColumn
	err := s.db.WithContext(ctx).Order("sort ASC, ali_header ASC").Find(&rows).Error
	return rows, err
}

// aliColumnMap 是「阿里表头 → 本站键」的索引（含别名，键已去空白）。
type aliColumnMap struct {
	byHeader map[string]string
	order    []AliColumn
}

func buildAliColumnMap(rows []AliColumn) aliColumnMap {
	m := aliColumnMap{byHeader: make(map[string]string, len(rows)*2), order: rows}
	for _, r := range rows {
		if h := strings.TrimSpace(r.AliHeader); h != "" {
			m.byHeader[h] = r.OurKey
		}
		for _, a := range strings.Split(r.Aliases, ",") {
			if a = strings.TrimSpace(a); a != "" {
				m.byHeader[a] = r.OurKey
			}
		}
	}
	return m
}

// AliColumnInput 是映射行的保存入参。
type AliColumnInput struct {
	AliHeader string `json:"aliHeader"`
	OurKey    string `json:"ourKey"`
	Aliases   string `json:"aliases"`
	Required  bool   `json:"required"`
	Sort      int    `json:"sort"`
	Note      string `json:"note"`
}

// SaveAliColumn 新增或更新一行映射；表头重复与未知本站键都拒绝。
func (s *Service) SaveAliColumn(ctx context.Context, id idgen.ID, in AliColumnInput) (*AliColumn, error) {
	in.AliHeader = strings.TrimSpace(in.AliHeader)
	in.OurKey = strings.TrimSpace(in.OurKey)
	if in.AliHeader == "" || len([]rune(in.AliHeader)) > 100 {
		return nil, response.BadRequest("website.ali_column_header_required")
	}
	if !aliOurKeys[in.OurKey] {
		return nil, response.BadRequest("website.ali_column_key_unknown", in.OurKey)
	}
	var dup int64
	q := s.db.WithContext(ctx).Model(&AliColumn{}).Where("ali_header = ?", in.AliHeader)
	if id > 0 {
		q = q.Where("id <> ?", int64(id))
	}
	if err := q.Count(&dup).Error; err != nil {
		return nil, err
	}
	if dup > 0 {
		return nil, response.Conflict("website.ali_column_header_exists")
	}
	row := AliColumn{
		AliHeader: in.AliHeader, OurKey: in.OurKey, Aliases: strings.TrimSpace(in.Aliases),
		Required: in.Required, Sort: in.Sort, Note: strings.TrimSpace(in.Note),
	}
	if id > 0 {
		if err := s.db.WithContext(ctx).Model(&AliColumn{}).Where("id = ?", int64(id)).Updates(map[string]any{
			"ali_header": row.AliHeader, "our_key": row.OurKey, "aliases": row.Aliases,
			"required": row.Required, "sort": row.Sort, "note": row.Note, "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return nil, err
		}
	} else if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	if id > 0 {
		var out AliColumn
		if err := s.db.WithContext(ctx).First(&out, "id = ?", int64(id)).Error; err != nil {
			return nil, err
		}
		return &out, nil
	}
	return &row, nil
}

// DeleteAliColumn 删除一行映射（出厂行也允许删：真实模板里可能就是没有这一列）。
func (s *Service) DeleteAliColumn(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&AliColumn{}, "id = ?", int64(id)).Error
}

// aliImportColumns 给导入中心生成列定义：表头用阿里的列名，别名交给解析器匹配。
// 读不到映射表（迁移未落、库异常、表被清空）时返回 nil，由注册时的出厂映射兜底。
func (s *Service) aliImportColumns(ctx context.Context) []importspec.Column {
	rows, err := s.aliColumns(ctx)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return aliColumnsOf(rows)
}

// aliColumnsOf 把映射行翻成导入中心的列定义。
func aliColumnsOf(rows []AliColumn) []importspec.Column {
	out := make([]importspec.Column, 0, len(rows))
	for _, r := range rows {
		out = append(out, importspec.Column{
			Key: r.OurKey, Label: r.AliHeader, Aliases: splitAliasList(r.Aliases),
			Required: r.Required, Example: aliExampleFor(r.OurKey),
		})
	}
	return out
}

// aliColumnsFromDefaults 是注册时的兜底列定义（出厂映射）。
func aliColumnsFromDefaults() []importspec.Column {
	return aliColumnsOf(defaultAliColumns())
}

func splitAliasList(raw string) []string {
	var out []string
	for _, a := range strings.Split(raw, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

func aliExampleFor(key string) string {
	switch key {
	case "status":
		return "online"
	case "alibabaProductId", "alibabaCategoryId":
		return "1600000000000"
	case "keywords":
		return "215kwh battery cabinet, energy storage system"
	case "coverUrl", "images", "videoUrl":
		return "https://sc01.alicdn.com/kf/demo.jpg"
	case "priceMin", "priceMax":
		return "1200.00"
	case "moq":
		return "1"
	case "leadTimeDays":
		return "25"
	case "port":
		return "Shanghai"
	case "paymentTerms":
		return "T/T, L/C"
	case "priceUnit":
		return "Set/Sets"
	case "currency":
		return "USD"
	case "categories":
		return "储能系统"
	case "specs":
		return `[{"label":"Model Number","value":"SOLARIS-215"}]`
	default:
		return ""
	}
}

// ImportAliRow 处理「阿里模板导入」的一行。
// 落库语义与商品导入一致：认 Product ID 命中就更新，否则新建；
// 未映射的列（类目属性、物流列等）整列并进动态属性 specs，不丢数据。
// 表头 → 本站键的匹配由导入中心按 AliImportColumns 的映射完成，这里只消费键。
func (s *Service) ImportAliRow(ctx context.Context, row map[string]string) error {
	f := ProductForm{
		Title: row["title"], Keywords: row["keywords"], Summary: row["summary"], Content: row["content"],
		CoverURL: row["coverUrl"], VideoURL: row["videoUrl"], Currency: row["currency"],
		PriceUnit: row["priceUnit"], Port: row["port"], PaymentTerms: row["paymentTerms"], Author: row["author"],
		Images: splitImportList(row["images"]),
	}
	if f.Title == "" {
		return rowError(ctx, response.BadRequest("website.product_title_required"))
	}
	if v, err := parseImportFloat(row["priceMin"]); err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	} else {
		f.PriceMin = v
	}
	if v, err := parseImportFloat(row["priceMax"]); err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	} else {
		f.PriceMax = v
	}
	if v, err := parseImportInt(row["moq"]); err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	} else {
		f.Moq = v
	}
	if v, err := parseImportInt(row["leadTimeDays"]); err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	} else {
		f.LeadTimeDays = v
	}
	status := ProductDraft
	if raw := strings.TrimSpace(row["status"]); raw != "" {
		v, ok := productStatusAliases[strings.ToLower(raw)]
		if !ok {
			return rowError(ctx, response.BadRequest("website.import_status_invalid"))
		}
		status = v
	}
	f.Status = status
	if ids, err := s.importCategoryIDs(ctx, row["categories"]); err != nil {
		return rowError(ctx, err)
	} else {
		f.CategoryIDs = ids
	}
	specs, err := parseSpecsImport(row["specs"])
	if err != nil {
		return rowError(ctx, err)
	}
	// 未映射列 → 动态属性：阿里按类目模板带出来的属性列、包装/供货等，先原样收着，
	// 等 B 阶段有专属字段再迁走（数据不丢比归类漂亮优先）。
	specs = append(specs, aliExtraSpecs(row[aliExtraKey])...)
	f.Specs = specs

	id, err := s.productIDByAliID(ctx, row["alibabaProductId"])
	if err != nil {
		return rowError(ctx, err)
	}
	saved, err := s.SaveProduct(ctx, id, f)
	if err != nil {
		return rowError(ctx, err)
	}
	// 阿里侧的两个 ID 不在 ProductForm 里（不是商品编辑面能改的字段），单独回写。
	updates := map[string]any{}
	if v := strings.TrimSpace(row["alibabaProductId"]); v != "" {
		updates["alibaba_product_id"] = trimLen(v, 64)
	}
	if v := strings.TrimSpace(row["alibabaCategoryId"]); v != "" {
		updates["alibaba_category_id"] = trimLen(v, 32)
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&Product{}).Where("id = ?", int64(saved.ID)).Updates(updates).Error; err != nil {
			return rowError(ctx, err)
		}
	}
	return nil
}

// productIDByAliID 按阿里商品 ID 找本地商品（导入幂等的关键：同一份文件重复导入只更新一行）。
func (s *Service) productIDByAliID(ctx context.Context, aliID string) (idgen.ID, error) {
	aliID = strings.TrimSpace(aliID)
	if aliID == "" {
		return 0, nil
	}
	var out Product
	err := s.db.WithContext(ctx).Where("alibaba_product_id = ?", aliID).Order("id ASC").First(&out).Error
	if err == gorm.ErrRecordNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return out.ID, nil
}

// aliExtraSpecs 把未映射列（JSON 对象文本）转成动态属性。
func aliExtraSpecs(raw string) []SpecItem {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	// 属性顺序要稳定：同一份文件重复导入产出的 specs 顺序一致，导出与预览才不会来回变
	sort.Strings(names)
	out := make([]SpecItem, 0, len(names))
	for _, k := range names {
		if v := strings.TrimSpace(m[k]); v != "" {
			out = append(out, SpecItem{Label: strings.TrimSpace(k), Value: v})
		}
	}
	return out
}

// aliExportHeader 是导出表头（列名 + 顺序都来自映射表）。
type aliExportHeader struct {
	Column AliColumn
	Key    string
}

// aliExportColumns 组装导出的表头与取值顺序。
func aliExportColumns(rows []AliColumn) []aliExportHeader {
	out := make([]aliExportHeader, 0, len(rows))
	for _, r := range rows {
		out = append(out, aliExportHeader{Column: r, Key: r.OurKey})
	}
	return out
}

// aliCellText 取一行商品在某个本站键上的导出值。
func aliCellText(p *Product, key string) string {
	switch key {
	case "alibabaProductId":
		return p.AlibabaProductID
	case "alibabaCategoryId":
		return p.AlibabaCategoryID
	case "title":
		return p.Title
	case "keywords":
		return p.Keywords
	case "summary":
		return p.Summary
	case "content":
		return trimRunes(p.Content, maxContentCellRunes)
	case "categories":
		names := make([]string, 0, len(p.Categories))
		for _, c := range p.Categories {
			names = append(names, c.Title)
		}
		return strings.Join(names, "|")
	case "status":
		return p.Status
	case "coverUrl":
		return p.CoverURL
	case "images":
		return strings.Join(safeImageList(p.ImagesJSON), "|")
	case "videoUrl":
		return p.VideoURL
	case "priceMin":
		return moneyOrEmpty(p.PriceMin)
	case "priceMax":
		return moneyOrEmpty(p.PriceMax)
	case "currency":
		return p.Currency
	case "priceUnit":
		return p.PriceUnit
	case "moq":
		return intOrEmpty(p.Moq)
	case "leadTimeDays":
		return intOrEmpty(p.LeadTimeDays)
	case "port":
		return p.Port
	case "paymentTerms":
		return p.PaymentTerms
	case "specs":
		return p.SpecsJSON
	case "author":
		return p.Author
	}
	return ""
}

func moneyOrEmpty(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f", v)
}

func intOrEmpty(v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprint(v)
}

// ---- 阿里模板导出 ----

const (
	jobTypeAliTemplateExport = "website.ali-template-export"
	// aliExportMax 与商品导出同一上限：模板是给人上传用的，超出阿里也收不下。
	aliExportMax = 50000
)

// aliExportParams 是导出任务参数：筛选条件与操作者（与商品导出同契约）。
type aliExportParams struct {
	Keyword     string  `json:"keyword,omitempty"`
	Status      string  `json:"status,omitempty"`
	CategoryIds string  `json:"categoryIds,omitempty"`
	IDs         []int64 `json:"ids,omitempty"`
	ActorID     int64   `json:"actorId"`
}

// RunAliTemplateExportJob 按映射表出 xlsx：表头与列序就是阿里批量模板的形态，
// 人可以直接拿去后台上传，也能再导回本站（Product ID 列命中即更新）。
func (s *Service) RunAliTemplateExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p aliExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	cols, err := s.aliColumns(ctx)
	if err != nil {
		return job.ResultFile{}, err
	}
	if len(cols) == 0 {
		return job.ResultFile{}, job.Errorf("列名映射表是空的：请先在「阿里列名映射」里补齐列")
	}
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.AliHeader
	}

	q := ContentQuery{Keyword: p.Keyword, Statuses: response.ParseList(p.Status, response.MaxFilterValues)}
	if ids := response.ParseFilterIDs(p.CategoryIds, response.MaxFilterValues); len(ids) > 0 {
		q.CategoryIDs = make([]idgen.ID, 0, len(ids))
		for _, id := range ids {
			q.CategoryIDs = append(q.CategoryIDs, idgen.ID(id))
		}
	}
	tx := exportProductTx(s.db.WithContext(ctx).Model(&Product{}), q, p.IDs)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	total = min(total, aliExportMax)
	report(0, fmt.Sprintf("共 %d 行", total))

	name := fmt.Sprintf("ali-template-%s.xlsx", time.Now().Format("20060102"))
	return job.WriteResult(ctx, j, name, productXlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("Products", excel.SheetOptions{Header: headers})
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
				cells := make([]any, len(cols))
				for k, c := range cols {
					cells[k] = aliCellText(&rows[i], c.OurKey)
				}
				if err := sh.WriteRow(cells...); err != nil {
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

// SubmitAliTemplateExport 提交导出任务（与商品导出同一入口形态：筛选条件或勾选的 ID）。
func (s *Service) SubmitAliTemplateExport(ctx context.Context, b productExportBody, owner idgen.ID) (idgen.ID, error) {
	q, ids, err := productExportAsyncBody(b)
	if err != nil {
		return 0, err
	}
	m := job.Default()
	if m == nil || !m.Ready() {
		return 0, response.BadRequest("website.export_center_disabled")
	}
	j, err := m.Submit(ctx, job.Submit{
		Type:  jobTypeAliTemplateExport,
		Owner: owner,
		Params: aliExportParams{
			Keyword: q.Keyword, Status: strings.Join(q.Statuses, ","),
			CategoryIds: strings.Join(categoryIdCSV(q.CategoryIDs), ","),
			IDs:         int64IDs(ids), ActorID: int64(owner),
		},
	})
	if err != nil {
		return 0, err
	}
	return j.ID, nil
}
