package log

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/i18n"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// 导出：operlog.export / loginlog.export 异步任务。无论数据量大小都建任务进导出中心，
// 筛选条件与数据范围复用列表查询（operQuery / loginQuery），没有第二套条件；
// 数据范围在任务执行时按任务所有者重算（对齐 user.export 的 ownerActor 模式）。

const (
	defaultExportMaxRows = 50000 // log.export_max_rows 默认值
	xlsxContentType      = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

	jobTypeOperExport  = "operlog.export"
	jobTypeLoginExport = "loginlog.export"
)

// 导出列是显式白名单：操作日志不含请求参数 / 响应 / 异常堆栈（可能带业务数据与内部细节），
// 登录日志只含分类后的失败原因。新增列必须在这里显式加，不会因为模型加字段而自动被导出。
var (
	operExportCols   = []string{"module", "type", "operator", "ip", "location", "status", "cost_ms", "method", "url", "created_at"}
	operExportHeader = []string{"模块", "操作类型", "操作人", "IP", "归属地", "状态", "耗时(ms)", "请求方法", "请求地址", "时间"}

	loginExportCols   = []string{"username", "login_type", "ip", "location", "browser", "os", "status", "fail_reason", "created_at"}
	loginExportHeader = []string{"账号", "登录方式", "IP", "归属地", "浏览器", "操作系统", "状态", "失败原因", "时间"}
)

// WithExportMaxRows 设置导出行数上限（log.export_max_rows，任务内上限：超过整个任务失败）；n <= 0 用默认 50000。
func (s *Service) WithExportMaxRows(n int) *Service {
	s.exportMaxRows = n
	return s
}

func (s *Service) exportMax() int {
	if s.exportMaxRows <= 0 {
		return defaultExportMaxRows
	}
	return s.exportMaxRows
}

func statusText(st string) string {
	if st == StatusSuccess {
		return "成功"
	}
	if st == StatusFailed {
		return "失败"
	}
	return st
}

// exportJobParams 是任务参数：筛选条件与操作者；数据范围不进参数，执行时按所有者重算。
type exportJobParams struct {
	Keyword   string   `json:"keyword,omitempty"`
	Status    []string `json:"status,omitempty"`
	Type      []string `json:"type,omitempty"`
	LoginType []string `json:"loginType,omitempty"`
	StartDate string   `json:"startDate,omitempty"`
	EndDate   string   `json:"endDate,omitempty"`
	ActorID   idgen.ID `json:"actorId"`
}

func (p exportJobParams) query() Query {
	return Query{Keyword: p.Keyword, Status: p.Status, Type: p.Type, LoginType: p.LoginType, StartDate: p.StartDate, EndDate: p.EndDate}
}

// exportJob 是 operlog.export / loginlog.export 共用的任务处理器：按类型选查询与列白名单，
// 流式写 xlsx，逐行汇报进度。
func (s *Service) exportJob(login bool) job.Handler {
	return func(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
		var p exportJobParams
		if err := j.Decode(&p); err != nil {
			return job.ResultFile{}, job.Errorf("任务参数无效")
		}
		owner, super, err := s.jobOwner(ctx, p.ActorID)
		if err != nil {
			return job.ResultFile{}, err
		}
		sc, err := datascope.Resolve(ctx, s.db, owner, super)
		if err != nil {
			return job.ResultFile{}, err
		}
		q := p.query()
		q.Scope = sc
		tx := s.operQuery(ctx, q)
		header, prefix := operExportHeader, "oper-log"
		if login {
			tx = s.loginQuery(ctx, q)
			header, prefix = loginExportHeader, "login-log"
		}
		var total int64
		if err := tx.Count(&total).Error; err != nil {
			return job.ResultFile{}, err
		}
		if max := s.exportMax(); total > int64(max) {
			return job.ResultFile{}, job.Errorf("%s", i18n.T(ctx, "log.export_too_many_rows", total, max))
		}
		report(0, fmt.Sprintf("共 %d 行", total))
		return job.WriteResult(ctx, j, s.exportFilename(prefix), xlsxContentType, func(w io.Writer) error {
			x := excel.NewWriter(w) // 默认公式注入防护（GuardTextStyle）
			sh, err := x.NewSheet(map[bool]string{false: "操作日志", true: "登录日志"}[login], excel.SheetOptions{Header: header})
			if err != nil {
				return err
			}
			cols := operExportCols
			if login {
				cols = loginExportCols
			}
			rows, err := tx.Select(cols).Order("created_at DESC, id DESC").Rows()
			if err != nil {
				return err
			}
			defer rows.Close()
			n := 0
			for rows.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if login {
					var r LoginLog
					if err := s.db.ScanRows(rows, &r); err != nil {
						return err
					}
					if err := sh.WriteRow(r.Username, r.LoginType, r.IP, r.Location, r.Browser, r.OS, statusText(r.Status), r.FailReason,
						r.CreatedAt.In(s.loc)); err != nil {
						return err
					}
				} else {
					var r OperLog
					if err := s.db.ScanRows(rows, &r); err != nil {
						return err
					}
					if err := sh.WriteRow(r.Module, r.Type, r.Operator, r.IP, r.Location, statusText(r.Status), r.CostMs, r.Method, r.URL,
						r.CreatedAt.In(s.loc)); err != nil {
						return err
					}
				}
				n++
				report(n * 100 / max(int(total), 1), fmt.Sprintf("已导出 %d / %d 行", n, total))
			}
			if err := rows.Err(); err != nil {
				return err
			}
			return x.Close()
		})
	}
}

// jobOwner 在执行时按任务所有者重取身份与超管标记：用户已删除或停用则拒绝（对齐 user.export）。
// sys_user 属于用户模块，这里按表直查（导出中心取所有者名同款做法）。
func (s *Service) jobOwner(ctx context.Context, id idgen.ID) (idgen.ID, bool, error) {
	if id == 0 {
		return 0, false, job.Errorf("任务所有者无效")
	}
	var u struct {
		ID      idgen.ID
		IsSuper bool `gorm:"column:is_super"`
		Status  string
	}
	if err := s.db.WithContext(ctx).Table("sys_user").Select("id, is_super, status").Where("id = ?", int64(id)).Take(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, job.Errorf("任务所有者不存在或已停用，无法导出")
		}
		return 0, false, err
	}
	if u.Status != "1" { // 与用户模块 statusOn 一致；不 import 用户模块，按字面量比较
		return 0, false, job.Errorf("任务所有者不存在或已停用，无法导出")
	}
	return u.ID, u.IsSuper, nil
}

// exportFilename 生成带时间的文件名，如 oper-log-20261020-153045.xlsx（应用时区）。
func (s *Service) exportFilename(prefix string) string {
	return prefix + "-" + time.Now().In(s.loc).Format("20060102-150405") + ".xlsx"
}

// submitExport 是两个导出提交端点共用的流程：构造任务参数并提交，返回任务 ID。
func (h *Handler) submitExport(c *gin.Context, jobType string, p exportJobParams) {
	m := job.Default()
	if m == nil {
		response.Fail(c, response.Unavailable("log.export_center_disabled"))
		return
	}
	if cl := auth.ClaimsFrom(c); cl != nil {
		p.ActorID = cl.UserID
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{Type: jobType, Owner: p.ActorID, Params: p})
	switch {
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(429, "error.too_many"))
	case err != nil:
		response.Fail(c, err)
	default:
		response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
	}
}
