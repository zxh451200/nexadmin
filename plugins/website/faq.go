package website

// 常见问题（FAQ）：首页板块的问答条目 CRUD，与 Feature 卡片同一范式（平台级、
// 排序展示、enabled 关掉即整条隐藏）。答案允许换行，公开页按段落渲染。

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
	pi18n "nexadmin/pkg/i18n"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/importspec"
)

// FaqQuery 是管理端 FAQ 列表查询。Enabled 为空字符串 = 全部，否则 "true"/"false"。
type FaqQuery struct {
	Keyword  string
	Enabled  string
	Sort     string
	Desc     bool
	Page     int
	PageSize int
}

// faqFilter 是列表与导出共用的筛选，保证导出的文件与页面看到的列表同口径。
func faqFilter(tx *gorm.DB, q FaqQuery) *gorm.DB {
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("LOWER(question) LIKE LOWER(?) OR LOWER(answer) LIKE LOWER(?) OR LOWER(COALESCE(i18n_json, '')) LIKE LOWER(?)", like, like, like)
	}
	if q.Enabled == "true" {
		tx = tx.Where("enabled = ?", true)
	} else if q.Enabled == "false" {
		tx = tx.Where("enabled = ?", false)
	}
	return tx
}

// ListFaqs 管理端列表（分页 + 关键词/启用过滤 + 表头排序）；默认按 sort、id 排序。
func (s *Service) ListFaqs(ctx context.Context, q FaqQuery) ([]Faq, int64, error) {
	tx := faqFilter(s.db.WithContext(ctx).Model(&Faq{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Faq
	if err := tx.Order(contentOrder(q.Sort, q.Desc, map[string]string{
		"question":   "question",
		"sort":       "sort",
		"enabled":    "enabled",
		"created_at": "created_at",
	}, "sort ASC, id DESC")).Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// PublicFaqs 公开页只取启用的（sort 排序），渲染 FAQ 板块。
// 非默认语言只返回真正录过译文的条目：FAQ 是营销内容，宁可少几条也不要把中文问答
// 摆到英文站上；默认语言仍展示全部（含未翻译的原文）。
func (s *Service) PublicFaqs(ctx context.Context, lang string) []Faq {
	var rows []Faq
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id DESC").Find(&rows).Error; err != nil {
		return nil
	}
	out := rows[:0]
	for i := range rows {
		if lang != defaultLang.Code && !rows[i].translated(lang) {
			continue
		}
		rows[i].localize(lang)
		out = append(out, rows[i])
	}
	return out
}

// FaqForm 是 FAQ 编辑表单。
type FaqForm struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Sort     int    `json:"sort"`
	Enabled  bool   `json:"enabled"`
	I18nJSON string `json:"i18nJson"`
}

// SaveFaq 创建（id=0）或更新一条 FAQ；问题必填，答案限长。
func (s *Service) SaveFaq(ctx context.Context, id idgen.ID, f FaqForm) (*Faq, error) {
	f.Question = strings.TrimSpace(f.Question)
	if f.Question == "" {
		return nil, response.BadRequest("website.faq_question_required")
	}
	if utf8Len(f.Question) > 300 {
		return nil, response.BadRequest("website.faq_question_too_long")
	}
	if utf8Len(f.Answer) > 5000 {
		return nil, response.BadRequest("website.faq_answer_too_long")
	}
	var row Faq
	if id != 0 {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", int64(id)).Error; err != nil {
			return nil, response.NotFound("website.faq_not_found")
		}
	} else {
		row = Faq{}
	}
	i18n, err := pi18n.Normalize(f.I18nJSON, siteLocales{}, faqI18nFields)
	if err != nil {
		return nil, response.BadRequest("website.i18n_invalid").Wrap(err)
	}
	row.Question = f.Question
	row.Answer = strings.TrimSpace(f.Answer)
	row.Sort = f.Sort
	row.Enabled = f.Enabled
	row.I18nJSON = i18n
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteFaq 删除一条 FAQ。
func (s *Service) DeleteFaq(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Delete(&Faq{}, "id = ?", int64(id)).Error
}

// DeleteFaqs 批量删除 FAQ，返回实际删除条数。
func (s *Service) DeleteFaqs(ctx context.Context, ids []idgen.ID) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.faq_ids_empty")
	}
	res := s.db.WithContext(ctx).Delete(&Faq{}, "id IN ?", int64IDs(ids))
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("website.faq_not_found")
	}
	return res.RowsAffected, nil
}

// utf8Len 是字符数（区别于字节），FAQ 限长按字符算。
func utf8Len(s string) int { return len([]rune(s)) }

// ReorderFaqs 把传入顺序写回 sort（=序号×10）：拖拽排序的落库。事务内逐条更新，
// 有 id 不存在即整批拒绝——并发删除下不给「成功但少改」的假象（与 ReorderNavs 同口径）。
func (s *Service) ReorderFaqs(ctx context.Context, ids []idgen.ID) error {
	if len(ids) == 0 {
		return response.BadRequest("website.faq_ids_empty")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			res := tx.Model(&Faq{}).Where("id = ?", int64(id)).Update("sort", (i+1)*10)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return response.NotFound("website.faq_not_found")
			}
		}
		return nil
	})
}

// ---- 导入：接入通用导入中心（importspec.Provider），与产品导入同一机制 ----

