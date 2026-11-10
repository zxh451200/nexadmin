package devicehub

// 指令下发：写寄存器/线圈（Modbus）或 publish 下行（MQTT）。
//
// 安全设计（指令是高危操作，比采集严格一档）：
//   - 独立权限码 devicehub:control（handler 层把关），前端二次确认；
//   - 只允许 rw=rw 的测点；请求先落 Command 行（审计），worker 异步执行并回执；
//   - 每条指令一条操作日志由 handler 的 OperLogMiddleware 记录，这里只管执行。

import (
	"context"
	"errors"
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

// commandLoop 是指令执行 worker：每秒认领一批 queued 指令，按设备分组并发执行。
// 超时 5s 没回执判 timeout（见 execCommand）。由 plugin.Start 起，ctx 取消即退出。
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

// cmdScanBatch 是一轮扫描的上限：够覆盖「一批控制点同时下发」的突发，
// 又不至于把积压几万条的队列一次拉进内存。
const cmdScanBatch = 200

// cmdDeviceSem 限制同时在执行的设备数。一台设备一个 goroutine，掉线设备的每条指令都要等满 5s，
// 设备多时不设闸门会起上千个 goroutine 各自开连接做 IO；8 台并行已能把
// 「一台掉线堵住全局」收敛成「最多堵住 1/8 的并发额度」。
var cmdDeviceSem = make(chan struct{}, 8)

// drainCommands 认领当前 queued 指令（一轮最多 200 条），按设备分组并发执行。
//
// 并发粒度取「设备」：改造前是单循环顺序执行，一台掉线设备每条指令等满 5s，
// 排在它后面的所有设备的指令一起被堵住。而同一台设备只有一条连接（Modbus 尤甚），
// 写操作必须串行，所以组内按 id 顺序串行、组间并行。
//
// 不等待 goroutine：下一轮（1s 后）靠 cmdInFlight 去重跳过还在跑的单号，
// 单轮耗时不再取决于最慢的设备。
func (s *Service) drainCommands(ctx context.Context) {
	if ctx.Err() != nil {
		return // 插件在停：不再认领新指令
	}
	var cmds []Command
	if err := s.db.WithContext(ctx).Where("status = ?", CmdQueued).Order("id").Limit(cmdScanBatch).Find(&cmds).Error; err != nil {
		s.logWarn("devicehub: load commands failed", "err", err.Error())
		return
	}
	// 分组时保持 id 升序（cmds 已按 id 排序），devices 记首现顺序，让每轮的并发顺序稳定可读。
	devices := make([]idgen.ID, 0, 8)
	groups := make(map[idgen.ID][]*Command, 8)
	var roundClaimed []idgen.ID
	for i := range cmds {
		if ctx.Err() != nil {
			// 认领到一半插件在停：本轮已认领但还没派发的单号必须回滚，
			// 否则它们会永远留在 cmdInFlight 里，重启后的下一轮再也认领不到。
			s.releaseCmdClaims(roundClaimed)
			return
		}
		cmd := &cmds[i]
		s.cmdMu.Lock()
		if s.cmdInFlight[cmd.ID] {
			// 上一轮已经认领、还在执行：跳过。这里的标记属于另一个 goroutine，
			// 绝不能在这里删——删了下一轮会把它再认领一次，同一台设备上出现并发写。
			s.cmdMu.Unlock()
			continue
		}
		s.cmdInFlight[cmd.ID] = true
		s.cmdMu.Unlock()
		roundClaimed = append(roundClaimed, cmd.ID)
		if _, ok := groups[cmd.DeviceID]; !ok {
			devices = append(devices, cmd.DeviceID)
		}
		groups[cmd.DeviceID] = append(groups[cmd.DeviceID], cmd)
	}
	for _, deviceID := range devices {
		go s.runDeviceCommands(ctx, groups[deviceID])
	}
}

// releaseCmdClaims 清掉本 goroutine 认领过的单号。只删传进来的单号——标记是「谁认领谁释放」，
// 别人的在途标记由别人的 defer 清，这里不能一把清空整个 map。
func (s *Service) releaseCmdClaims(ids []idgen.ID) {
	s.cmdMu.Lock()
	for _, id := range ids {
		delete(s.cmdInFlight, id)
	}
	s.cmdMu.Unlock()
}

// runDeviceCommands 执行一台设备本轮认领到的指令：组内按 id 串行，组结束释放本组全部认领标记。
//
// 在途语义只放内存（cmdInFlight）而不新增 status=running：指令状态是前端状态映射与历史列表的
// 一部分（queued/done/failed/timeout 四态），多一个中间态要同时改前端映射、筛选与统计口径，
// 历史数据里还会留下进程重启时永远停在 running 的悬挂行；内存标记进程内有效、重启即归零，
// queued 行自然被下一轮重新认领。
func (s *Service) runDeviceCommands(ctx context.Context, cmds []*Command) {
	claimed := make([]idgen.ID, 0, len(cmds))
	for _, cmd := range cmds {
		claimed = append(claimed, cmd.ID)
	}
	// 组结束（正常跑完 / ctx 取消 / 拿不到信号量）都要释放本组标记，下一轮才能重新认领未执行的单。
	defer s.releaseCmdClaims(claimed)

	// 信号量获取要能被 ctx 打断：插件停的时候不能卡在这里等别的设备跑完 5s 超时。
	// 拿不到就返回，defer 回滚本组标记，指令保持 queued，重启后重新排队。
	select {
	case cmdDeviceSem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-cmdDeviceSem }()

	for _, cmd := range cmds {
		select {
		case <-ctx.Done():
			return // 剩余指令不回执：仍是 queued，插件重启后继续
		default:
		}
		s.execCommand(ctx, cmd)
	}
}

// execCommand 执行单条指令：设备驱动 → 写 → 回执。
func (s *Service) execCommand(ctx context.Context, cmd *Command) {
	var d Device
	if err := s.db.WithContext(ctx).Take(&d, "id = ?", cmd.DeviceID).Error; err != nil {
		s.finishCommand(ctx, cmd, "", CmdFailed, s.text("设备不存在或已被删除", "Device no longer exists"))
		return
	}
	if !d.Enabled {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, s.text("设备已停用", "Device is disabled"))
		return
	}
	var p Point
	if err := s.db.WithContext(ctx).Take(&p, "id = ?", cmd.PointID).Error; err != nil {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, s.text("测点不存在或已被删除", "Point no longer exists"))
		return
	}
	drv, err := driver.New(d.Protocol, d.Endpoint, []byte(d.Config))
	if err != nil {
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdFailed, err.Error())
		return
	}
	execCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// 驱动内部有自己的网络超时（Modbus 默认 2s、S7 2s、OPC UA 5s），且多数阻塞在
	// 自己的 socket deadline 上、不认 execCtx。放在单独的 goroutine 里执行并 select
	// 到点就回 timeout：否则一台设备的慢 IO 会占住整组到驱动自己放弃为止，5s 执行窗形同虚设。
	// 结果通道带缓冲：超时返回后 goroutine 仍可把迟到结果写进去并自行关闭连接，不泄漏。
	done := make(chan error, 1)
	go func() {
		defer func() { _ = drv.Close() }()
		if err := drv.Connect(execCtx); err != nil {
			done <- err
			return
		}
		done <- drv.Write(execCtx, p.Address, p.DataType, cmd.RawValue)
	}()
	select {
	case <-execCtx.Done():
		// 驱动可能仍在写：回执先按超时落库，迟到结果不覆盖状态（设备端是否真执行以现场为准）。
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdTimeout, s.text("执行超时（5 秒）", "Timed out after 5 seconds"))
	case err := <-done:
		if err != nil {
			status, result := s.cmdExecResult(err)
			s.finishCommand(ctx, cmd, d.DeviceNo, status, result)
			return
		}
		s.finishCommand(ctx, cmd, d.DeviceNo, CmdDone, "")
	}
}

// cmdExecResult 把执行错误翻译成回执（状态 + 文案）：5s 执行窗到点（Connect/Write 拿到
// DeadlineExceeded，驱动用 %w 包装也算）归 timeout，其余（拒连、寄存器非法、写被拒）归 failed。
// 两者在运维口径上不是一回事——混成 failed 就看不出「设备慢/掉线」与「参数写错」。
// 文案走 s.text：指令协程没有请求上下文，跟随应用默认语言。
func (s *Service) cmdExecResult(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return CmdTimeout, s.text("执行超时（5 秒）", "Timed out after 5 seconds")
	}
	return CmdFailed, err.Error()
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
