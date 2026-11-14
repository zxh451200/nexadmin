package website

// content_export.go 是新闻 / 留言的异步导出：与商品导出（website.product-export）同一套机制，
// 无论数据量大小都提交 core/job 任务进导出中心，worker 流式写 xlsx；同步直出端点已下线。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

const (
	jobTypePostExport    = "website.post-export"
	jobTypeMessageExport = "website.message-export"
	// contentExportMax 是单次导出的行数上限（任务内上限：超过整个任务失败并提示缩小范围）。
	contentExportMax = 50000
	xlsxContentType  = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// postExportParams 是新闻导出任务参数：筛选条件与操作者（与列表 query 参数同契约，多值逗号拼接）。
type postExportParams struct {
	Keyword     string   `json:"keyword,omitempty"`
	Status      string   `json:"status,omitempty"`
	CategoryIds string   `json:"categoryIds,omitempty"`
	ActorID     int64    `json:"actorId"`
}

func (p postExportParams) query() ContentQuery {
	return ContentQuery{
		Keyword:     p.Keyword,
		Statuses:    response.ParseList(p.Status, response.MaxFilterValues),
		CategoryIDs: parseIDList(p.CategoryIds),
	}
}

// parseIDList 把逗号拼接的 ID 串还原成列表（与 query 参数同一契约）。
func parseIDList(raw string) []idgen.ID {
	ids := response.ParseFilterIDs(raw, response.MaxFilterValues)
	if len(ids) == 0 {
		return nil
	}
	out := make([]idgen.ID, 0, len(ids))
	for _, id := range ids {
		out = append(out, idgen.ID(id))
	}
	return out
}

// messageExportParams 是留言导出任务参数。
type messageExportParams struct {
	Keyword string `json:"keyword,omitempty"`
	Status  string `json:"status,omitempty"`
	ActorID int64  `json:"actorId"`
}

func (p messageExportParams) query() MessageQuery {
	statuses := response.ParseList(p.Status, response.MaxFilterValues)
	for _, st := range statuses {
		if !ValidMessageStatus(st) {
			return MessageQuery{}
		}
	}
	return MessageQuery{Keyword: p.Keyword, Status: statuses}
}

// RunPostExportJob 是 website.post-export 的任务处理器：列与页面上看到的列表一致
//（标题 / slug / 分类 / 发布状态 / 发布时间 / 更新时间）。
func (s *Service) RunPostExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p postExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	tx := contentTx(s.db.WithContext(ctx).Model(&Post{}), p.query()).Preload("Categories")
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	if total > contentExportMax {
		return job.ResultFile{}, job.Errorf("匹配到 %d 条新闻，超过导出上限 %d 条，请缩小筛选范围后重试", total, contentExportMax)
	}
	var rows []Post
	if err := tx.Order("updated_at DESC, id DESC").Limit(contentExportMax).Find(&rows).Error; err != nil {
		return job.ResultFile{}, err
	}
	report(0, fmt.Sprintf("共 %d 行", len(rows)))
	return job.WriteResult(ctx, j, fmt.Sprintf("posts-%s.xlsx", time.Now().Format("20060102")), xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("新闻", excel.SheetOptions{Header: []string{"标题", "Slug", "分类", "发布状态", "发布时间", "更新时间"}})
		if err != nil {
			return err
		}
		now := time.Now()
		for i := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := &rows[i]
			cats := make([]string, 0, len(p.Categories))
			for _, c := range p.Categories {
				cats = append(cats, c.Title)
			}
			status := "未发布"
			if p.Published {
				status = "已发布"
				if p.PublishedAt != nil && p.PublishedAt.After(now) {
					status = "定时发布"
				}
			}
			var publishedAt any
			if p.PublishedAt != nil {
				publishedAt = p.PublishedAt.In(time.Local)
			}
			if err := sh.WriteRow(p.Title, p.Slug, strings.Join(cats, "、"), status, publishedAt, p.UpdatedAt.In(time.Local)); err != nil {
				return err
			}
			report((i + 1) * 100 / max(len(rows), 1), fmt.Sprintf("已导出 %d / %d 行", i+1, len(rows)))
		}
		return x.Close()
	})
}

// RunMessageExportJob 是 website.message-export 的任务处理器：列与留言页一致
//（状态 / 姓名 / 邮箱 / 电话 / 内容 / IP / 时间）。
func (s *Service) RunMessageExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p messageExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	q := p.query()
	tx := messageFilter(s.db.WithContext(ctx).Model(&Message{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	if total > contentExportMax {
		return job.ResultFile{}, job.Errorf("匹配到 %d 条留言，超过导出上限 %d 条，请缩小筛选范围后重试", total, contentExportMax)
	}
	var rows []Message
	if err := tx.Order("created_at DESC, id DESC").Limit(contentExportMax).Find(&rows).Error; err != nil {
		return job.ResultFile{}, err
	}
	report(0, fmt.Sprintf("共 %d 行", len(rows)))
	return job.WriteResult(ctx, j, fmt.Sprintf("messages-%s.xlsx", time.Now().Format("20060102")), xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("留言", excel.SheetOptions{Header: []string{"状态", "姓名", "邮箱", "电话", "内容", "IP", "时间"}})
		if err != nil {
			return err
		}
		for i := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			m := &rows[i]
			if err := sh.WriteRow(messageStatusText(m.Status), m.Name, m.Email, m.Phone, m.Content, m.IP, m.CreatedAt.In(time.Local)); err != nil {
				return err
			}
			report((i + 1) * 100 / max(len(rows), 1), fmt.Sprintf("已导出 %d / %d 行", i+1, len(rows)))
		}
		return x.Close()
	})
}

func messageStatusText(st string) string {
	switch st {
	case MsgNew:
		return "新留言"
	case MsgRead:
		return "已读待跟进"
	case MsgHandled:
		return "已处理"
	}
	return st
}

// submitContentExport 是新闻 / 留言导出提交端点共用的流程。
func submitContentExport(c *gin.Context, jobType string, owner idgen.ID, params any) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("website.export_center_disabled"))
		return
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{Type: jobType, Owner: owner, Params: params})
	switch {
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(429, "error.too_many"))
	case err != nil:
		response.Fail(c, err)
	default:
		response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
	}
}

// PostExportAsync POST /posts/export-async
func (h *handler) PostExportAsync(c *gin.Context) {
	p := postExportParams{
		Keyword:     c.Query("keyword"),
		Status:      c.Query("status"),
		CategoryIds: c.Query("categoryIds"),
	}
	var owner idgen.ID
	if cl := auth.ClaimsFrom(c); cl != nil {
		p.ActorID, owner = int64(cl.UserID), cl.UserID
	}
	submitContentExport(c, jobTypePostExport, owner, p)
}

// MessageExportAsync POST /messages/export-async
func (h *handler) MessageExportAsync(c *gin.Context) {
	p := messageExportParams{Keyword: c.Query("keyword"), Status: c.Query("status")}
	var owner idgen.ID
	if cl := auth.ClaimsFrom(c); cl != nil {
		p.ActorID, owner = int64(cl.UserID), cl.UserID
	}
	submitContentExport(c, jobTypeMessageExport, owner, p)
}
