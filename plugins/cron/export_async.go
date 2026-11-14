package cron

// 异步导出：执行日志导出一律登记 core/job 任务，worker 流式写 xlsx 到存储，
// 用户在「导出中心」下载（无论数据量大小，行为一致；小任务几秒完成）。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

const (
	jobTypeLogExport = "cron.log-export"
	// logCellTruncateAt 与 service.go 里 logListColumns 的 SUBSTR(...,1,500) 同一个数：
	// 导出里的 output / error 是日志的前 500 个字符，不是全文（全文用 GET /logs/:id）。
	logCellTruncateAt = 500
	exportBatch       = 1000 // 每批读取行数
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// logExportHeader 是导出列的白名单（与原 CSV 列一致，中文表头与列表页对齐）。
var logExportHeader = []string{"开始时间", "任务", "处理器", "触发方式", "状态", "尝试次数", "耗时 ms", "输出", "错误"}

// logExportParams 是任务参数：只含筛选条件与操作者身份，不含任何凭据。
// JobIDs / Status 与列表接口的 query 参数同一契约（jobId 逗号多选、status 多值）。
type logExportParams struct {
	JobIDs  []int64  `json:"jobIds,omitempty"`
	Status  []string `json:"status,omitempty"`
	ActorID idgen.ID `json:"actorId"`
}

// head 给被 SUBSTR 截过断的单元格补一个省略号：导出文件里没有「这格被截了」的元信息，
// 不加标记读日志的人会以为输出就是这么长。恰好 500 字符的完整输出会被误标，宁可多标不可漏标。
func head(s string) string {
	if utf8.RuneCountInString(s) >= logCellTruncateAt {
		return s + "…"
	}
	return s
}

// logExportBody 是 POST /logs/export-async 的请求体（与查询参数同形）。
type logExportBody struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// exportLogsAsync POST /plugins/cron/logs/export-async：创建导出任务并立即返回任务 ID。
func (h *handler) exportLogsAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("cron.export_center_disabled"))
		return
	}
	jobIDRaw, statusRaw := c.Query("jobId"), c.Query("status")
	if c.Request.Method == "POST" && c.Request.ContentLength != 0 {
		var b logExportBody
		if c.ShouldBindJSON(&b) == nil {
			if b.JobID != "" {
				jobIDRaw = b.JobID
			}
			if b.Status != "" {
				statusRaw = b.Status
			}
		}
	}
	var owner idgen.ID
	if cl := auth.ClaimsFrom(c); cl != nil {
		owner = cl.UserID
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{
		Type: jobTypeLogExport, Owner: owner,
		Params: logExportParams{
			JobIDs:  response.ParseFilterIDs(jobIDRaw, response.MaxFilterValues),
			Status:  response.ParseList(statusRaw, response.MaxFilterValues),
			ActorID: owner,
		},
	})
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

// runLogExportJob 是 cron.log-export 的任务处理器：按列表同款筛选流式分批写 xlsx，逐批汇报进度。
// 数据范围以任务所有者在执行时的权限为准已由提交时的权限点把守（cron:log）；这里只做数据装配。
func (s *Service) runLogExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p logExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	tx, err := s.logFilter(ctx, LogQuery{JobIDs: p.JobIDs, Status: p.Status})
	if err != nil {
		return job.ResultFile{}, err
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	if total > logExportMax {
		return job.ResultFile{}, job.Errorf("命中 %d 行执行日志，超过单次导出上限 %d 行，请缩小筛选范围后重试", total, logExportMax)
	}
	report(0, fmt.Sprintf("共 %d 行", total))

	name := fmt.Sprintf("cron-logs-%s.xlsx", time.Now().Format("20060102"))
	return job.WriteResult(ctx, j, name, xlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("执行日志", excel.SheetOptions{Header: logExportHeader})
		if err != nil {
			return err
		}
		var done int64
		last := int64(math.MaxInt64)
		for done < total {
			if err := ctx.Err(); err != nil {
				return err
			}
			var rows []Log
			if err := tx.Where("id < ?", last).Order("id DESC").
				Select(logListColumns).Limit(int(min(exportBatch, total-done))).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for i := range rows {
				l := &rows[i]
				if err := sh.WriteRow(l.StartedAt.UTC(), l.JobName, l.Handler, l.TriggerType, l.Status,
					l.Attempts, l.CostMs, head(l.Output), head(l.Error)); err != nil {
					return err
				}
			}
			done += int64(len(rows))
			last = int64(rows[len(rows)-1].ID)
			report(int(done*100/max(total, 1)), fmt.Sprintf("已导出 %d / %d 行", done, total))
		}
		return x.Close()
	})
}
