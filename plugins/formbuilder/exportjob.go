package formbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"nexadmin/core/auth"
	corei18n "nexadmin/core/i18n"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
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
		n, err := s.writeExport(ctx, w, p.query(), MaxAsyncExport, p.Lang)
		if err != nil {
			var re *response.Error
			if errors.As(err, &re) {
				// response.Error 只带 i18n key；job 行里存的是给任务所有者看的一句话，
				// 翻成文案落库（job ctx 不带语言时回落默认语言），而不是存 key 本身。
				if re.Msg != "" {
					return job.Errorf("%s", re.Msg)
				}
				return job.Errorf("%s", corei18n.T(ctx, re.Key, re.Args...))
			}
			return err
		}
		report(100, fmt.Sprintf("已导出 %d 行", n))
		return nil
	})
}

// ---- 导出文件的装配（只服务导出中心任务，同步直出端点已下线） ----

type exportCol struct{ Key, Title string }

// exportColumns 取所有版本字段的并集：按首次出现的顺序排列，标题取最新出现的版本，所以改版删字段后历史数据仍能导出。
func exportColumns(defs map[int]*Definition, versions []int) []exportCol {
	var cols []exportCol
	idx := map[string]int{}
	for _, v := range versions {
		for _, f := range defs[v].Fields {
			if !f.hasValue() {
				continue
			}
			if i, ok := idx[f.Key]; ok {
				cols[i].Title = f.Title
				continue
			}
			idx[f.Key] = len(cols)
			cols = append(cols, exportCol{f.Key, f.Title})
		}
	}
	return cols
}

// exportFixedCols 是导出表头里由框架固定的前四列，词条在 core/i18n/locales 的
// formbuilder 命名空间下。加一门语言只需补四个键（或放进部署级 config/locales），
// 不用像旧的 map[string][4]string 那样改 Go 代码——那种写法字面上只认两种语言，
// 第三种会掉进兜底分支拿到简体中文，而英文其实是写死在第二行的。
var exportFixedCols = [...]string{
	"formbuilder.head_id",
	"formbuilder.head_submitter",
	"formbuilder.head_time",
	"formbuilder.head_version",
}

// writeExport 把符合条件的提交写成 xlsx（最新的在前），最多 maxRows 行，超过则返回 errExportTooLarge 且不应使用已写内容。
// 文本以 = + - @ 开头时由 excel 包加前缀，防 CSV/公式注入。
func (s *Service) writeExport(ctx context.Context, w io.Writer, q SubQuery, maxRows int, lang string) (int, error) {
	if err := checkFilter(q); err != nil {
		return 0, err
	}
	if _, err := s.store.GetForm(ctx, q.FormID); err != nil {
		return 0, err
	}
	vs, err := s.store.ListVersions(ctx, q.FormID)
	if err != nil {
		return 0, err
	}
	defs := make(map[int]*Definition, len(vs))
	order := make([]int, 0, len(vs))
	for _, v := range vs {
		d, err := decodeDefinition([]byte(v.Definition))
		if err != nil {
			return 0, err
		}
		defs[v.Version] = d
		order = append(order, v.Version)
	}
	cols := exportColumns(defs, order)
	header := make([]string, 0, len(exportFixedCols)+len(cols))
	for _, key := range exportFixedCols {
		header = append(header, corei18n.T(corei18n.WithLang(ctx, lang), key))
	}
	for _, c := range cols {
		header = append(header, c.Title)
	}
	x := excel.NewWriter(w, excel.Options{FormulaGuard: excel.GuardPrefix})
	sh, err := x.NewSheet("Submissions", excel.SheetOptions{Header: header})
	if err != nil {
		return 0, err
	}
	rows := 0
	var werr error
	_, err = s.iterate(ctx, q, 1<<30, func(sub *Submission) bool {
		if rows >= maxRows {
			werr = errExportTooLarge
			return false
		}
		def := defs[sub.Version]
		var data map[string]any
		_ = json.Unmarshal([]byte(sub.Data), &data)
		vals := []any{sub.ID.String(), sub.Username, sub.CreatedAt.UTC(), sub.Version}
		for _, c := range cols {
			vals = append(vals, cellValue(def, c.Key, data[c.Key]))
		}
		if werr = sh.WriteRow(vals...); werr != nil {
			return false
		}
		rows++
		return ctx.Err() == nil
	})
	if err == nil {
		err = werr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return rows, err
	}
	return rows, x.Close()
}

// cellValue 把一个提交值转成单元格值：选项换成名称，多值用顿号连接，文件取文件名。
func cellValue(def *Definition, key string, v any) any {
	if v == nil || def == nil {
		return nil
	}
	var f *Field
	for i := range def.Fields {
		if def.Fields[i].Key == key {
			f = &def.Fields[i]
			break
		}
	}
	label := func(val any) string {
		s, _ := val.(string)
		if f != nil {
			for _, o := range f.Options {
				if o.Value == s {
					return o.Label
				}
			}
		}
		return s
	}
	switch x := v.(type) {
	case string:
		return label(x)
	case float64, bool:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				n, _ := m["name"].(string)
				parts = append(parts, n)
			} else {
				parts = append(parts, label(e))
			}
		}
		return strings.Join(parts, "、")
	}
	return fmt.Sprint(v)
}
