package devicehub

// 服务层：设备/测点 CRUD、采集落库（onReading）、告警评估、历史查询与导出。
// 权限模型与 ticket 同款四码粒度：devicehub:list 查看（含实时/历史/导出）、
// devicehub:edit 管理设备/测点/告警规则、devicehub:control 指令下发（高危独立）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/core/ws"
	"nexadmin/pkg/idgen"
	"nexadmin/plugins/devicehub/driver"
)

// Service 是设备监控业务层。
type Service struct {
	db        *gorm.DB
	log       logger
	collector *collector
	// notifier 站内信出口与告警收件人解析（nil 安全，测试夹具不注入）；lang 取应用默认语言
	notifier   notifySender
	recipients recipientsFunc
	lang       string
}

// logger 是最小日志面（便于测试注入空实现），与 ticket 同款。
type logger interface {
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// notifySender 是 core/notify 的最小面（测试注入假实现）。
type notifySender interface {
	Send(ctx context.Context, m notify.Msg) error
}

// recipientsFunc 解析告警收件人（持有 devicehub:list 的用户，按租户分组的准备工作）。
type recipientsFunc func(ctx context.Context) ([]alarmRecipient, error)

// NewService 创建服务；采集 worker 由 plugin.Start 起。
func NewService(db *gorm.DB, log logger) *Service {
	s := &Service{db: db, log: log}
	s.collector = newCollector(s)
	return s
}

// WithNotify 注入站内信出口、收件人解析与默认语言；不注入时告警只落库不通知。
func (s *Service) WithNotify(n notifySender, recipients recipientsFunc, lang string) *Service {
	s.notifier, s.recipients, s.lang = n, recipients, lang
	return s
}

func (s *Service) logWarn(msg string, keysAndValues ...any) {
	if s.log != nil {
		s.log.Warn(msg, keysAndValues...)
	}
}

// ---- 设备 CRUD ----

// ListDevices 返回全部设备（含最近值统计用的状态字段）。
func (s *Service) ListDevices(ctx context.Context) ([]Device, error) {
	var rows []Device
	err := s.db.WithContext(ctx).Order("id").Find(&rows).Error
	return rows, err
}

// DeviceInput 是设备表单。Config 是协议 JSON 文本（前端已按协议拼好）。
type DeviceInput struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`
	Config   string `json:"config"`
	Enabled  bool   `json:"enabled"`
}

var validProtocols = map[string]bool{"modbus-tcp": true, "modbus-rtu": true, "mqtt": true, "s7": true, "opcua": true}

// SaveDevice 新建/更新设备；配置变更后重启该设备的采集 worker。
func (s *Service) SaveDevice(ctx context.Context, id idgen.ID, in DeviceInput) (*Device, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Protocol = strings.TrimSpace(in.Protocol)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	in.Config = strings.TrimSpace(in.Config)
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return nil, response.BadRequest("devicehub.name_required")
	case !validProtocols[in.Protocol]:
		return nil, response.BadRequest("devicehub.protocol_invalid")
	case in.Endpoint == "" || len(in.Endpoint) > 255:
		return nil, response.BadRequest("devicehub.endpoint_required")
	}
	// 配置必须是合法 JSON 对象：坏配置到采集循环才爆不如在保存时就拦下
	if in.Config != "" && !json.Valid([]byte(in.Config)) {
		return nil, response.BadRequest("devicehub.config_invalid")
	}
	var row Device
	if id != 0 {
		if err := s.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
			return nil, err
		}
	}
	row.Name, row.Protocol, row.Endpoint, row.Config, row.Enabled = in.Name, in.Protocol, in.Endpoint, in.Config, in.Enabled
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	// 新设备号 = TK 式雪花前缀；Save 前 ID 未定，新建后补一次（device_no 唯一键靠它）
	if row.DeviceNo == "" {
		row.DeviceNo = "DEV" + strconv.FormatInt(int64(row.ID), 10)
		if err := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", int64(row.ID)).Update("device_no", row.DeviceNo).Error; err != nil {
			return nil, err
		}
	}
	if s.collector != nil {
		s.collector.Reload(ctx, row.ID)
	}
	return &row, nil
}

// DeleteDevice 级联删除测点/读数/聚合/指令/告警（设备删除没有保留价值，与 nav 级联同口径）。
//
// 两类子表的键不一样：读数 / 指令 / 告警事件带 device_id，可以直接按设备删；
// 聚合行（plg_devhub_reading_agg）与告警规则只有 point_id，必须顺着测点找，
// 而且要在测点行还在的时候删——先删测点，子查询就一行也查不到，孤儿数据会留在库里。
func (s *Service) DeleteDevice(ctx context.Context, id idgen.ID) error {
	if s.collector != nil {
		s.collector.stopDevice(id)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pointIDs := func() *gorm.DB {
			return tx.Session(&gorm.Session{NewDB: true}).Table("plg_devhub_point").Select("id").Where("device_id = ?", id)
		}
		for _, row := range []any{&ReadingAgg{}, &AlarmRule{}} {
			if err := tx.Where("point_id IN (?)", pointIDs()).Delete(row).Error; err != nil {
				return err
			}
		}
		for _, row := range []any{&Reading{}, &Command{}, &AlarmEvent{}, &Point{}} {
			if err := tx.Where("device_id = ?", id).Delete(row).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&Device{}, "id = ?", id).Error
	})
}

// ---- 测点 CRUD ----

func (s *Service) pointsOf(deviceID idgen.ID) []Point {
	var rows []Point
	err := s.db.Where("device_id = ? AND enabled = ?", deviceID, true).Order("sort, id").Find(&rows).Error
	if err != nil {
		return nil
	}
	return rows
}

// ListPoints 返回设备测点（含最近值）。
func (s *Service) ListPoints(ctx context.Context, deviceID idgen.ID) ([]Point, error) {
	var rows []Point
	err := s.db.WithContext(ctx).Where("device_id = ?", deviceID).Order("sort, id").Find(&rows).Error
	return rows, err
}

// PointInput 是测点表单。
type PointInput struct {
	Name     string  `json:"name"`
	Address  string  `json:"address"`
	Unit     string  `json:"unit"`
	RW       string  `json:"rw"`
	DataType string  `json:"dataType"`
	Scale    float64 `json:"scale"`
	PollMs   int     `json:"pollIntervalMs"`
	Deadband float64 `json:"deadband"`
	Enabled  bool    `json:"enabled"`
	Sort     int     `json:"sort"`
}

var validDataTypes = map[string]bool{"bool": true, "int16": true, "uint16": true, "int32": true, "float32": true}

// SavePoint 新建/更新测点；采集 worker 随设备整体重载。
func (s *Service) SavePoint(ctx context.Context, deviceID idgen.ID, id idgen.ID, in PointInput) (*Point, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	in.Unit = strings.TrimSpace(in.Unit)
	in.RW = strings.TrimSpace(in.RW)
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return nil, response.BadRequest("devicehub.point_name_required")
	case in.Address == "" || len(in.Address) > 128:
		return nil, response.BadRequest("devicehub.point_address_required")
	case in.RW != RWRead && in.RW != RWBoth:
		return nil, response.BadRequest("devicehub.point_rw_invalid")
	case !validDataTypes[in.DataType]:
		return nil, response.BadRequest("devicehub.point_type_invalid")
	case in.Scale <= 0:
		in.Scale = 1
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", deviceID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, response.NotFound("devicehub.device_not_found")
	}
	var row Point
	if id != 0 {
		if err := s.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
			return nil, err
		}
		if row.DeviceID != deviceID {
			return nil, response.NotFound("devicehub.point_not_found")
		}
	}
	row.DeviceID = deviceID
	row.Name, row.Address, row.Unit, row.RW = in.Name, in.Address, in.Unit, in.RW
	row.DataType, row.Scale, row.PollMs, row.Deadband = in.DataType, in.Scale, in.PollMs, in.Deadband
	row.Enabled, row.Sort = in.Enabled, in.Sort
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	s.collector.Reload(ctx, deviceID)
	return &row, nil
}

// DeletePoint 删测点（历史读数保留——趋势数据不因测点删除而丢）。
// 告警规则不保留：规则是配置不是历史，留着就是指向已删测点的死配置，
// 而设备级删除（DeleteDevice）本来也会把规则清掉，两处口径要一致。
func (s *Service) DeletePoint(ctx context.Context, deviceID idgen.ID, id idgen.ID) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND device_id = ?", id, deviceID).Delete(&Point{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return response.NotFound("devicehub.point_not_found")
		}
		return tx.Where("point_id = ?", id).Delete(&AlarmRule{}).Error
	})
	if err != nil {
		return err
	}
	if s.collector != nil {
		s.collector.Reload(ctx, deviceID)
	}
	return nil
}

// ---- 采集落库（collector 回调）----

// wsFrame 是实时推送帧：type=tick（测点值变化）/ status（设备在线状态变化）。
type wsFrame struct {
	Type     string      `json:"type"`
	DeviceNo string      `json:"deviceNo"`
	DeviceID idgen.ID    `json:"deviceId"`
	Status   string      `json:"status,omitempty"`
	Points   []pointTick `json:"points,omitempty"`
}

type pointTick struct {
	PointID idgen.ID  `json:"pointId"`
	Address string    `json:"address"`
	Name    string    `json:"name"`
	Unit    string    `json:"unit"`
	Num     *float64  `json:"num,omitempty"`
	Str     string    `json:"str,omitempty"`
	TS      time.Time `json:"ts"`
}

// onReading 是采集落库的唯一入口：缩放 → 死区 → 落库 → 推送 → 告警。
// address 是测点注册地址；value 是驱动原始值（缩放前）。
func (s *Service) onReading(ctx context.Context, d *Device, address string, raw driver.Value, interval time.Duration) {
	var p Point
	if err := s.db.Where("device_id = ? AND address = ?", d.ID, address).Take(&p).Error; err != nil {
		return // 未注册地址：网关推来的杂项，忽略
	}
	if !p.Enabled {
		return
	}
	now := time.Now().UTC()
	tick := pointTick{PointID: p.ID, Address: p.Address, Name: p.Name, Unit: p.Unit, TS: now}
	switch {
	case raw.Str != "":
		tick.Str = raw.Str
		updates := map[string]any{"last_value_str": raw.Str, "last_ts": now}
		if err := s.db.Model(&Point{}).Where("id = ?", p.ID).Updates(updates).Error; err != nil {
			s.logWarn("devicehub: update last value failed", "err", err.Error())
			return
		}
		s.storeReading(ctx, p, d, now, nil, raw.Str)
	default:
		v := raw.Num / p.Scale
		tick.Num = &v
		// 死区：与上次存储值偏差 ≤ deadband 只刷新 last_*，不落库（秒级采集的存储压缩）
		if p.Deadband > 0 && p.LastValueNum != nil && p.LastTS != nil &&
			now.Sub(*p.LastTS) < 10*time.Minute &&
			abs(*p.LastValueNum-v) <= p.Deadband {
			if err := s.db.Model(&Point{}).Where("id = ?", p.ID).
				Updates(map[string]any{"last_value_num": v, "last_ts": now}).Error; err != nil {
				s.logWarn("devicehub: update last value failed", "err", err.Error())
			}
		} else {
			if err := s.db.Model(&Point{}).Where("id = ?", p.ID).
				Updates(map[string]any{"last_value_num": v, "last_ts": now}).Error; err != nil {
				s.logWarn("devicehub: update last value failed", "err", err.Error())
				return
			}
			s.storeReading(ctx, p, d, now, &v, "")
		}
	}
	s.evaluateAlarms(ctx, d, &p, tick)
	s.publishTick(d, tick)
}

// storeReading 落一条原始读数（雪花 ID 由 GORM 钩子/数据库自增——PlatformModel 统一处理）。
func (s *Service) storeReading(ctx context.Context, p Point, d *Device, ts time.Time, num *float64, str string) {
	row := Reading{PointID: p.ID, DeviceID: d.ID, TS: ts, NumValue: num, StrValue: str}
	if err := s.db.Create(&row).Error; err != nil {
		s.logWarn("devicehub: store reading failed", "err", err.Error())
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ---- 设备在线状态 ----

func (s *Service) setDeviceStatus(ctx context.Context, d *Device, status string) {
	if d.Status == status && status != StatusOnline {
		return // 状态没变就不写库（online 的 last_seen 每轮都刷）
	}
	now := time.Now().UTC()
	updates := map[string]any{"status": status}
	if status == StatusOnline {
		updates["last_seen_at"] = now
	}
	if err := s.db.Model(&Device{}).Where("id = ?", int64(d.ID)).Updates(updates).Error; err != nil {
		s.logWarn("devicehub: update status failed", "err", err.Error())
		return
	}
	if d.Status != status {
		d.Status = status
		ws.Publish("device:feed", wsFrame{Type: "status", DeviceNo: d.DeviceNo, DeviceID: d.ID, Status: status})
		s.publish(ctx, EventStatus, EventData{DeviceNo: d.DeviceNo, DeviceID: int64(d.ID), Message: status})
	}
}

// publishTick 把测点变化推给订阅了 device:<no> 的前端。
func (s *Service) publishTick(d *Device, tick pointTick) {
	ws.Publish("device:"+d.DeviceNo, wsFrame{Type: "tick", DeviceNo: d.DeviceNo, DeviceID: d.ID, Points: []pointTick{tick}})
}

// ---- 历史查询 ----

// HistoryQuery 是历史趋势查询。
type HistoryQuery struct {
	PointID idgen.ID  `json:"pointId" form:"pointId"`
	From    time.Time `json:"from" form:"from"`
	To      time.Time `json:"to" form:"to"`
	// Bucket：raw（原始，48h 内）或 hour（降采样桶，更久远的数据只有它）
	Bucket string `json:"bucket" form:"bucket"`
}

// HistoryRow 是趋势图的一行。
type HistoryRow struct {
	TS  time.Time `json:"ts"`
	Min float64   `json:"min"`
	Max float64   `json:"max"`
	Avg float64   `json:"avg"`
}

// History 查历史趋势：窗口 ≤48h 走原始表，否则走降采样桶。
func (s *Service) History(ctx context.Context, q HistoryQuery) ([]HistoryRow, string, error) {
	if q.PointID == 0 {
		return nil, "", response.BadRequest("devicehub.point_required")
	}
	if q.To.IsZero() {
		q.To = time.Now().UTC()
	}
	if q.From.IsZero() {
		q.From = q.To.Add(-24 * time.Hour)
	}
	bucket := q.Bucket
	if bucket == "" {
		if q.To.Sub(q.From) <= 48*time.Hour {
			bucket = "raw"
		} else {
			bucket = "hour"
		}
	}
	var rows []HistoryRow
	if bucket == "raw" {
		var readings []Reading
		if err := s.db.WithContext(ctx).
			Where("point_id = ? AND ts >= ? AND ts <= ? AND num_value IS NOT NULL", q.PointID, q.From, q.To).
			Order("ts").Limit(5000).Find(&readings).Error; err != nil {
			return nil, "", err
		}
		for _, r := range readings {
			if r.NumValue != nil {
				rows = append(rows, HistoryRow{TS: r.TS, Min: *r.NumValue, Max: *r.NumValue, Avg: *r.NumValue})
			}
		}
		return rows, bucket, nil
	}
	if err := s.db.WithContext(ctx).
		Where("point_id = ? AND bucket >= ? AND bucket <= ?", q.PointID, q.From.Truncate(time.Hour), q.To).
		Order("bucket").Limit(5000).Find(&rows).Error; err != nil {
		return nil, "", err
	}
	return rows, bucket, nil
}

// HistoryRowsForExport 供 xlsx 导出复用（与 History 同源）。
func (s *Service) HistoryRowsForExport(ctx context.Context, q HistoryQuery) ([]HistoryRow, string, error) {
	return s.History(ctx, q)
}

// ---- 告警 ----

var validOps = map[string]bool{"gt": true, "lt": true, "ge": true, "le": true, "eq": true, "ne": true}

// opMatches 评估阈值比较符；未知操作符一律 false（保存时已拦，这里是防御）。
func opMatches(op string, v, threshold float64) bool {
	switch op {
	case "gt":
		return v > threshold
	case "lt":
		return v < threshold
	case "ge":
		return v >= threshold
	case "le":
		return v <= threshold
	case "eq":
		return v == threshold
	case "ne":
		return v != threshold
	}
	return false
}

// evaluateAlarms 在每个新读数上评估该点的启用规则；触发则落事件 + 站内信 + 推送。
func (s *Service) evaluateAlarms(ctx context.Context, d *Device, p *Point, tick pointTick) {
	if tick.Num == nil {
		return // 字符串测点不做阈值告警
	}
	var rules []AlarmRule
	if err := s.db.Where("point_id = ? AND enabled = ?", p.ID, true).Find(&rules).Error; err != nil {
		return
	}
	now := time.Now().UTC()
	for _, r := range rules {
		if !validOps[r.Op] || !opMatches(r.Op, *tick.Num, r.Threshold) {
			continue
		}
		if r.LastFiredAt != nil && now.Sub(*r.LastFiredAt) < time.Duration(r.CooldownSeconds)*time.Second {
			continue // 冷却期内不重复
		}
		if err := s.db.Model(&AlarmRule{}).Where("id = ?", r.ID).Update("last_fired_at", now).Error; err != nil {
			continue
		}
		msg := fmt.Sprintf("%s / %s = %.3g %s（%s %.3g）", d.Name, p.Name, *tick.Num, p.Unit, opLabel(r.Op), r.Threshold)
		event := AlarmEvent{RuleID: r.ID, DeviceID: d.ID, PointID: p.ID, Value: *tick.Num, Level: r.Level, Message: msg}
		if err := s.db.Create(&event).Error; err != nil {
			s.logWarn("devicehub: store alarm event failed", "err", err.Error())
			continue
		}
		s.notifyAlarm(ctx, d, r.Level, msg)
		s.publish(ctx, EventAlarm, EventData{
			DeviceNo: d.DeviceNo, DeviceID: int64(d.ID), PointID: int64(p.ID),
			Address: p.Address, Value: *tick.Num, Level: r.Level, Message: msg,
		})
		ws.Publish("device:"+d.DeviceNo, wsFrame{Type: "alarm", DeviceNo: d.DeviceNo, DeviceID: d.ID,
			Points: []pointTick{{PointID: p.ID, Address: p.Address, Name: p.Name, Unit: p.Unit, Num: tick.Num, TS: now}}})
	}
}

func opLabel(op string) string {
	switch op {
	case "gt":
		return ">"
	case "lt":
		return "<"
	case "ge":
		return ">="
	case "le":
		return "<="
	case "eq":
		return "="
	case "ne":
		return "≠"
	}
	return op
}

// notifyAlarm 告警通知：站内信发给 recipients(ctx) 返回的坐席（按租户分组——sys_message 是租户表）。
// 收件人解析由 plugin.go 注入（查 devicehub:list 权限用户，与 ticket 的 queryAgents 同款思路）。
func (s *Service) notifyAlarm(ctx context.Context, d *Device, level, msg string) {
	if s.notifier == nil || s.recipients == nil {
		return
	}
	title := "设备告警：" + d.Name
	if strings.HasPrefix(strings.ToLower(s.lang), "en") {
		title = "Device alarm: " + d.Name
	}
	holders, err := s.recipients(ctx)
	if err != nil {
		s.logWarn("devicehub: resolve alarm recipients failed", "err", err.Error())
		return
	}
	byTenant := map[idgen.ID][]idgen.ID{}
	for _, h := range holders {
		byTenant[h.tenantID] = append(byTenant[h.tenantID], h.userID)
	}
	for tid, ids := range byTenant {
		m := notify.Msg{
			To: ids, Category: "business", Title: title, Content: msg,
			Link: "/devicehub/monitor?device=" + d.DeviceNo, Channels: []string{notify.ChannelInApp},
		}
		if err := s.notifier.Send(tenant.With(ctx, tid), m); err != nil {
			s.logWarn("devicehub: alarm notify failed", "err", err.Error())
		}
	}
}

// alarmRecipient 是告警收件人（坐席）的最小形态。
type alarmRecipient struct {
	tenantID idgen.ID
	userID   idgen.ID
}

// AlarmRuleInput 是告警规则表单。
type AlarmRuleInput struct {
	PointID         idgen.ID `json:"pointId"`
	Name            string   `json:"name"`
	Op              string   `json:"op"`
	Threshold       float64  `json:"threshold"`
	Level           string   `json:"level"`
	Enabled         bool     `json:"enabled"`
	CooldownSeconds int      `json:"cooldownSeconds"`
}

// ListAlarmEvents 返回最近告警事件（监控页的告警流）。
func (s *Service) ListAlarmEvents(ctx context.Context, deviceID idgen.ID, limit int) ([]AlarmEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []AlarmEvent
	q := s.db.WithContext(ctx).Order("id DESC").Limit(limit)
	if deviceID > 0 {
		q = q.Where("device_id = ?", deviceID)
	}
	err := q.Find(&rows).Error
	return rows, err
}

// ListAlarmRules 返回测点的告警规则。
func (s *Service) ListAlarmRules(ctx context.Context, pointID idgen.ID) ([]AlarmRule, error) {
	var rows []AlarmRule
	err := s.db.WithContext(ctx).Where("point_id = ?", pointID).Order("id").Find(&rows).Error
	return rows, err
}

// SaveAlarmRule 新建/更新阈值规则。
func (s *Service) SaveAlarmRule(ctx context.Context, id idgen.ID, in AlarmRuleInput) (*AlarmRule, error) {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return nil, response.BadRequest("devicehub.alarm_name_required")
	case !validOps[in.Op]:
		return nil, response.BadRequest("devicehub.alarm_op_invalid")
	case in.Level != LevelInfo && in.Level != LevelWarn && in.Level != LevelCritical:
		return nil, response.BadRequest("devicehub.alarm_level_invalid")
	case in.CooldownSeconds < 0:
		in.CooldownSeconds = 300
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&Point{}).Where("id = ?", in.PointID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, response.NotFound("devicehub.point_not_found")
	}
	var row AlarmRule
	if id != 0 {
		if err := s.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
			return nil, err
		}
	}
	row.PointID, row.Name, row.Op, row.Threshold = in.PointID, in.Name, in.Op, in.Threshold
	row.Level, row.Enabled, row.CooldownSeconds = in.Level, in.Enabled, in.CooldownSeconds
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteAlarmRule 删阈值规则（事件保留）。
func (s *Service) DeleteAlarmRule(ctx context.Context, id idgen.ID) error {
	res := s.db.WithContext(ctx).Delete(&AlarmRule{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("devicehub.alarm_not_found")
	}
	return nil
}
