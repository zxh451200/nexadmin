package im

// 会话导出的异步任务：无论数据量大小一律提交 core/job 进导出中心（下线同步直出端点）。
// 任务参数只带会话列表的筛选条件与操作者（照 convQueryFrom 现有口径，assignee=me 提交时已解析）；
// 权限在执行时按任务所有者重查——角色被收回后，排队中的任务不应继续产出聊天记录。
// 双 sheet（会话 + 完整对话）与列顺序、i18n 表头 key 与原直出完全一致；job ctx 没有请求语言，
// i18n.T 回落应用默认语言。行数上限改为任务内上限：超限明确失败（中文原因写进任务），不再静默截断。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/i18n"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// jobTypeConversationExport 是导出中心里的任务类型。
const jobTypeConversationExport = "im.conversation-export"

// xlsxContentType 是导出文件的 MIME。
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// permList 是查看会话的权限点：提交导出与任务执行时重查都用它。
const permList = "im:list"

// exportLimit 是一次导出的会话数上限（沿用原直出端点口径；任务内超限明确失败）。
const exportLimit = 5000

// exportParams 是任务参数：只有会话列表的筛选字段与操作者身份，不含任何凭据。
type exportParams struct {
	Status     string   `json:"status,omitempty"`
	BizType    string   `json:"bizType,omitempty"`
	Keyword    string   `json:"keyword,omitempty"`
	Tag        string   `json:"tag,omitempty"`
	AssigneeID string   `json:"assigneeId,omitempty"`
	Unassigned bool     `json:"unassigned,omitempty"`
	ActorID    idgen.ID `json:"actorId"`
}

func (p exportParams) query() ConversationQuery {
	return ConversationQuery{
		Status: p.Status, BizType: p.BizType, Keyword: p.Keyword, Tag: p.Tag,
		AssigneeID: p.AssigneeID, Unassigned: p.Unassigned,
	}
}

// ownerMayExport 在任务执行时重新确认所有者仍然存在并持有 im:list
// （formbuilder/exportjob.go ownerMayExport 的同一模式）。
func ownerMayExport(ctx context.Context, owner idgen.ID) error {
	svc := auth.Default()
	if svc == nil || svc.Provider() == nil || owner == 0 {
		return job.Errorf("无法确认任务所有者的权限，已拒绝导出")
	}
	u, err := svc.Provider().Profile(ctx, owner)
	if err != nil || u == nil {
		return job.Errorf("任务所有者不存在或已停用，无法导出")
	}
	if !u.Super && !auth.HasPerm(u.Perms, permList) {
		return job.Errorf("任务所有者已没有导出权限")
	}
	return nil
}

// RunConversationExportJob 是 im.conversation-export 的任务处理器。ctx 带任务所属租户（job 管理器注入）。
func (s *Service) RunConversationExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p exportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	if err := ownerMayExport(ctx, p.ActorID); err != nil {
		return job.ResultFile{}, err
	}
	q := p.query()
	// 会话数上限沿用 exportLimit：超限明确失败，不再静默截断。
	var total int64
	if err := s.convFilter(ctx, q).Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	if total > exportLimit {
		return job.ResultFile{}, job.Errorf("导出共 %d 个会话，超过单次导出上限 %d 个，请缩小筛选范围后重试", total, exportLimit)
	}
	rows, err := s.ExportConversations(ctx, q, exportLimit)
	if err != nil {
		return job.ResultFile{}, err
	}
	ids := make([]idgen.ID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	// 消息行数同样任务内上限：每个会话的消息数无界，不设上限会把一次导出变成全表扫描；超限明确失败。
	var msgTotal int64
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Table("plg_im_message").Where("conv_id IN ?", ids).Count(&msgTotal).Error; err != nil {
			return job.ResultFile{}, err
		}
	}
	if msgTotal > exportMsgLimit {
		return job.ResultFile{}, job.Errorf("完整对话共 %d 条消息，超过单次导出上限 %d 条，请缩小筛选范围后重试", msgTotal, exportMsgLimit)
	}
	msgs, err := s.ExportMessages(ctx, ids, exportMsgLimit)
	if err != nil {
		return job.ResultFile{}, err
	}
	report(0, fmt.Sprintf("共 %d 个会话 / %d 条消息", total, msgTotal))
	name := fmt.Sprintf("im-conversations-%s.xlsx", time.Now().Format("20060102-150405"))
	return job.WriteResult(ctx, j, name, xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet(i18n.T(ctx, "im.export_sheet"), excel.SheetOptions{Header: []string{
			i18n.T(ctx, "im.export_conv_no"), i18n.T(ctx, "im.export_visitor"), i18n.T(ctx, "im.export_biz"),
			i18n.T(ctx, "im.export_assignee"), i18n.T(ctx, "im.export_status"), i18n.T(ctx, "im.export_tags"),
			i18n.T(ctx, "im.export_note"), i18n.T(ctx, "im.export_rating"), i18n.T(ctx, "im.export_created_at"),
			i18n.T(ctx, "im.export_closed_at"),
		}})
		if err != nil {
			return err
		}
		for _, r := range rows {
			visitor := r.VisitorName
			if visitor == "" {
				visitor = r.VisitorToken
			}
			biz := r.BizNo
			if r.BizType != "" && biz != "" {
				biz = r.BizType + ": " + biz
			}
			closed, rating := "", ""
			if r.ClosedAt != nil {
				closed = r.ClosedAt.Local().Format("2006-01-02 15:04:05")
			}
			if r.Rating > 0 {
				rating = strconv.Itoa(r.Rating)
			}
			if err := sh.WriteRow(r.ConvNo, visitor, biz, r.AssigneeName, i18n.T(ctx, "im.status_"+r.Status),
				r.Tags, r.Note, rating, r.CreatedAt.Local().Format("2006-01-02 15:04:05"), closed); err != nil {
				return err
			}
		}
		ms, err := x.NewSheet(i18n.T(ctx, "im.export_msgs_sheet"), excel.SheetOptions{Header: []string{
			i18n.T(ctx, "im.export_conv_no"), i18n.T(ctx, "im.export_seq"), i18n.T(ctx, "im.export_time"),
			i18n.T(ctx, "im.export_sender"), i18n.T(ctx, "im.export_name"), i18n.T(ctx, "im.export_content"),
		}})
		if err != nil {
			return err
		}
		for _, m := range msgs {
			content := m.Content
			if m.RecalledAt != nil {
				content = i18n.T(ctx, "im.export_recalled")
			}
			if err := ms.WriteRow(m.ConvNo, m.Seq, m.CreatedAt.Local().Format("2006-01-02 15:04:05"),
				i18n.T(ctx, "im.sender_"+m.SenderType), m.SenderName, content); err != nil {
				return err
			}
		}
		return x.Close()
	})
}

// exportAsync POST /plugins/im/conversations-export-async：创建导出任务并立即返回任务 ID。
// 筛选字段与原 GET /conversations-export 相同（查询参数同名单）；权限点沿用 im:list。
func (h *handler) exportAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("im.export_center_disabled"))
		return
	}
	idStr, _ := agentOf(c)
	if idStr == "" {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	uid, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || uid <= 0 {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return
	}
	q := convQueryFrom(c)
	p := exportParams{
		Status: q.Status, BizType: q.BizType, Keyword: q.Keyword, Tag: q.Tag,
		AssigneeID: q.AssigneeID, Unassigned: q.Unassigned, ActorID: idgen.ID(uid),
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{Type: jobTypeConversationExport, Owner: idgen.ID(uid), Params: p})
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
