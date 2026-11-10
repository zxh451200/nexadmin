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
	"sync"
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

	// 指令在途集合：commandLoop 每轮扫 queued 时认领单号，已在执行的跳过。
	// 按设备分组并发后，下一轮扫描不能再把同一台设备上还在跑的指令领一遍。
	cmdMu       sync.Mutex
	cmdInFlight map[idgen.ID]bool
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
	s := &Service{db: db, log: log, cmdInFlight: map[idgen.ID]bool{}}
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

// text 按应用默认语言取文案：采集/指令协程没有请求上下文，落库与回执文案跟随默认语言。
func (s *Service) text(zh, en string) string {
	if strings.HasPrefix(strings.ToLower(s.lang), "en") {
		return en
	}
	return zh
}

// ---- 设备 CRUD ----

// configSecretMask 是协议配置里密码回显的占位串。
const configSecretMask = "***"

// maskDeviceConfig 把协议配置 JSON 里的密码字段替换成占位串（键名不区分大小写）。
// 只改顶层的 password：协议配置是各驱动自己解析的扁平对象，嵌套里没有密码概念。
// 解析失败原样返回——配置坏时把原文交还前端，至少能让人看见并改对，不影响其他字段。
func maskDeviceConfig(cfg string) string {
	if strings.TrimSpace(cfg) == "" {
		return cfg
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(cfg), &obj); err != nil || obj == nil {
		return cfg
	}
	masked := false
	for k, v := range obj {
		if !strings.EqualFold(k, "password") || v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue // 空密码不算秘密，保持原值
		}
		obj[k] = configSecretMask
		masked = true
	}
	if !masked {
		return cfg // 没有密码字段就不重排 JSON，省得前端/日志里配置形状无谓变化
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return cfg
	}
	return string(out)
}

// mergeMaskedConfig 处理「回显的占位密码」：incoming 中 password == "***" 视作未修改，
// 用 stored 里的同名（不区分大小写）字段还原；其他值一律按用户输入处理——空串 = 清空密码。
// incoming 解析失败原样返回，交给 SaveDevice 的 json.Valid 校验拦成 400。
//
// 关键不变式：库里永远只能存真实密码。所以 stored 解析失败 / 找不到同名密码 / 它本身就是占位串时，
// 直接删掉该键（等效「保持无密码」），绝不把 "***" 写回库。
func mergeMaskedConfig(incoming, stored string) string {
	if !strings.Contains(incoming, configSecretMask) {
		return incoming // 没有占位串就不用解析，保持用户提交的原文
	}
	var in map[string]any
	if err := json.Unmarshal([]byte(incoming), &in); err != nil || in == nil {
		return incoming
	}
	var old map[string]any
	if err := json.Unmarshal([]byte(stored), &old); err != nil {
		old = nil // 旧配置不可解析 = 没有可还原的真值，走下面的删除分支
	}
	var real any
	hasReal := false
	for k, v := range old {
		if strings.EqualFold(k, "password") {
			if v != nil {
				if s, ok := v.(string); !ok || s != configSecretMask {
					real, hasReal = v, true
				}
			}
			break
		}
	}
	changed := false
	for k, v := range in {
		if !strings.EqualFold(k, "password") {
			continue
		}
		if s, ok := v.(string); !ok || s != configSecretMask {
			continue // 用户真的改了密码 / 清空了密码：按输入落库
		}
		if hasReal {
			in[k] = real
		} else {
			delete(in, k)
		}
		changed = true
	}
	if !changed {
		return incoming
	}
	out, err := json.Marshal(in)
	if err != nil {
		return incoming
	}
	return string(out)
}

