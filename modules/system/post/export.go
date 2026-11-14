package post

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

// 岗位导出：post.export 异步任务。岗位是小表，但导出入口与其他列表保持同一种行为：
// 无论数据量大小都建任务进导出中心，不在请求里同步吐文件。

const (
	jobTypePostExport = "post.export"
	postExportMax     = 10000 // 单次导出行数上限（岗位远小于此，纯防御）
	xlsxContentType   = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// postExportParams 是任务参数：只含筛选与操作者；租户由任务管理器注入。
type postExportParams struct {
	Keyword string   `json:"keyword,omitempty"`
	ActorID idgen.ID `json:"actorId"`
}

// ExportJob 是 post.export 的任务处理器。
func (s *Service) ExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p postExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	rows, err := s.List(ctx, p.Keyword)
	if err != nil {
		return job.ResultFile{}, err
	}
	if len(rows) > postExportMax {
		rows = rows[:postExportMax]
	}
	report(0, fmt.Sprintf("共 %d 行", len(rows)))

	// 部门名一并查出（导出里给名字比给 ID 有用）
	deptName := map[idgen.ID]string{}
	var deptIDs []idgen.ID
	for i := range rows {
		if rows[i].DeptID != nil && *rows[i].DeptID != 0 {
			deptIDs = append(deptIDs, *rows[i].DeptID)
		}
	}
	if len(deptIDs) > 0 {
		var depts []struct {
			ID   idgen.ID
			Name string
		}
		if err := s.db.WithContext(ctx).Table("sys_dept").Select("id, name").Where("id IN ?", deptIDs).Scan(&depts).Error; err == nil {
			for _, d := range depts {
				deptName[d.ID] = d.Name
			}
		}
	}

	return job.WriteResult(ctx, j, fmt.Sprintf("posts-%s.xlsx", time.Now().Format("20060102")), xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("岗位", excel.SheetOptions{Header: []string{"名称", "编码", "部门", "排序", "状态", "备注", "创建时间"}})
		if err != nil {
			return err
		}
		for i := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := &rows[i]
			status := "停用"
			if p.Status == "1" {
				status = "启用"
			}
			dept := ""
			if p.DeptID != nil {
				dept = deptName[*p.DeptID]
			}
			if err := sh.WriteRow(p.Name, p.Code, dept, p.Sort, status, p.Remark, p.CreatedAt); err != nil {
				return err
			}
			report((i + 1) * 100 / max(len(rows), 1), fmt.Sprintf("已导出 %d / %d 行", i+1, len(rows)))
		}
		return x.Close()
	})
}

// ExportAsync POST /posts/export-async
func (h *Handler) ExportAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("post.export_center_disabled"))
		return
	}
	p := postExportParams{Keyword: c.Query("keyword")}
	if cl := auth.ClaimsFrom(c); cl != nil {
		p.ActorID = cl.UserID
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{Type: jobTypePostExport, Owner: p.ActorID, Params: p})
	switch {
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(429, "error.too_many"))
	case err != nil:
		response.Fail(c, err)
	default:
		response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
	}
}
