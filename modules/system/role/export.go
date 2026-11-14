package role

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// 角色导出：role.export 异步任务。角色是小表，但导出入口与其他列表保持同一种行为：
// 无论数据量大小都建任务进导出中心，不在请求里同步吐文件。

const (
	jobTypeRoleExport = "role.export"
	roleExportMax     = 1000 // 与列表接口同一上限（角色页一次拉全量），纯防御
	xlsxContentType   = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// roleExportParams 是任务参数：只含筛选与操作者；租户由任务管理器注入。
type roleExportParams struct {
	Keyword string   `json:"keyword,omitempty"`
	Status  string   `json:"status,omitempty"`
	ActorID idgen.ID `json:"actorId"`
}

// scopeText 数据范围的导出文案（与列表页枚举一致）。
var scopeText = map[string]string{
	scopeAll: "全部", scopeCustom: "自定义部门", scopeDept: "本部门",
	scopeDeptAndChild: "本部门及以下", scopeSelf: "仅本人",
}

// ExportJob 是 role.export 的任务处理器。
func (s *service) ExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p roleExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	rows, _, err := s.list(ctx, listQuery{Page: 1, PageSize: roleExportMax, Keyword: p.Keyword, Status: p.Status})
	if err != nil {
		return job.ResultFile{}, err
	}
	report(0, fmt.Sprintf("共 %d 行", len(rows)))

	return job.WriteResult(ctx, j, fmt.Sprintf("roles-%s.xlsx", time.Now().Format("20060102")), xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("角色", excel.SheetOptions{Header: []string{"名称", "编码", "数据范围", "排序", "状态", "内置", "备注", "创建时间"}})
		if err != nil {
			return err
		}
		for i, r := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			status := "停用"
			if r.Status == statusOn {
				status = "启用"
			}
			builtin := "否"
			if r.Builtin {
				builtin = "是"
			}
			if err := sh.WriteRow(r.Name, r.Code, scopeText[r.DataScope], r.Sort, status, builtin, r.Remark, r.CreatedAt); err != nil {
				return err
			}
			report((i + 1) * 100 / max(len(rows), 1), fmt.Sprintf("已导出 %d / %d 行", i+1, len(rows)))
		}
		return x.Close()
	})
}

// ExportAsync POST /roles/export-async
func (h *handler) ExportAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("role.export_center_disabled"))
		return
	}
	p := roleExportParams{Keyword: c.Query("keyword"), Status: c.Query("status")}
	if cl := auth.ClaimsFrom(c); cl != nil {
		p.ActorID = cl.UserID
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{Type: jobTypeRoleExport, Owner: p.ActorID, Params: p})
	switch {
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(429, "error.too_many"))
	case err != nil:
		response.Fail(c, err)
	default:
		response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
	}
}
