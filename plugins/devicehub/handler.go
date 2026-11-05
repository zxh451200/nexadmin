package devicehub

// HTTP 路由（坐席端）。权限码：devicehub:list 查看（含实时/历史/导出/配置读取）、
// devicehub:edit 管理设备/测点/告警规则、devicehub:control 指令下发（高危独立 + 操作日志）。
// 访客侧没有端点——工业数据不对外。

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

type handler struct {
	svc *Service
	log *zap.Logger
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// parseIDParam 解析路径里的数字 ID。
func parseIDParam(s string) (idgen.ID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, response.BadRequest("error.bad_request")
	}
	return idgen.ID(v), nil
}

// claimsOperator 取当前登录坐席（指令下发单的 operator 落库用）。
func claimsOperator(c *gin.Context) (idgen.ID, string) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		return 0, "agent"
	}
	name := cl.Username
	if name == "" {
		name = "agent"
	}
	return cl.UserID, name
}

// Register 注册全部路由（由 plugin.go 的 module 调用）。
func (h *handler) Register(r *gin.RouterGroup, d *core.Deps) {
	g := r.Group("/plugins/devicehub")
	// 设备台账/测点/读数/指令/告警七张表全嵌 PlatformModel，一张都没有 tenant_id：
	// 任何持 devicehub:list 的租户都能列出并导出别的租户的设备与历史读数，
	// 而 devicehub:control 还能对别人的设备下发指令——那已经不是泄露而是越权操作。
	// 与在线客服、官网同一判据（表不分租户 → 读也是暴露），整个后台只对平台调用方开放。
	// 真要按租户开：七张表补 tenant_id（三引擎迁移 + 唯一键改形），采集侧再带上归属来源。
	g.Use(core.RequirePlatformFeature())

	// 设备
	g.GET("/devices", core.Perm("devicehub:list"), h.listDevices)
	g.POST("/devices", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "create"), h.saveDevice)
	g.PUT("/devices/:no", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "update"), h.saveDevice)
	g.DELETE("/devices/:no", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "delete"), h.deleteDevice)

	// 测点
	g.GET("/devices/:no/points", core.Perm("devicehub:list"), h.listPoints)
	g.POST("/devices/:no/points", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "create"), h.savePoint)
	g.PUT("/devices/:no/points/:pid", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "update"), h.savePoint)
	g.DELETE("/devices/:no/points/:pid", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "delete"), h.deletePoint)

	// 历史 / 导出
	g.GET("/devices/:no/history", core.Perm("devicehub:list"), h.history)
	g.GET("/devices/:no/history-export", core.Perm("devicehub:list"), h.historyExport)

	// 指令（高危独立权限 + 操作日志）
	g.GET("/devices/:no/commands", core.Perm("devicehub:list"), h.listCommands)
	g.POST("/devices/:no/commands", core.Perm("devicehub:control"), d.OperLogMiddleware("devicehub", "control"), h.createCommand)

	// 告警
	g.GET("/alarms", core.Perm("devicehub:list"), h.listAlarms)
	g.GET("/points/:pid/rules", core.Perm("devicehub:list"), h.listRules)
	g.POST("/points/:pid/rules", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "create"), h.saveRule)
	g.PUT("/rules/:id", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "update"), h.saveRule)
	g.DELETE("/rules/:id", core.Perm("devicehub:edit"), d.OperLogMiddleware("devicehub", "delete"), h.deleteRule)
}

// deviceIDByNo 按设备号查 ID；查不到直接回 404。
func (h *handler) deviceIDByNo(c *gin.Context) (idgen.ID, bool) {
	var d Device
	if err := h.svc.db.WithContext(c.Request.Context()).Select("id").Where("device_no = ?", c.Param("no")).Take(&d).Error; err != nil {
		response.Fail(c, response.NotFound("devicehub.device_not_found"))
		return 0, false
	}
	return d.ID, true
}

// saveDevice 是 POST（新建）/ PUT（更新）的共用实现：POST 无 :no，PUT 从路径取。
func (h *handler) saveDevice(c *gin.Context) {
	var in DeviceInput
	if !response.BindJSON(c, &in) {
		return
	}
	var id idgen.ID
	if raw := c.Param("no"); raw != "" {
		var d Device
		if err := h.svc.db.WithContext(c.Request.Context()).Select("id").Where("device_no = ?", raw).Take(&d).Error; err != nil {
			response.Fail(c, response.NotFound("devicehub.device_not_found"))
			return
		}
		id = d.ID
	}
	row, err := h.svc.SaveDevice(c.Request.Context(), id, in)
	respond(c, row, err)
}

