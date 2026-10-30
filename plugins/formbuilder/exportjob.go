package formbuilder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// JobType 是导出中心里的任务类型。
const JobType = "formbuilder.export"

// 导出任务的权限码（任务执行时按任务所有者重新校验）。
const PermExport = "formbuilder:export"

// exportParams 是任务参数：只有筛选条件和所有者，不含任何凭据。
type exportParams struct {
	FormID   idgen.ID `json:"formId"`
	Version  int      `json:"version,omitempty"`
	Username string   `json:"username,omitempty"`
	Field    string   `json:"field,omitempty"`
	Value    string   `json:"value,omitempty"`
	From     string   `json:"from,omitempty"`
	To       string   `json:"to,omitempty"`
	Lang     string   `json:"lang,omitempty"`
	ActorID  idgen.ID `json:"actorId"`
}

func (p exportParams) query() SubQuery {
	q := SubQuery{FormID: p.FormID, Version: p.Version, Username: p.Username, Field: p.Field, Value: p.Value}
	if t, err := time.Parse(time.RFC3339, p.From); err == nil {
		q.From = &t
	}
	if t, err := time.Parse(time.RFC3339, p.To); err == nil {
		q.To = &t
	}
	return q
}

// SubmitExportJob 把导出提交到导出中心（core/job），返回任务信息。导出中心未启用时返回 503。
func (s *Service) SubmitExportJob(ctx context.Context, actor Actor, q SubQuery, lang string) (map[string]any, error) {
	m := job.Default()
	if m == nil {
		return nil, response.Unavailable("formbuilder.export_center_disabled")
	}
	if err := checkFilter(q); err != nil {
		return nil, err
	}
	if _, err := s.store.GetForm(ctx, q.FormID); err != nil {
		return nil, err
	}
	p := exportParams{
		FormID: q.FormID, Version: q.Version, Username: q.Username, Field: q.Field, Value: q.Value, Lang: lang, ActorID: actor.ID,
	}
	if q.From != nil {
		p.From = q.From.UTC().Format(time.RFC3339)
	}
	if q.To != nil {
		p.To = q.To.UTC().Format(time.RFC3339)
	}
	j, err := m.Submit(ctx, job.Submit{Type: JobType, Owner: actor.ID, Params: p})
	switch {
	case errors.Is(err, job.ErrTooMany):
		return nil, response.New(429, "formbuilder.export_too_many")
	case err != nil:
		return nil, err
	}
	return map[string]any{"id": j.ID.String(), "type": j.Type, "status": j.Status}, nil
}

// ownerMayExport 在任务执行时重新确认所有者仍然存在并拥有导出权限（角色被收回后，排队中的任务不应继续产出数据）。
func ownerMayExport(ctx context.Context, owner idgen.ID) error {
	svc := auth.Default()
	if svc == nil || svc.Provider() == nil || owner == 0 {
		return job.Errorf("无法确认任务所有者的权限，已拒绝导出")
	}
	u, err := svc.Provider().Profile(ctx, owner)
	if err != nil || u == nil {
		return job.Errorf("任务所有者不存在或已停用，无法导出")
	}
	if !u.Super && !auth.HasPerm(u.Perms, PermExport) {
		return job.Errorf("任务所有者已没有导出权限")
	}
	return nil
}

// exportJob 是导出中心任务处理器。ctx 带任务所属租户（job 管理器注入）。
func (s *Service) exportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p exportParams
	if err := j.Decode(&p); err != nil || p.FormID == 0 {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	if err := ownerMayExport(ctx, p.ActorID); err != nil {
		return job.ResultFile{}, err
	}
	report(0, "导出中")
	name := fmt.Sprintf("formbuilder-%s-%s.xlsx", p.FormID, time.Now().Format("20060102"))
	return job.WriteResult(ctx, j, name, xlsxContentType, func(w io.Writer) error {
		n, err := s.WriteExport(ctx, w, p.query(), MaxAsyncExport, p.Lang)
		if err != nil {
			var re *response.Error
			if errors.As(err, &re) && re.Msg != "" {
				return job.Errorf("%s", re.Msg)
			}
			return err
		}
		report(100, fmt.Sprintf("已导出 %d 行", n))
		return nil
	})
}
