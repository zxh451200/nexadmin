package ticket

// 工单导出的异步任务：无论数据量大小一律提交 core/job 进导出中心（下线同步直出端点）。
// 任务参数只带筛选条件与操作者；权限与数据范围都在执行时按任务所有者重算——worker 里没有
// request 上下文，角色被收回、数据范围被收紧后，排队中的任务不应继续产出越权数据。
// 列仍是显式白名单（见 exportColumns）：只出工单元数据，不出正文与内部备注。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// jobTypeTicketExport 是导出中心里的任务类型。
const jobTypeTicketExport = "ticket.export"

// xlsxContentType 是导出文件的 MIME。
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// permList 是查看工单的权限点：提交导出与任务执行时重查都用它。
const permList = "ticket:list"

// exportParams 是任务参数：只有与 GET /tickets-export 相同的筛选字段（照 ticketFilter 的口径，
// status 逗号多值）与操作者身份，不含任何凭据。提交时 mine=1 已解析成具体的 AssigneeID。
type exportParams struct {
	Status     string   `json:"status,omitempty"`
	Priority   string   `json:"priority,omitempty"`
	BizType    string   `json:"bizType,omitempty"`
	Keyword    string   `json:"keyword,omitempty"`
	Overdue    string   `json:"overdue,omitempty"`
	Tag        string   `json:"tag,omitempty"`
	AssigneeID int64    `json:"assigneeId,omitempty"`
	Unassigned bool     `json:"unassigned,omitempty"`
	GroupID    int64    `json:"groupId,omitempty"`
	Sort       string   `json:"sort,omitempty"`
	Order      string   `json:"order,omitempty"`
	ActorID    idgen.ID `json:"actorId"`
}

func (p exportParams) query() TicketQuery {
	return TicketQuery{
		Status: p.Status, Priority: p.Priority, BizType: p.BizType, Keyword: p.Keyword,
		Overdue: p.Overdue, Tag: p.Tag,
		AssigneeID: idgen.ID(p.AssigneeID), Unassigned: p.Unassigned, GroupID: idgen.ID(p.GroupID),
		Sort: p.Sort, Order: p.Order,
	}
}

// ownerMayExport 在任务执行时重新确认所有者仍然存在并持有 ticket:list；返回的用户
// 供数据范围解析（formbuilder/exportjob.go ownerMayExport 的同一模式）。
func ownerMayExport(ctx context.Context, owner idgen.ID) (*auth.User, error) {
	svc := auth.Default()
	if svc == nil || svc.Provider() == nil || owner == 0 {
		return nil, job.Errorf("无法确认任务所有者的权限，已拒绝导出")
	}
	u, err := svc.Provider().Profile(ctx, owner)
	if err != nil || u == nil {
		return nil, job.Errorf("任务所有者不存在或已停用，无法导出")
	}
	if !u.Super && !auth.HasPerm(u.Perms, permList) {
		return nil, job.Errorf("任务所有者已没有导出权限")
	}
	return u, nil
}

// RunExportJob 是 ticket.export 的任务处理器。ctx 带任务所属租户（job 管理器注入）。
func (s *Service) RunExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p exportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	owner, err := ownerMayExport(ctx, p.ActorID)
	if err != nil {
		return job.ResultFile{}, err
	}
	// 数据范围按执行时的角色重算（ticketFilter 里的 scopeCond 读同一份 scopeKey）。
	sc, err := datascope.Resolve(ctx, s.db, owner.ID, owner.Super)
	if err != nil {
		return job.ResultFile{}, err
	}
	ctx = context.WithValue(ctx, scopeKey{}, sc)
	q := p.query()
	if err := validateStatuses(q.Status); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效：状态筛选不合法")
	}
	// 行数上限沿用 exportLimit 语义：超限明确失败（中文原因写进任务），不再截断。
	var total int64
	if err := s.ticketFilter(ctx, q).Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	if total > int64(maxExportRows()) {
		return job.ResultFile{}, job.Errorf("导出共 %d 行，超过单次导出上限 %d 行，请缩小筛选范围后重试", total, maxExportRows())
	}
	report(0, fmt.Sprintf("共 %d 行", total))
	rows, err := s.ExportTickets(ctx, q, maxExportRows())
	if err != nil {
		return job.ResultFile{}, err
	}
	name := s.ExportFilename()
	res, werr := job.WriteResult(ctx, j, name, xlsxContentType, func(w io.Writer) error {
		return s.WriteTicketsXLSX(ctx, w, rows)
	})
	// 审计（谁、按什么条件、导出了几行）与 GET 直出时代同一口径：不进操作日志中间件，记应用日志。
	if werr == nil && s.log != nil {
		operator := owner.Username
		if operator == "" {
			operator = strconv.FormatInt(int64(p.ActorID), 10)
		}
		s.log.Info("ticket export", "operator", operator, "rows", len(rows), "filters", j.Params)
	}
	return res, werr
}

// exportAsync POST /plugins/ticket/tickets-export-async：创建导出任务并立即返回任务 ID。
// 筛选字段与 GET /tickets-export 相同（查询参数同名单）；权限点沿用 ticket:list。
func (h *handler) exportAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("ticket.export_center_disabled"))
		return
	}
	uid, _ := claimsOperator(c)
	if uid == 0 {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	q := ticketQueryFrom(c)
	// 先校验筛选条件（状态合法性），避免任务提交后才失败
	if err := validateStatuses(q.Status); err != nil {
		response.Fail(c, err)
		return
	}
	p := exportParams{
		Status: q.Status, Priority: q.Priority, BizType: q.BizType, Keyword: q.Keyword,
		Overdue: q.Overdue, Tag: q.Tag,
		AssigneeID: int64(q.AssigneeID), Unassigned: q.Unassigned, GroupID: int64(q.GroupID),
		Sort: q.Sort, Order: q.Order, ActorID: uid,
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{Type: jobTypeTicketExport, Owner: uid, Params: p})
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
