package region

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/cluster"
	"nexadmin/core/response"
)

// Handler 是省市区数据同步的 HTTP 层。
type Handler struct {
	svc *Service
	bus *cluster.Bus
}

type syncRequest struct {
	Source string `json:"source"`
}

// Status GET /regions/status
func (h *Handler) Status(c *gin.Context) {
	response.OK(c, h.svc.Status())
}

// Sync POST /regions/sync：拉取数据源、校验后落库并立即生效。
func (h *Handler) Sync(c *gin.Context) {
	var q syncRequest
	if err := c.ShouldBindJSON(&q); err != nil && !errors.Is(err, io.EOF) {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	actor := ""
	if cl := auth.ClaimsFrom(c); cl != nil {
		actor = cl.Username
	}
	res, err := h.svc.Sync(c.Request.Context(), strings.TrimSpace(q.Source), actor)
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.bus.Publish(busKind, nil)
	response.OK(c, res)
}

// Reset POST /regions/reset：删除同步快照，退回随包发布的数据。
func (h *Handler) Reset(c *gin.Context) {
	if _, err := h.svc.Reset(c.Request.Context()); err != nil {
		response.Fail(c, err)
		return
	}
	h.bus.Publish(busKind, nil)
	response.OK(c, h.svc.Status())
}

// reload 是其它实例同步成功后的回调：重新从库里装载快照。
func (h *Handler) reload() {
	h.svc.Load(context.Background())
}
