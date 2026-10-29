package log

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// StatsOper GET /oper-logs/stats：操作日志聚合。筛选条件与数据范围与列表接口完全一致，
// 这样「统计卡上的数」与「翻列表看到的数」不会各说各话。
func (h *Handler) StatsOper(c *gin.Context) {
	q := query(c)
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	q.Scope = sc
	st, err := h.svc.OperStats(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, st)
}

// StatsLogin GET /login-logs/stats：登录日志聚合。
func (h *Handler) StatsLogin(c *gin.Context) {
	q := query(c)
	sc, ok := h.scope(c)
	if !ok {
		return
	}
	q.Scope = sc
	st, err := h.svc.LoginStats(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, st)
}