func (h *handler) listDevices(c *gin.Context) {
	rows, err := h.svc.ListDevices(c.Request.Context())
	respond(c, rows, err)
}

func (h *handler) deleteDevice(c *gin.Context) {
	id, ok := h.deviceIDByNo(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteDevice(c.Request.Context(), id))
}

func (h *handler) listPoints(c *gin.Context) {
	id, ok := h.deviceIDByNo(c)
	if !ok {
		return
	}
	rows, err := h.svc.ListPoints(c.Request.Context(), id)
	respond(c, rows, err)
}

func (h *handler) savePoint(c *gin.Context) {
	var in PointInput
	if !response.BindJSON(c, &in) {
		return
	}
	id, ok := h.deviceIDByNo(c)
	if !ok {
		return
	}
	var pid idgen.ID
	if raw := c.Param("pid"); raw != "" {
		v, err := parseIDParam(raw)
		if err != nil {
			return
		}
		pid = v
	}
	row, err := h.svc.SavePoint(c.Request.Context(), id, pid, in)
	respond(c, row, err)
}

func (h *handler) deletePoint(c *gin.Context) {
	id, ok := h.deviceIDByNo(c)
	if !ok {
		return
	}
	pid, err := parseIDParam(c.Param("pid"))
	if err != nil {
		return
	}
	respond(c, nil, h.svc.DeletePoint(c.Request.Context(), id, pid))
}

func (h *handler) history(c *gin.Context) {
	if _, ok := h.deviceIDByNo(c); !ok {
		return
	}
	var q HistoryQuery
	if !response.BindQuery(c, &q) {
		return
	}
	rows, bucket, err := h.svc.History(c.Request.Context(), q)
	respond(c, gin.H{"rows": rows, "bucket": bucket}, err)
}

func (h *handler) historyExport(c *gin.Context) {
	var q HistoryQuery
	if !response.BindQuery(c, &q) {
		return
	}
	d, p, err := h.svc.pointAndDeviceByNo(c.Request.Context(), c.Param("no"), q.PointID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	rows, _, err := h.svc.History(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="history-`+d.DeviceNo+`.xlsx"`)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err := h.svc.WriteHistoryXLSX(c.Request.Context(), c.Writer, d, p, rows); err != nil {
		response.Fail(c, err)
	}
}

func (h *handler) listCommands(c *gin.Context) {
	id, ok := h.deviceIDByNo(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	rows, err := h.svc.ListCommands(c.Request.Context(), id, limit)
	respond(c, rows, err)
}

func (h *handler) createCommand(c *gin.Context) {
	var in CommandInput
	if !response.BindJSON(c, &in) {
		return
	}
	uid, name := claimsOperator(c)
	in.OperatorID, in.OperatorName = uid, name
	row, err := h.svc.CreateCommand(c.Request.Context(), in)
	respond(c, row, err)
}

func (h *handler) listAlarms(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	var deviceID idgen.ID
	if raw := c.Query("deviceNo"); raw != "" {
		var d Device
		if err := h.svc.db.WithContext(c.Request.Context()).Select("id").Where("device_no = ?", raw).Take(&d).Error; err == nil {
			deviceID = d.ID
		}
	}
	rows, err := h.svc.ListAlarmEvents(c.Request.Context(), deviceID, limit)
	respond(c, rows, err)
}

func (h *handler) listRules(c *gin.Context) {
	pid, err := parseIDParam(c.Param("pid"))
	if err != nil {
		return
	}
	rows, err := h.svc.ListAlarmRules(c.Request.Context(), pid)
	respond(c, rows, err)
}

func (h *handler) saveRule(c *gin.Context) {
	var in AlarmRuleInput
	if !response.BindJSON(c, &in) {
		return
	}
	var id idgen.ID
	if raw := c.Param("id"); raw != "" {
		v, perr := parseIDParam(raw)
		if perr != nil {
			return
		}
		id = v
	} else if pid, perr := parseIDParam(c.Param("pid")); perr == nil {
		in.PointID = pid
	}
	row, err := h.svc.SaveAlarmRule(c.Request.Context(), id, in)
	respond(c, row, err)
}

func (h *handler) deleteRule(c *gin.Context) {
	id, err := parseIDParam(c.Param("id"))
	if err != nil {
		return
	}
	respond(c, nil, h.svc.DeleteAlarmRule(c.Request.Context(), id))
}