// SourceFaqs 是常见问题导入源在导入中心台账里的标识。
const SourceFaqs = "website_faqs"

// faqImportColumns 是导入模板的列定义：列名与导出表头一致（问题/答案/排序/状态）。
// 导出的 xlsx 末尾多一列「更新时间」，导入中心会忽略没登记的列——导出的文件可以直接改完再导回。
func faqImportColumns() []importspec.Column {
	return []importspec.Column{
		{Key: "question", Label: "问题", LabelKey: "website.import_faq_question", Required: true, Example: "支持哪些付款方式？"},
		{Key: "answer", Label: "答案", LabelKey: "website.import_faq_answer", Example: "支持 T/T、PayPal 等方式"},
		{Key: "sort", Label: "排序", LabelKey: "website.import_faq_sort", Example: "10"},
		{Key: "status", Label: "状态", LabelKey: "website.import_faq_status", Example: "展示中"},
	}
}

// faqEnabledAliases 把导入表里的状态别名收敛到 enabled：导出用词为主，常见布尔写法顺手认。
var faqEnabledAliases = map[string]bool{
	"展示中": true, "启用": true, "开启": true, "是": true, "true": true, "on": true, "enabled": true, "yes": true,
	"已停用": false, "停用": false, "隐藏": false, "否": false, "false": false, "off": false, "disabled": false, "no": false,
}

// importFaqRow 把导入中心解析好的一行映射成 FAQ 落库。认行键是问题文本：完全相同
// （去空白）的问题更新既有条目，否则新增。状态留空按启用；排序留空的新行接在当前
// 最大排序之后——全部挤在 0 会顶着「sort 升序」的默认排序插到最前面。
func (s *Service) importFaqRow(ctx context.Context, row map[string]string) error {
	question := strings.TrimSpace(row["question"])
	if question == "" {
		return rowError(ctx, response.BadRequest("website.faq_question_required"))
	}
	sort, err := parseImportInt(row["sort"])
	if err != nil {
		return rowError(ctx, response.BadRequest("website.import_number_invalid"))
	}
	enabled := true
	if raw := strings.ToLower(strings.TrimSpace(row["status"])); raw != "" {
		v, ok := faqEnabledAliases[raw]
		if !ok {
			return rowError(ctx, response.BadRequest("website.faq_import_status_invalid"))
		}
		enabled = v
	}
	id, err := s.faqIDByQuestion(ctx, question)
	if err != nil {
		return rowError(ctx, err)
	}
	if id == 0 && sort == 0 {
		var maxSort int
		if err := s.db.WithContext(ctx).Model(&Faq{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return rowError(ctx, err)
		}
		sort = maxSort + 10
	}
	// 导入只覆盖模板里的列：已有行按问题匹配时保留译文，不能因为重导一次答案就把译文洗掉。
	i18n := ""
	if id != 0 {
		var exist Faq
		if err := s.db.WithContext(ctx).First(&exist, "id = ?", int64(id)).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return rowError(ctx, err)
			}
		} else {
			i18n = exist.I18nJSON
		}
	}
	_, err = s.SaveFaq(ctx, id, FaqForm{Question: question, Answer: row["answer"], Sort: sort, Enabled: enabled, I18nJSON: i18n})
	return rowError(ctx, err)
}

// faqIDByQuestion 按问题文本（精确、去空白）找已有 FAQ：查库出错原样上抛，没查到返回 0（新建）。
func (s *Service) faqIDByQuestion(ctx context.Context, question string) (idgen.ID, error) {
	var row Faq
	if err := s.db.WithContext(ctx).Select("id").Where("question = ?", question).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return row.ID, nil
}

// ---- HTTP 层 ----

// GET /plugins/website/faqs —— 管理端列表（分页/关键词/启用状态/排序）。
func (h *handler) listFaqs(c *gin.Context) {
	page, size := response.ParsePage(c)
	sortField, desc := response.ParseSort(c)
	q := FaqQuery{Page: page, PageSize: size, Keyword: c.Query("keyword"), Enabled: c.Query("enabled"), Sort: sortField, Desc: desc}
	if q.Enabled != "" && q.Enabled != "true" && q.Enabled != "false" {
		response.Fail(c, response.BadRequest("website.faq_enabled_invalid"))
		return
	}
	list, total, err := h.svc.ListFaqs(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// DELETE /plugins/website/faqs?ids=1,2,3 —— 批量删除 FAQ。
func (h *handler) deleteFaqs(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteFaqs(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// POST /plugins/website/faqs
func (h *handler) createFaq(c *gin.Context) {
	var f FaqForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveFaq(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/faqs/:id
func (h *handler) updateFaq(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f FaqForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveFaq(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/faqs/:id
func (h *handler) deleteFaq(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteFaq(c.Request.Context(), id))
}

// FaqReorderForm 是拖拽排序请求体：ids 按目标顺序排列。
type FaqReorderForm struct {
	IDs []idgen.ID `json:"ids"`
}

// PUT /plugins/website/faqs/reorder
func (h *handler) reorderFaqs(c *gin.Context) {
	var f FaqReorderForm
	if !response.BindJSON(c, &f) {
		return
	}
	respond(c, nil, h.svc.ReorderFaqs(c.Request.Context(), f.IDs))
}
