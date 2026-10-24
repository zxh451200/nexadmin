package clienterror

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/middleware"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

func init() { core.RegisterBuiltin(module{}) }

// PermList 与服务监控页一致：前端错误是服务监控页下的一个标签页。
const PermList = "serverMonitor:list"

type module struct{}

func (module) Name() string { return "client-error" }

// Register 上报接口挂在 d.Public（登录与未登录均可），先按 IP 限流（规则 client_error），再限制请求体；
// 列表接口需要 serverMonitor:list。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := NewService(d.DB, d.Log)
	h := &Handler{svc: svc}
	rl := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "client_error", middleware.ByIP())
	d.Public.POST("/client-errors", rl, h.Report)
	r.GET("/monitor/client-errors", core.Perm(PermList), h.List)

	stop := make(chan struct{})
	if d.OnClose != nil {
		d.OnClose(func() { close(stop) })
	}
	go svc.purgeLoop(stop)
}

// Handler 是 HTTP 层。
type Handler struct{ svc *Service }

func tooLarge() *response.Error {
	return response.New(http.StatusRequestEntityTooLarge, "error.payload_too_large").WithMsg("request body too large")
}

// Report POST /client-errors
func (h *Handler) Report(c *gin.Context) {
	if c.Request.ContentLength > MaxBody {
		response.Fail(c, tooLarge())
		return
	}
	// ContentLength 可能是 -1（分块传输）或被谎报：用 MaxBytesReader 兜底，读到上限就报错。
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, MaxBody))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			response.Fail(c, tooLarge())
			return
		}
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	var uid idgen.ID
	var username string
	if cl := auth.ClaimsFrom(c); cl != nil {
		uid, username = cl.UserID, cl.Username
	}
	switch err := h.svc.Ingest(c.Request.Context(), uid, username, rep, c.GetHeader("User-Agent")); {
	case err == nil:
		response.OK(c, nil)
	case errors.Is(err, ErrEmpty):
		response.Fail(c, response.BadRequest("error.bad_request"))
	case errors.Is(err, ErrThrottled):
		middleware.TooManyRequests(c, time.Minute)
	default:
		response.Fail(c, err)
	}
}

// List GET /monitor/client-errors?keyword=&page=&pageSize=
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	rows, total, err := h.svc.List(c.Request.Context(), Query{Keyword: c.Query("keyword"), Page: page, PageSize: size})
	if err != nil {
		response.Fail(c, err)
		return
	}
	if rows == nil {
		rows = []ClientError{}
	}
	response.Page(c, rows, total, page, size)
}
