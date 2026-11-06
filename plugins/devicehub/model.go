package devicehub

// 设备接入与监控的数据模型。协议差异收敛在 Device.Config（JSON）与 Point.Address 里：
// 驱动层只认 (address, dataType)，上层不知道 Modbus 寄存器和 MQTT topic 的差别。

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 设备在线状态。
const (
	StatusOnline   = "online"
	StatusOffline  = "offline"
	StatusDisabled = "disabled"
)

// 协议。
const (
	ProtocolModbusTCP = "modbus-tcp"
	ProtocolMQTT      = "mqtt"
)

// 读写方向。
const (
	RWRead = "r"
	RWBoth = "rw"
)

// 数据类型（驱动层解析原始值用）。
const (
	TypeBool   = "bool"
	TypeInt16  = "int16"
	TypeUint16 = "uint16"
	TypeInt32  = "int32"
	TypeFloat  = "float32"
)

// 指令状态。
const (
	CmdQueued  = "queued"
	CmdDone    = "done"
	CmdFailed  = "failed"
	CmdTimeout = "timeout"
)

// 告警级别与比较符。
const (
	LevelInfo     = "info"
	LevelWarn     = "warn"
	LevelCritical = "critical"
)

// Device 是一台可采集的工业设备（变频器 / PLC / 网关上位机）。
type Device struct {
	database.PlatformModel
	DeviceNo string `gorm:"size:40;not null;uniqueIndex:uk_plg_devhub_device_no" json:"deviceNo"`
	Name     string `gorm:"size:100;not null" json:"name"`
	Protocol string `gorm:"size:16;not null" json:"protocol"`
	Endpoint string `gorm:"size:255;not null;default:''" json:"endpoint"`
	// Config 是协议特定参数（JSON）：modbus-tcp → {unitId, pollIntervalMs}；
	// mqtt → {username, password, upstreamPrefix}。密码字段序列化时由 handler 剥除。
	Config     string     `gorm:"type:text;not null" json:"config"`
	Status     string     `gorm:"size:16;not null;default:offline" json:"status"`
	Enabled    bool       `gorm:"not null;default:1" json:"enabled"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
	// 安装位置：LocationField 选点或手填。坐标统一存 WGS-84 原值（前端按所选地图厂商转换显示）。
	Lng      *float64   `json:"lng,omitempty"`
	Lat      *float64   `json:"lat,omitempty"`
	Location string     `gorm:"size:255;not null;default:''" json:"location"`
}

func (Device) TableName() string { return "plg_devhub_device" }

// Point 是设备上的一个测点（寄存器 / 线圈 / topic）。
type Point struct {
	database.PlatformModel
	DeviceID idgen.ID `gorm:"not null" json:"deviceId"`
	Name     string   `gorm:"size:100;not null" json:"name"`
	Address  string   `gorm:"size:128;not null;uniqueIndex:uk_plg_devhub_point_addr,priority:2" json:"address"`
	Unit     string   `gorm:"size:16;not null;default:''" json:"unit"`
	RW       string   `gorm:"column:rw;size:8;not null;default:r" json:"rw"`
	DataType string   `gorm:"size:16;not null;default:float32" json:"dataType"`
	Scale    float64  `gorm:"not null;default:1" json:"scale"`
	PollMs   int      `gorm:"column:poll_interval_ms;not null;default:0" json:"pollIntervalMs"`
	Deadband float64  `gorm:"not null;default:0" json:"deadband"`
	Enabled  bool     `gorm:"not null;default:1" json:"enabled"`
	Sort     int      `gorm:"not null;default:0" json:"sort"`
	// LastValueNum/LastValueStr 是最近一次采集值（内存缓存不丢时用它渲染监控页），
	// last_ts 为空表示从未采到。
	LastValueNum *float64   `gorm:"column:last_value_num" json:"lastValueNum,omitempty"`
	LastValueStr string     `gorm:"column:last_value_str;not null;default:''" json:"lastValueStr"`
	LastTS       *time.Time `gorm:"column:last_ts" json:"lastTs,omitempty"`
}

func (Point) TableName() string { return "plg_devhub_point" }

// Reading 是一条原始时序读数（sweeper 会把过期数据聚合进 ReadingAgg 后删除）。
type Reading struct {
	database.PlatformModel
	PointID  idgen.ID  `gorm:"not null" json:"-"`
	DeviceID idgen.ID  `gorm:"not null" json:"-"`
	TS       time.Time `gorm:"not null" json:"ts"`
	NumValue *float64  `gorm:"column:num_value" json:"numValue,omitempty"`
	StrValue string    `gorm:"column:str_value;not null;default:''" json:"strValue"`
}

func (Reading) TableName() string { return "plg_devhub_reading" }

// ReadingAgg 是小时级降采样桶：原始数据滚动删除后，历史趋势靠它。
type ReadingAgg struct {
	database.PlatformModel
	PointID idgen.ID  `gorm:"not null;uniqueIndex:uk_plg_devhub_agg,priority:1" json:"-"`
	Bucket  time.Time `gorm:"not null;uniqueIndex:uk_plg_devhub_agg,priority:2" json:"bucket"`
	NumMin  float64   `gorm:"not null" json:"min"`
	NumMax  float64   `gorm:"not null" json:"max"`
	NumAvg  float64   `gorm:"not null" json:"avg"`
	Cnt     int       `gorm:"not null" json:"cnt"`
}

func (ReadingAgg) TableName() string { return "plg_devhub_reading_agg" }

// Command 是一条指令下发单（写寄存器/线圈或 MQTT 下行）：queued → done/failed/timeout。
type Command struct {
	database.PlatformModel
	DeviceID     idgen.ID   `gorm:"not null" json:"deviceId"`
	PointID      idgen.ID   `gorm:"not null" json:"pointId"`
	RawValue     string     `gorm:"size:64;not null" json:"rawValue"`
	Status       string     `gorm:"size:16;not null;default:queued" json:"status"`
	Result       string     `gorm:"size:255;not null;default:''" json:"result"`
	OperatorID   idgen.ID   `gorm:"not null;default:0" json:"operatorId"`
	OperatorName string     `gorm:"size:64;not null;default:''" json:"operatorName"`
	DoneAt       *time.Time `json:"doneAt,omitempty"`
}

func (Command) TableName() string { return "plg_devhub_command" }

// AlarmRule 是测点阈值规则：op 阈值 持续 → 事件。
type AlarmRule struct {
	database.PlatformModel
	PointID   idgen.ID `gorm:"not null" json:"pointId"`
	Name      string   `gorm:"size:100;not null" json:"name"`
	Op        string   `gorm:"size:8;not null" json:"op"`
	Threshold float64  `gorm:"not null" json:"threshold"`
	Level     string   `gorm:"size:8;not null;default:warn" json:"level"`
	Enabled   bool     `gorm:"not null;default:1" json:"enabled"`
	// CooldownSeconds 触发后冷却期内不重复发事件/通知，避免秒级采集刷屏。
	CooldownSeconds int        `gorm:"not null;default:300" json:"cooldownSeconds"`
	LastFiredAt     *time.Time `json:"lastFiredAt,omitempty"`
}

func (AlarmRule) TableName() string { return "plg_devhub_alarm_rule" }

// AlarmEvent 是一次告警触发记录。
type AlarmEvent struct {
	database.PlatformModel
	RuleID   idgen.ID `gorm:"not null" json:"ruleId"`
	DeviceID idgen.ID `gorm:"not null" json:"deviceId"`
	PointID  idgen.ID `gorm:"not null" json:"pointId"`
	Value    float64  `gorm:"not null" json:"value"`
	Level    string   `gorm:"size:8;not null" json:"level"`
	Message  string   `gorm:"size:255;not null;default:''" json:"message"`
}

func (AlarmEvent) TableName() string { return "plg_devhub_alarm_event" }
