package identity

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 权限码：identity:list 查看；identity:edit 增删改与启停；identity:test 测试连接 / 测试用户登录；
// identity:sync 目录同步；identity:match 开启 matchExisting（外部账号按用户名绑定已有本地账号）。

type handler struct{ svc *Service }

func paramID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return id, true
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// audit 写一条操作日志（沿用 sys_oper_log）。params 必须是不含口令的字段。
func (h *handler) audit(c *gin.Context, typ string, start time.Time, params map[string]any, err error) {
	e := auditEntry{Type: typ, IP: c.ClientIP(), Method: c.Request.Method, URL: c.Request.URL.Path, Params: params, Err: err, Cost: time.Since(start)}
	if cl := auth.ClaimsFrom(c); cl != nil {
		e.Operator = cl.Username
	}
	h.svc.record(c.Request.Context(), e)
}

// GET /plugins/identity/sources
func (h *handler) list(c *gin.Context) {
	v, err := h.svc.List(c.Request.Context())
	respond(c, v, err)
}

// GET /plugins/identity/sources/:id
func (h *handler) get(c *gin.Context) {
	if id, ok := paramID(c); ok {
		v, err := h.svc.Get(c.Request.Context(), id)
		respond(c, v, err)
	}
}

// requestCtx 返回带“调用者是否拥有 identity:match”标记的请求 context。
func requestCtx(c *gin.Context) context.Context {
	ctx := c.Request.Context()
	if hasPerm(c, permMatch) {
		return withMatchPerm(ctx)
	}
	return ctx
}

// hasPerm 与 middleware.Perm 同一套判定：超管放行；开放应用看令牌权限；其余读 Provider.Profile 的最新权限。
func hasPerm(c *gin.Context, code string) bool {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return false
	}
	if cl.Super {
		return true
	}
	if cl.Principal == auth.PrincipalApp {
		return auth.HasPerm(cl.Perms, code)
	}
	svc := auth.Default()
	if svc == nil || svc.Provider() == nil {
		return false
	}
	u, err := svc.Profile(c.Request.Context(), cl)
	return err == nil && auth.HasPerm(u.Perms, code)
}

func (h *handler) save(c *gin.Context, id idgen.ID, typ string) {
	start := time.Now()
	var f SourceForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.Save(requestCtx(c), id, f)
	p := map[string]any{"name": f.Name, "type": f.Type, "enabled": f.Enabled}
	if id != 0 {
		p["id"] = id.String()
	}
	if v != nil {
		p["id"] = v.ID.String()
	}
	h.audit(c, typ, start, p, err)
	respond(c, v, err)
}

// POST /plugins/identity/sources
func (h *handler) create(c *gin.Context) { h.save(c, 0, "create") }

// PUT /plugins/identity/sources/:id
func (h *handler) update(c *gin.Context) {
	if id, ok := paramID(c); ok {
		h.save(c, id, "update")
	}
}

// PUT /plugins/identity/sources/:id/enabled
func (h *handler) setEnabled(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	start := time.Now()
	v, err := h.svc.SetEnabled(requestCtx(c), id, body.Enabled)
	h.audit(c, "update", start, map[string]any{"id": id.String(), "enabled": body.Enabled}, err)
	respond(c, v, err)
}

// DELETE /plugins/identity/sources/:id
func (h *handler) remove(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	start := time.Now()
	err := h.svc.Delete(c.Request.Context(), id)
	h.audit(c, "delete", start, map[string]any{"id": id.String()}, err)
	respond(c, nil, err)
}

// POST /plugins/identity/sources/:id/test-connection
func (h *handler) testConnection(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	start := time.Now()
	v, err := h.svc.TestConnection(c.Request.Context(), id)
	h.audit(c, "test", start, map[string]any{"id": id.String(), "kind": "connection", "ok": v != nil && v.OK}, err)
	respond(c, v, err)
}

// POST /plugins/identity/sources/:id/test-login  {"username":"","password":""}
func (h *handler) testLogin(c *gin.Context) {
	id, ok := paramID(c)
	if !ok {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !response.BindJSON(c, &body) {
		return
	}
	start := time.Now()
	v, err := h.svc.TestLogin(c.Request.Context(), id, body.Username, body.Password)
	h.audit(c, "test", start, map[string]any{"id": id.String(), "kind": "login", "username": clip(body.Username, 64), "ok": v != nil && v.OK}, err)
	respond(c, v, err)
}