// ListDevices 分页返回设备：kw 按名称/编号模糊匹配；在线在前、离线其次、停用沉底
// （监控页默认关心活着的设备），同档按名称排。密码字段只回显占位串。
func (s *Service) ListDevices(ctx context.Context, kw string, page, pageSize int) ([]Device, int64, error) {
	q := s.db.WithContext(ctx).Model(&Device{})
	if kw = strings.TrimSpace(kw); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("name LIKE ? OR device_no LIKE ?", like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Device
	err := q.
		Order("CASE status WHEN 'online' THEN 0 WHEN 'offline' THEN 1 ELSE 2 END, name").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	for i := range rows {
		rows[i].Config = maskDeviceConfig(rows[i].Config)
	}
	return rows, total, nil
}

// GetDevice 按设备号取单台。列表分页后详情面板不能再从当前页里捞，得有自己的入口。
func (s *Service) GetDevice(ctx context.Context, deviceNo string) (*Device, error) {
	var row Device
	if err := s.db.WithContext(ctx).Where("device_no = ?", deviceNo).Take(&row).Error; err != nil {
		return nil, err
	}
	row.Config = maskDeviceConfig(row.Config)
	return &row, nil
}

// DeviceStats 是监控页顶部的总览计数：不进详情就该知道机群整体健不健康。
type DeviceStats struct {
	Total    int64 `json:"total"`
	Online   int64 `json:"online"`
	Offline  int64 `json:"offline"`
	Disabled int64 `json:"disabled"`
}

func (s *Service) DeviceStats(ctx context.Context) (*DeviceStats, error) {
	var st DeviceStats
	err := s.db.WithContext(ctx).Model(&Device{}).Select(
		"COUNT(*) AS total",
		"COALESCE(SUM(CASE WHEN status = 'online' THEN 1 ELSE 0 END), 0) AS online",
		"COALESCE(SUM(CASE WHEN status = 'offline' THEN 1 ELSE 0 END), 0) AS offline",
		"COALESCE(SUM(CASE WHEN status = 'disabled' THEN 1 ELSE 0 END), 0) AS disabled",
	).Scan(&st).Error
	return &st, err
}

// DeviceInput 是设备表单。Config 是协议 JSON 文本（前端已按协议拼好）。
type DeviceInput struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`
	Config   string `json:"config"`
	Enabled  bool   `json:"enabled"`
	// Location 是安装位置（LocationField 选点或手填）；nil = 清空。坐标存 WGS-84 原值。
	Location *GeoPointInput `json:"location,omitempty"`
}

// GeoPointInput 是一个地理点位（经纬度 + 可读地址）。
type GeoPointInput struct {
	Lng     float64 `json:"lng"`
	Lat     float64 `json:"lat"`
	Address string  `json:"address"`
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
	// 更新分支先取旧行：回传的配置可能带着占位密码（***），要在合法性校验之前还原成库里的真值。
	// 新建没有旧行可比，不 merge（提交什么就是什么）。
	var row Device
	if id != 0 {
		if err := s.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
			return nil, err
		}
		in.Config = mergeMaskedConfig(in.Config, row.Config)
	}
	// 配置必须是合法 JSON 对象：坏配置到采集循环才爆不如在保存时就拦下
	if in.Config != "" && !json.Valid([]byte(in.Config)) {
		return nil, response.BadRequest("devicehub.config_invalid")
	}
	// 位置坐标范围校验：越界一律 400，不静默钳位（错位的设备定位比报错更难排查）
	if in.Location != nil && (in.Location.Lng < -180 || in.Location.Lng > 180 || in.Location.Lat < -90 || in.Location.Lat > 90) {
		return nil, response.BadRequest("devicehub.location_invalid")
	}
	row.Name, row.Protocol, row.Endpoint, row.Config, row.Enabled = in.Name, in.Protocol, in.Endpoint, in.Config, in.Enabled
	if in.Location != nil {
		lng, lat := in.Location.Lng, in.Location.Lat
		row.Lng, row.Lat, row.Location = &lng, &lat, strings.TrimSpace(in.Location.Address)
	} else {
		row.Lng, row.Lat, row.Location = nil, nil, ""
	}
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
		s.collector.Reload(row.ID)
	}
	// 回给前端的是脱敏副本：collector.Reload 读的是数据库，不受影响；库里始终是真实密码。
	out := row
	out.Config = maskDeviceConfig(out.Config)
	return &out, nil
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
	s.collector.Reload(deviceID)
	return &row, nil
}

// DeletePoint 删测点（历史读数保留——趋势数据不因测点删除而丢）。
// 告警规则与小时聚合不保留：规则是配置不是历史，留着就是指向已删测点的死配置；聚合桶是只认
// point_id 的派生缓存，测点没了就没有入口读它，而 DeleteDevice 的级联是「顺着该设备现存的测点找」，
// 留着就成了设备删完也清不掉的孤儿行。原始 Reading 带 device_id，删设备时收得回，两处口径一致。
func (s *Service) DeletePoint(ctx context.Context, deviceID idgen.ID, id idgen.ID) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND device_id = ?", id, deviceID).Delete(&Point{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return response.NotFound("devicehub.point_not_found")
		}
		for _, row := range []any{&AlarmRule{}, &ReadingAgg{}} {
			if err := tx.Where("point_id = ?", id).Delete(row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.collector != nil {
		s.collector.Reload(deviceID)
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
	// 首次上线前 LastSeenAt 为空：用它区分「恢复」与「第一次连上」，避免新设备上线就报一条恢复。
	hadSeen := d.LastSeenAt != nil
	updates := map[string]any{"status": status}
	if status == StatusOnline {
		updates["last_seen_at"] = now
	}
	if err := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", int64(d.ID)).Updates(updates).Error; err != nil {
		s.logWarn("devicehub: update status failed", "err", err.Error())
		return
	}
	if status == StatusOnline {
		d.LastSeenAt = &now
	}
	if d.Status == status {
		return // online 心跳：状态没变，不重复发 WS/事件/告警
	}
	prev := d.Status
	d.Status = status
	ws.Publish("device:feed", wsFrame{Type: "status", DeviceNo: d.DeviceNo, DeviceID: d.ID, Status: status})
	s.publish(ctx, EventStatus, EventData{DeviceNo: d.DeviceNo, DeviceID: int64(d.ID), Message: status})

	// 状态真正变化才补设备级告警事件（RuleID=0 表示不是阈值规则触发）。
	// 离线给 critical、恢复给 info；事件写失败只记日志——状态已经提交，不回头撤销。
	recordStatusAlarm := func(level, msg string) {
		event := AlarmEvent{RuleID: 0, DeviceID: d.ID, PointID: 0, Value: 0, Level: level, Message: msg}
		if err := s.db.WithContext(ctx).Create(&event).Error; err != nil {
			s.logWarn("devicehub: store status alarm failed", "deviceNo", d.DeviceNo, "err", err.Error())
			return
		}
		s.notifyAlarm(ctx, d, level, msg)
	}
	switch {
	case status == StatusOffline:
		recordStatusAlarm(LevelCritical, s.text("设备离线："+d.Name, "Device offline: "+d.Name))
	case status == StatusOnline && prev == StatusOffline && hadSeen:
		// 只有「在线过 → 离线 → 再上线」算恢复；从没上线过的设备首次连上不报喜
		recordStatusAlarm(LevelInfo, s.text("设备恢复在线："+d.Name, "Device back online: "+d.Name))
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

// 趋势图点数上限与原始表扫描上限。
//
// Why 查询期聚合：默认窗口 24h、5s 采集约 1.7 万行，原先 Limit(5000) 会静默截断成最早
// 约 7 小时的数据——前端无从得知被截断，趋势图缺后半段比画得粗更糟。现在「取整个窗口 +
// 查询期按整齐间隔降采样」：返回行数有上限，但覆盖完整窗口，拿到什么画什么。
//
// Why 不再抬高 LIMIT：抬高只是把截断点往后推（覆盖 24h 要 2 万行、48h 要 4 万行），
// 传输与前端渲染都白花，窗口一放大照样截断。点数的上限交给自适应间隔，扫描上限单独放宽到
// historyScanLimit 兜住内存；真扫满说明单窗口数据量异常，按已扫到的行聚合仍是「覆盖窗口
// 前段 + 明确告诉调用方桶间隔」，不会退化成静默只画开头。
const (
	historyMaxPoints = 800
	historyScanLimit = 200000
)

// historyIntervals 是查询期聚合的候选间隔（严格升序）。
var historyIntervals = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// historyStep 取第一个让「窗口/间隔 ≤ historyMaxPoints」的候选间隔（升序找）。
func historyStep(span time.Duration) time.Duration {
	for _, step := range historyIntervals {
		if span/step <= historyMaxPoints {
			return step
		}
	}
	return historyIntervals[len(historyIntervals)-1]
}

// historyIntervalLabel 把聚合间隔渲染成桶标签：整小时 %dh、整分钟 %dm、否则 %ds。
func historyIntervalLabel(step time.Duration) string {
	switch {
	case step%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(step/time.Hour))
	case step%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(step/time.Minute))
	default:
		return fmt.Sprintf("%ds", int64(step/time.Second))
	}
}

// downsampleHistory 把按 ts 升序的原始读数按 step 聚合 min/max/avg。
// 桶键用 TS.Truncate(step)（UTC），保证同一时间片落的桶稳定；输入有序，输出也就有序，
// 不需要再排序（空桶不补行，趋势图按实际点连线）。
func downsampleHistory(readings []Reading, step time.Duration) []HistoryRow {
	rows := make([]HistoryRow, 0, historyMaxPoints)
	var cur HistoryRow
	var key time.Time
	var sum float64
	var cnt int
	for i := range readings {
		r := &readings[i]
		if r.NumValue == nil {
			continue
		}
		k := r.TS.Truncate(step)
		if cnt == 0 || !k.Equal(key) {
			if cnt > 0 {
				cur.Avg = sum / float64(cnt)
				rows = append(rows, cur)
			}
			key, sum, cnt = k, 0, 0
			cur = HistoryRow{TS: k, Min: *r.NumValue, Max: *r.NumValue}
		}
		if v := *r.NumValue; v < cur.Min {
			cur.Min = v
		} else if v > cur.Max {
			cur.Max = v
		}
		sum += *r.NumValue
		cnt++
	}
	if cnt > 0 {
		cur.Avg = sum / float64(cnt)
		rows = append(rows, cur)
	}
	return rows
}

// History 查历史趋势：显式 hour 或窗口 >48h 走小时桶；其余走原始表，并在查询期降到
// ≤ historyMaxPoints 个点，返回的 bucket 是实际聚合间隔标签（"raw" 表示未降采样）。
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
	span := q.To.Sub(q.From)
	bucket := q.Bucket
	if bucket == "" {
		if span <= 48*time.Hour {
			bucket = "raw"
		} else {
			bucket = "hour"
		}
	}
	// 只有显式 hour 才走聚合桶；其余（含前端把我们回传的间隔标签原样带回来）都走原始表——
	// 原始表里能按请求窗口重新聚合，比误读小时桶更贴近调用方意图。
	// 查询必须落在 ReadingAgg 上：HistoryRow 是纯 DTO 没有 TableName，直接 Find 会去查
	// 不存在的 history_rows 表，>48h 的历史与导出必 500。列名也不同（bucket/num_min/num_max/num_avg），
	// 查完按列映射成 DTO。
	if bucket == "hour" {
		var aggs []ReadingAgg
		if err := s.db.WithContext(ctx).
			Where("point_id = ? AND bucket >= ? AND bucket <= ?", q.PointID, q.From.Truncate(time.Hour), q.To).
			Order("bucket").Limit(5000).Find(&aggs).Error; err != nil {
			return nil, "", err
		}
		rows := make([]HistoryRow, 0, len(aggs))
		for i := range aggs {
			rows = append(rows, HistoryRow{TS: aggs[i].Bucket, Min: aggs[i].NumMin, Max: aggs[i].NumMax, Avg: aggs[i].NumAvg})
		}
		return rows, "hour", nil
	}
	var readings []Reading
	if err := s.db.WithContext(ctx).
		Where("point_id = ? AND ts >= ? AND ts <= ? AND num_value IS NOT NULL", q.PointID, q.From, q.To).
		Order("ts").Limit(historyScanLimit).Find(&readings).Error; err != nil {
		return nil, "", err
	}
	if len(readings) <= historyMaxPoints {
		rows := make([]HistoryRow, 0, len(readings))
		for i := range readings {
			if v := readings[i].NumValue; v != nil {
				rows = append(rows, HistoryRow{TS: readings[i].TS, Min: *v, Max: *v, Avg: *v})
			}
		}
		return rows, "raw", nil
	}
	step := historyStep(span)
	return downsampleHistory(readings, step), historyIntervalLabel(step), nil
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
			// 恢复事件：这条规则上次触发过、现在回到阈值内——补一条 info 级恢复并清 last_fired_at。
			// 不清标记，恢复永远无人知晓，且下一次越限会被旧冷却吞掉；清了之后恢复事件也天然只发一次。
			if r.LastFiredAt != nil && validOps[r.Op] {
				s.recoverAlarm(ctx, d, p, r, *tick.Num)
			}
			continue
		}
		if r.LastFiredAt != nil && now.Sub(*r.LastFiredAt) < time.Duration(r.CooldownSeconds)*time.Second {
			continue // 冷却期内不重复
		}
		if err := s.db.Model(&AlarmRule{}).Where("id = ?", r.ID).Update("last_fired_at", now).Error; err != nil {
			continue
		}
		// 文案跟随应用默认语言：前端两套模板只差括号全半角与空格，操作符继续走 opLabel。
		msg := s.text(
			fmt.Sprintf("%s / %s = %.3g %s（%s %.3g）", d.Name, p.Name, *tick.Num, p.Unit, opLabel(r.Op), r.Threshold),
			fmt.Sprintf("%s / %s = %.3g %s (%s %.3g)", d.Name, p.Name, *tick.Num, p.Unit, opLabel(r.Op), r.Threshold),
		)
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

// recoverAlarm 写告警恢复事件（info 级）并清规则的触发标记；随后照常走通知与事件总线，
// 订阅方按 Level 区分触发/恢复。
func (s *Service) recoverAlarm(ctx context.Context, d *Device, p *Point, r AlarmRule, v float64) {
	msg := s.text(
		fmt.Sprintf("%s / %s 已恢复（阈值 %s %.3g，当前 %.3g）", d.Name, p.Name, opLabel(r.Op), r.Threshold, v),
		fmt.Sprintf("%s / %s recovered (threshold %s %.3g, now %.3g)", d.Name, p.Name, opLabel(r.Op), r.Threshold, v),
	)
	event := AlarmEvent{RuleID: r.ID, DeviceID: d.ID, PointID: p.ID, Value: v, Level: LevelInfo, Message: msg}
	if err := s.db.Create(&event).Error; err != nil {
		s.logWarn("devicehub: store alarm recovery failed", "err", err.Error())
		return
	}
	if err := s.db.Model(&AlarmRule{}).Where("id = ?", r.ID).Update("last_fired_at", nil).Error; err != nil {
		s.logWarn("devicehub: clear alarm last_fired_at failed", "err", err.Error())
	}
	s.notifyAlarm(ctx, d, LevelInfo, msg)
	s.publish(ctx, EventAlarm, EventData{
		DeviceNo: d.DeviceNo, DeviceID: int64(d.ID), PointID: int64(p.ID),
		Address: p.Address, Value: v, Level: LevelInfo, Message: msg,
	})
	ws.Publish("device:"+d.DeviceNo, wsFrame{Type: "alarm", DeviceNo: d.DeviceNo, DeviceID: d.ID,
		Points: []pointTick{{PointID: p.ID, Address: p.Address, Name: p.Name, Unit: p.Unit, Num: &v, TS: time.Now().UTC()}}})
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
	title := s.text("设备告警："+d.Name, "Device alarm: "+d.Name)
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

// validLevels 是合法告警级别（筛选参数只认这几个，其余一律当「全部」）。
var validLevels = map[string]bool{LevelInfo: true, LevelWarn: true, LevelCritical: true}

// ListAlarmEvents 分页返回告警事件（监控页的告警流）：level 为空 = 全部级别。
func (s *Service) ListAlarmEvents(ctx context.Context, deviceID idgen.ID, level string, page, pageSize int) ([]AlarmEvent, int64, error) {
	q := s.db.WithContext(ctx).Model(&AlarmEvent{})
	if deviceID > 0 {
		q = q.Where("device_id = ?", deviceID)
	}
	if validLevels[level] {
		q = q.Where("level = ?", level)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []AlarmEvent
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

// ListAlarmRulesByDevice 一次返回设备的全部告警规则（按测点分组在消费端做）。
// 监控页告警 Tab 原来对每个测点单独发一次请求，测点一多打开就是一串请求。
func (s *Service) ListAlarmRulesByDevice(ctx context.Context, deviceID idgen.ID) ([]AlarmRule, error) {
	var rows []AlarmRule
	err := s.db.WithContext(ctx).
		Where("point_id IN (?)", s.db.Model(&Point{}).Select("id").Where("device_id = ?", deviceID)).
		Order("id").Find(&rows).Error
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
