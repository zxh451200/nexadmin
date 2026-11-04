package devicehub

// 指令下发：写寄存器/线圈（Modbus）或 publish 下行（MQTT）。
//
// 安全设计（指令是高危操作，比采集严格一档）：
//   - 独立权限码 devicehub:control（handler 层把关），前端二次确认；
//   - 只允许 rw=rw 的测点；请求先落 Command 行（审计），worker 异步执行并回执；
//   - 每条指令一条操作日志由 handler 的 OperLogMiddleware 记录，这里只管执行。

import (
	"context"
	"strconv"
	"time"

	"nexadmin/core/response"
	"nexadmin/core/ws"
	"nexadmin/pkg/idgen"
	"nexadmin/plugins/devicehub/driver"
)

// CommandInput 是指令下发请求。
type CommandInput struct {
	DeviceNo string   `json:"deviceNo"`
	PointID  idgen.ID `json:"pointId"`
	// Value 是界面上按工程量填的值（scale 已由后端还原成 raw：raw = value * scale）。
	Value float64 `json:"value"`
	// RawValue 是直接下发的原始值（bool/整数/浮点字符串）。与 Value 二选一，RawValue 优先。
	RawValue     string   `json:"rawValue"`
	OperatorID   idgen.ID `json:"-"`
	OperatorName string   `json:"-"`
}

// CreateCommand 校验并落一条 queued 指令；执行由 commandLoop 异步完成。
func (s *Service) CreateCommand(ctx context.Context, in CommandInput) (*Command, error) {
	var d Device
	if err := s.db.WithContext(ctx).Where("device_no = ?", in.DeviceNo).Take(&d).Error; err != nil {
		return nil, response.NotFound("devicehub.device_not_found")
	}
	if !d.Enabled {
		return nil, response.BadRequest("devicehub.device_disabled")
	}
	var p Point
	if err := s.db.WithContext(ctx).Where("id = ? AND device_id = ?", in.PointID, d.ID).Take(&p).Error; err != nil {
		return nil, response.NotFound("devicehub.point_not_found")
	}
	if p.RW != RWBoth {
		return nil, response.BadRequest("devicehub.point_readonly")
	}
	raw := in.RawValue
	if raw == "" {
		// 工程量 → 原始值：raw = value * scale（显示 = raw / scale 的逆运算）
		raw = strconv.FormatFloat(in.Value*p.Scale, 'f', -1, 64)
	}
	row := Command{
		DeviceID: d.ID, PointID: p.ID, RawValue: raw,
		OperatorID: in.OperatorID, OperatorName: in.OperatorName,
	}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// commandLoop 是指令执行 worker：拉 queued 指令逐条执行（串行——Modbus 单连接串行语义天然匹配）。
// 超时 5s 没回执判 timeout。由 plugin.Start 起，ctx 取消即退出。
func (s *Service) commandLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.drainCommands(ctx)
		}
	}
}

// drainCommands 处理当前所有 queued 指令（一轮最多 20 条，防止单轮过长）。
func (s *Service) drainCommands(ctx context.Context) {
	var cmds []Command
	if err := s.db.WithContext(ctx).Where("status = ?", CmdQueued).Order("id").Limit(20).Find(&cmds).Error; err != nil {
		s.logWarn("devicehub: load commands failed", "err", err.Error())
		return
	}
	for i := range cmds {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.execCommand(ctx, &cmds[i])
	}
}

// execCommand 执行单条指令：设备驱动 → 写 → 回执。
func (s *Service) execCommand(ctx context.Context, cmd *Command) {
	var d Device
	if err := s.db.WithContext(ctx).Take(&d, "id = ?", cmd.DeviceID).Error; err != nil {
		s.finishCommand(ctx, cmd, "", CmdFailed, "设备不存在或已被删除")
		return
	}
	if !d.Enabled {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, "设备已停用")
		return
	}
	var p Point
	if err := s.db.WithContext(ctx).Take(&p, "id = ?", cmd.PointID).Error; err != nil {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, "测点不存在或已被删除")
		return
	}
	drv, err := driver.New(d.Protocol, d.Endpoint, []byte(d.Config))
	if err != nil {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, err.Error())
		return
	}
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := drv.Connect(execCtx); err != nil {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, err.Error())
		return
	}
	defer func() { _ = drv.Close() }()
	if err := drv.Write(execCtx, p.Address, p.DataType, cmd.RawValue); err != nil {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, err.Error())
		return
	}
	s.finishCommand(ctx, cmd, d.DeviceNo, CmdDone, "")
}

// finishCommand 写回执并推送（前端指令列表轮询/WS 均可感知）。
func (s *Service) finishCommand(ctx context.Context, cmd *Command, deviceNo, status, result string) {
	now := time.Now().UTC()
	updates := map[string]any{"status": status, "result": result, "done_at": now, "updated_at": now}
	if err := s.db.Model(&Command{}).Where("id = ?", cmd.ID).Updates(updates).Error; err != nil {
		s.logWarn("devicehub: finish command failed", "err", err.Error())
		return
	}
	cmd.Status, cmd.Result = status, result
	if deviceNo != "" {
		ws.Publish("device:"+deviceNo, wsFrame{Type: "command", DeviceNo: deviceNo, DeviceID: cmd.DeviceID})
		s.publish(ctx, EventCommand, EventData{
			DeviceNo: deviceNo, DeviceID: int64(cmd.DeviceID), PointID: int64(cmd.PointID),
			Value: rawValue(cmd.RawValue), Message: status + " " + result,
		})
	}
}

// ListCommands 返回设备的指令历史（最新的在前）。
func (s *Service) ListCommands(ctx context.Context, deviceID idgen.ID, limit int) ([]Command, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []Command
	err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// rawValue 把指令原始字符串解析成事件载荷里的数值（解析失败 0，载荷以 message 为准）。
func rawValue(raw string) float64 {
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}
