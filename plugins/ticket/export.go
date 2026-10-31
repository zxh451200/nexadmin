package ticket

// 工单导出：沿用列表的全套筛选（见 ticketFilter），一次最多 exportLimit 行。
// 超限在写响应头之前拒绝，所以调用方拿到的是明确的 JSON 错误，不是半个文件。
// 列是显式白名单：只出工单元数据，不出正文与内部备注——备注里常有坐席的内部判断，
// 导出件会被随意转发，不该把它一起带出去。

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
)

// exportLimit 是一次导出的行数上限（与在线列表接口分开，避免大结果集拖垮请求）。
const exportLimit = 5000

// ExportTooManyReason 是行数超限错误的 reason，前端据此显示本地化提示。
const ExportTooManyReason = "TICKET_EXPORT_TOO_MANY"

// ExportTickets 按列表筛选取工单行（不含分页）；命中行数超过 limit 时拒绝。
func (s *Service) ExportTickets(ctx context.Context, q TicketQuery, limit int) ([]Ticket, error) {
	if limit <= 0 {
		limit = exportLimit
	}
	var total int64
	if err := s.ticketFilter(ctx, q).Count(&total).Error; err != nil {
		return nil, err
	}
	if total > int64(limit) {
		e := response.BadRequest("error.export_too_many")
		e.Reason = ExportTooManyReason
		return nil, e
	}
	var rows []Ticket
	err := s.ticketFilter(ctx, q).Order("COALESCE(last_reply_at, created_at) DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// exportEnv 是一次导出请求内共享的呈现上下文：语言译文缓存 + 应用时区。
type exportEnv struct {
	labels *exportLabels
	loc    *time.Location
}

// time 把 UTC 存值换算成操作者看到的墙上时间；nil 交给 excel 写成空单元格。
func (e *exportEnv) time(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.In(e.loc)
}

// exportColumns 是列顺序与对应的表头 key、取值函数；加列改这里，表头与数据行不会错位。
var exportColumns = []struct {
	key string
	val func(e *exportEnv, r *Ticket) any
}{
	{"ticket.export_no", func(_ *exportEnv, r *Ticket) any { return r.TicketNo }},
	{"ticket.export_title", func(_ *exportEnv, r *Ticket) any { return r.Title }},
	{"ticket.export_status", func(e *exportEnv, r *Ticket) any { return e.labels.status(r.Status) }},
	{"ticket.export_priority", func(e *exportEnv, r *Ticket) any { return e.labels.priority(r.Priority) }},
	{"ticket.export_group", func(_ *exportEnv, r *Ticket) any { return r.GroupName }},
	{"ticket.export_assignee", func(_ *exportEnv, r *Ticket) any { return r.AssigneeName }},
	{"ticket.export_reporter", func(_ *exportEnv, r *Ticket) any {
		if r.ReporterName != "" {
			return r.ReporterName
		}
		return r.ReporterID
	}},
	{"ticket.export_creator", func(_ *exportEnv, r *Ticket) any { return r.CreatorName }},
	{"ticket.export_biz", func(_ *exportEnv, r *Ticket) any {
		if r.BizType == "" || r.BizNo == "" {
			return r.BizType + r.BizNo
		}
		return r.BizType + ": " + r.BizNo
	}},
	{"ticket.export_tags", func(_ *exportEnv, r *Ticket) any { return strings.Join(r.TagList, ",") }},
	{"ticket.export_rating", func(_ *exportEnv, r *Ticket) any {
		if r.Rating == nil {
			return nil
		}
		return strconv.Itoa(*r.Rating)
	}},
	{"ticket.export_response_overdue", func(_ *exportEnv, r *Ticket) any { return r.Overdue }},
	{"ticket.export_resolution_overdue", func(_ *exportEnv, r *Ticket) any { return r.ResolutionOverdue }},
	{"ticket.export_created_at", func(e *exportEnv, r *Ticket) any { return e.time(&r.CreatedAt) }},
	{"ticket.export_first_reply_at", func(e *exportEnv, r *Ticket) any { return e.time(r.FirstReplyAt) }},
	{"ticket.export_resolved_at", func(e *exportEnv, r *Ticket) any { return e.time(r.ResolvedAt) }},
	{"ticket.export_closed_at", func(e *exportEnv, r *Ticket) any { return e.time(r.ClosedAt) }},
	{"ticket.export_last_reply_at", func(e *exportEnv, r *Ticket) any { return e.time(r.LastReplyAt) }},
}

// exportLabels 缓存一次状态/优先级译文：整份文件用同一种语言，逐行查字典是浪费。
type exportLabels struct {
	status   func(string) string
	priority func(string) string
}

func newExportLabels(ctx context.Context) *exportLabels {
	return &exportLabels{
		status:   func(v string) string { return exportLabel(ctx, "ticket.status_", v) },
		priority: func(v string) string { return exportLabel(ctx, "ticket.priority_", v) },
	}
}

// exportLabel 取字典译文；字典没有该 key（i18n.T 原样回显 key）时退回存值，
// 这样新增状态在译文补齐前也不会导出空白列。
func exportLabel(ctx context.Context, prefix, value string) string {
	key := prefix + value
	if t := i18n.T(ctx, key); t != key {
		return t
	}
	return value
}

// WriteTicketsXLSX 把导出行写成 xlsx。
func (s *Service) WriteTicketsXLSX(ctx context.Context, w io.Writer, rows []Ticket) error {
	env := &exportEnv{labels: newExportLabels(ctx), loc: s.location()}
	headers := make([]string, len(exportColumns))
	for i, c := range exportColumns {
		headers[i] = i18n.T(ctx, c.key)
	}
	x := excel.NewWriter(w)
	sh, err := x.NewSheet(i18n.T(ctx, "ticket.export_sheet"), excel.SheetOptions{Header: headers})
	if err != nil {
		return err
	}
	for i := range rows {
		vals := make([]any, len(exportColumns))
		for j, c := range exportColumns {
			vals[j] = c.val(env, &rows[i])
		}
		if err := sh.WriteRow(vals...); err != nil {
			return err
		}
	}
	return x.Close()
}

// ExportFilename 是导出文件名（带应用时区时间戳，重复导出不覆盖）。
func (s *Service) ExportFilename() string {
	return "tickets-" + time.Now().In(s.location()).Format("20060102-150405") + ".xlsx"
}
