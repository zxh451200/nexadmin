package backup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/auth"
	"nexadmin/core/ctl"
	"nexadmin/core/response"
	logmod "nexadmin/modules/log"
	"nexadmin/pkg/idgen"
)

// auditModule 是操作日志里的模块名。
const auditModule = "数据备份"

// Handler 是备份的 HTTP 层，只做参数解析和响应。
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

// fail 把业务错误映射成 HTTP 响应。
func fail(c *gin.Context, err error) {
	var un *UnavailableError
	var tm *ctl.ToolMissingError
	switch {
	case errors.As(err, &tm):
		// 缺工具是可预期状态：文案按当前语言从字典取，工具名作为参数
		e := response.Unavailable("error.backup_tool_missing", strings.Join(tm.Names, " / "))
		e.Reason = "BACKUP_TOOL_MISSING"
		response.Fail(c, e)
	case errors.As(err, &un):
		// Reason 来自底层工具/连接校验（可能是动态中文），只进日志；给用户的是本地化文案。
		e := response.Unavailable("error.unavailable")
		if errors.Is(err, ErrIsolationUnsupported) {
			e = response.Unavailable("backup.isolation_unsupported")
		}
		e = e.Wrap(un)
		e.Reason = "BACKUP_TOOL_MISSING"
		response.Fail(c, e)
	case errors.Is(err, ErrNotFound):
		response.Fail(c, response.NotFound("error.not_found").Wrap(err))
	case errors.Is(err, ErrBusy):
		response.Fail(c, response.Conflict("backup.busy").Wrap(err))
	case errors.Is(err, ErrNotReady):
		response.Fail(c, response.Conflict("backup.not_ready").Wrap(err))
	default:
		response.Fail(c, err)
	}
}

// superOnly 整库备份跨租户，只允许超管（并且仍然要有对应权限点）。
func superOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if cl := auth.ClaimsFrom(c); cl == nil || !cl.Super {
			response.Fail(c, response.Forbidden("backup.super_only"))
			return
		}
		c.Next()
	}
}

// audit 记录操作日志（模块被停用时跳过）。在请求时才取 Service，避免注册顺序问题。
func audit(typ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if s := logmod.Default(); s != nil {
			s.Middleware(auditModule, typ)(c)
			return
		}
		c.Next()
	}
}

// List GET /backups
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), page, size)
	if err != nil {
		fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Status GET /backups/status
func (h *Handler) Status(c *gin.Context) {
	st, err := h.svc.Status(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, st)
}

// GetConfig GET /backups/config
func (h *Handler) GetConfig(c *gin.Context) {
	set, err := h.svc.Setting(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, ConfigInput{Schedule: set.Schedule, KeepDays: set.KeepDays, KeepCount: set.KeepCount})
}

// PutConfig PUT /backups/config
func (h *Handler) PutConfig(c *gin.Context) {
	var in ConfigInput
	if !response.BindJSON(c, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		response.Fail(c, response.BadRequest("backup.config_invalid").Wrap(err))
		return
	}
	set, err := h.svc.SaveSetting(c.Request.Context(), in)
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, ConfigInput{Schedule: set.Schedule, KeepDays: set.KeepDays, KeepCount: set.KeepCount})
}

// Create POST /backups：立即备份，异步执行，返回 status=running 的记录。
func (h *Handler) Create(c *gin.Context) {
	var by idgen.ID
	if cl := auth.ClaimsFrom(c); cl != nil {
		by = cl.UserID
	}
	rec, err := h.svc.Start(c.Request.Context(), SourceManual, by)
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, rec)
}

// Delete DELETE /backups/:id
func (h *Handler) Delete(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	response.OK(c, nil)
}

// Download GET /backups/:id/download：文件名取自库里的记录并再次校验（不信任任何请求里的路径），
// 响应头带 X-Checksum-SHA256，下载本身写操作日志（二进制响应体不入日志）。
func (h *Handler) Download(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	rec, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	if rec.Status != StatusSuccess {
		fail(c, ErrNotReady)
		return
	}
	path, err := h.svc.Path(rec)
	if err != nil {
		response.Fail(c, response.BadRequest("backup.file_invalid").Wrap(err))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		response.Fail(c, response.NotFound("backup.file_missing").Wrap(err))
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		response.Fail(c, response.NotFound("backup.file_unreadable"))
		return
	}
	h.writeDownloadAudit(c, rec)
	hd := c.Writer.Header()
	hd.Set("Content-Type", "application/gzip")
	hd.Set("Content-Disposition", `attachment; filename="`+rec.FileName+`"`)
	hd.Set("X-Checksum-SHA256", rec.SHA256)
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, rec.FileName, st.ModTime(), f)
}

// writeDownloadAudit 同步写下载操作日志（模块被停用时跳过）。
// 同步而非 goroutine：备份下载是低频、仅超管的重操作，多一次 INSERT 的延迟可忽略，
// 却保证审计在下载真正开始前就已落库、进程退出也不会丢，且不再有失控的并发写。
// 写失败不阻断合法下载，但要记日志，不能静默丢弃。ctx 脱离请求取消，避免客户端断开丢掉这条审计。
func (h *Handler) writeDownloadAudit(c *gin.Context, rec *Record) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return
	}
	write := h.svc.auditWrite
	if write == nil {
		s := logmod.Default()
		if s == nil {
			return // 日志模块被停用
		}
		write = s.WriteOper
	}
	l := &logmod.OperLog{Module: auditModule, Type: "download", Operator: cl.Username, IP: c.ClientIP(), Status: logmod.StatusSuccess,
		Method: c.Request.Method, URL: c.Request.URL.Path, Params: `{"id":` + strconv.Quote(rec.ID.String()) + `,"file":` + strconv.Quote(rec.FileName) + `}`}
	l.TenantID = cl.TenantID
	if err := write(context.WithoutCancel(c.Request.Context()), l); err != nil && h.svc.log != nil {
		h.svc.log.Warn("backup: download audit write failed", zap.String("file", rec.FileName), zap.Error(err))
	}
}
