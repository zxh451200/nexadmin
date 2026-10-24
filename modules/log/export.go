package log

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
)

// 服务端导出：GET /oper-logs/export、GET /login-logs/export，权限点 operLog:export / loginLog:export。
// 筛选条件与数据范围直接复用列表查询（operQuery / loginQuery），没有第二套条件。

const (
	defaultExportMaxRows = 50000 // log.export_max_rows 默认值
	xlsxContentType      = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

	// ExportTooManyReason 是行数超限错误的 reason（响应体 reason 字段），前端按它显示本地化提示。
	ExportTooManyReason = "EXPORT_TOO_MANY"
)

// 导出列是显式白名单：操作日志不含请求参数 / 响应 / 异常堆栈（可能带业务数据与内部细节），
// 登录日志只含分类后的失败原因。新增列必须在这里显式加，不会因为模型加字段而自动被导出。
var (
	operExportCols   = []string{"module", "type", "operator", "ip", "location", "status", "cost_ms", "method", "url", "created_at"}
	operExportHeader = []string{"模块", "操作类型", "操作人", "IP", "归属地", "状态", "耗时(ms)", "请求方法", "请求地址", "时间"}

	loginExportCols   = []string{"username", "login_type", "ip", "location", "browser", "os", "status", "fail_reason", "created_at"}
	loginExportHeader = []string{"账号", "登录方式", "IP", "归属地", "浏览器", "操作系统", "状态", "失败原因", "时间"}
)

// WithExportMaxRows 设置导出行数上限（log.export_max_rows）；n <= 0 用默认 50000。
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

// checkExportSize 先数一遍行数：超过上限直接拒绝，此时响应还没开始写，可以返回明确的 JSON 错误。
func (s *Service) checkExportSize(n int64) error {
	if max := s.exportMax(); n > int64(max) {
		e := response.BadRequest("error.export_too_many").WithMsg(
			fmt.Sprintf("匹配到 %d 条日志，超过导出上限 %d 条，请缩小筛选范围（如时间范围）后重试，或使用导出中心", n, max))
		e.Reason = ExportTooManyReason // 前端据此显示本地化提示
		return e
	}
	return nil
}

// ExportOper 导出操作日志。通过上限校验后才调用 open 取得输出流（调用方在此时写响应头），然后流式写 xlsx；
// 返回已写出的行数。
func (s *Service) ExportOper(ctx context.Context, q Query, open func() io.Writer) (int, error) {
	tx := s.operQuery(ctx, q).Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return 0, err
	}
	if err := s.checkExportSize(total); err != nil {
		return 0, err
	}
	rows, err := tx.Select(operExportCols).Order("created_at DESC, id DESC").Limit(s.exportMax()).Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	x := excel.NewWriter(open()) // 默认公式注入防护（GuardTextStyle）
	sh, err := x.NewSheet("操作日志", excel.SheetOptions{Header: operExportHeader})
	if err != nil {
		return 0, err
	}
	n := 0
	for rows.Next() {
		var r OperLog
		if err := s.db.ScanRows(rows, &r); err != nil {
			return n, err
		}
		if err := sh.WriteRow(r.Module, r.Type, r.Operator, r.IP, r.Location, statusText(r.Status), r.CostMs, r.Method, r.URL,
			r.CreatedAt.In(s.loc)); err != nil {
			return n, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	return n, x.Close()
}

// ExportLogin 导出登录日志，流程同 ExportOper。
func (s *Service) ExportLogin(ctx context.Context, q Query, open func() io.Writer) (int, error) {
	tx := s.loginQuery(ctx, q).Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return 0, err
	}
	if err := s.checkExportSize(total); err != nil {
		return 0, err
	}
	rows, err := tx.Select(loginExportCols).Order("created_at DESC, id DESC").Limit(s.exportMax()).Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	x := excel.NewWriter(open())
	sh, err := x.NewSheet("登录日志", excel.SheetOptions{Header: loginExportHeader})
	if err != nil {
		return 0, err
	}
	n := 0
	for rows.Next() {
		var r LoginLog
		if err := s.db.ScanRows(rows, &r); err != nil {
			return n, err
		}
		if err := sh.WriteRow(r.Username, r.LoginType, r.IP, r.Location, r.Browser, r.OS, statusText(r.Status), r.FailReason,
			r.CreatedAt.In(s.loc)); err != nil {
			return n, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	return n, x.Close()
}

// exportFilename 生成带时间的文件名，如 oper-log-20261020-153045.xlsx（应用时区）。
func (s *Service) exportFilename(prefix string) string {
	return prefix + "-" + time.Now().In(s.loc).Format("20060102-150405") + ".xlsx"
}

// recordExport 记一条导出操作日志（type=export）。
//
// 取舍：这里在 handler 里直接写，而不挂 Service.Middleware —— 中间件会把响应体前 8KB 当文本入库，
// 对二进制 xlsx 没有意义。也不会递归：导出读的是调用前的快照，且记录在文件写完之后才落库，所以
// 导出文件不含自己这一条；下一次导出才会看到它。代价是“每次导出都多一行”的噪音，由限流
// （ratelimit.log_export，默认每用户 10 次/分钟）兜住上限；通过权限与限流之后的失败（如超行数）也记，便于审计。
func (s *Service) recordExport(c *gin.Context, module string, start time.Time, err error) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return
	}
	l := &OperLog{Module: module, Type: "export", Operator: cl.Username, IP: c.ClientIP(), Status: StatusSuccess,
		CostMs: int(time.Since(start).Milliseconds()), Method: c.Request.Method, URL: c.Request.URL.RequestURI(),
		Params: paramsOf(c, nil)}
	l.TenantID = cl.TenantID
	if err != nil {
		l.Status, l.ErrorStack = StatusFailed, clip(err.Error())
	}
	if werr := s.WriteOper(context.WithoutCancel(c.Request.Context()), l); werr != nil && s.log != nil {
		s.log.Warn("log: write export oper log failed", zap.Error(werr))
	}
}

type exportFn func(ctx context.Context, q Query, open func() io.Writer) (int, error)

// export 是两个导出接口共用的 HTTP 流程：校验 format → 数据范围 → 导出 → 记操作日志。
func (h *Handler) export(c *gin.Context, module, filePrefix string, run exportFn) {
	if f := c.Query("format"); f != "" && f != "xlsx" {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("format 仅支持 xlsx"))
		return
	}
	q := query(c)
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	q.Scope = sc
	start := time.Now()
	started := false
	n, err := run(c.Request.Context(), q, func() io.Writer {
		started = true
		hd := c.Writer.Header()
		hd.Set("Content-Type", xlsxContentType)
		hd.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": h.svc.exportFilename(filePrefix)}))
		hd.Set("Cache-Control", "no-store")
		hd.Set("X-Content-Type-Options", "nosniff")
		c.Status(http.StatusOK)
		return c.Writer
	})
	h.svc.recordExport(c, module, start, err)
	if err == nil {
		return
	}
	if !started {
		response.Fail(c, err)
		return
	}
	// 响应头已发出，无法再改状态码；记录日志，客户端会拿到不完整的文件
	if h.svc.log != nil {
		h.svc.log.Warn("log: export failed midway", zap.String("module", module), zap.Int("rows", n), zap.Error(err))
	}
}

// ExportOper GET /oper-logs/export
func (h *Handler) ExportOper(c *gin.Context) {
	h.export(c, "操作日志", "oper-log", h.svc.ExportOper)
}

// ExportLogin GET /login-logs/export
func (h *Handler) ExportLogin(c *gin.Context) {
	h.export(c, "登录日志", "login-log", h.svc.ExportLogin)
}
