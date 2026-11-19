package website

// 常见问题（FAQ）：首页板块的问答条目 CRUD，与 Feature 卡片同一范式（平台级、
// 排序展示、enabled 关掉即整条隐藏）。答案允许换行，公开页按段落渲染。

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
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
		tx = tx.Where("LOWER(question) LIKE LOWER(?) OR LOWER(answer) LIKE LOWER(?)", like, like)
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
func (s *Service) PublicFaqs(ctx context.Context) []Faq {
	var rows []Faq
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("sort, id DESC").Find(&rows).Error; err != nil {
		return nil
	}
	return rows
}

// FaqForm 是 FAQ 编辑表单。
type FaqForm struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Sort     int    `json:"sort"`
	Enabled  bool   `json:"enabled"`
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
	row.Question = f.Question
	row.Answer = strings.TrimSpace(f.Answer)
	row.Sort = f.Sort
	row.Enabled = f.Enabled
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
