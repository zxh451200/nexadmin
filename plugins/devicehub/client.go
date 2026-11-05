package devicehub

// 宿主侧进程内 API。业务模块（如 MES、能源管理）import 本包做服务端集成：
// 读最近值做联动判断、按地址下发指令、订阅 devicehub.* 事件推业务流；HTTP 接口给前端用。

import (
	"context"
	"time"

	"nexadmin/core/response"
)

// defaultSvc 在插件 Register 时设置；宿主调用发生在请求期，此时插件已挂载。
var defaultSvc = make(chan *Service, 1)

// SetDefault 由插件装配层调用，注册进程级默认服务。
func SetDefault(s *Service) {
	select {
	case <-defaultSvc: // 已有则替换
	default:
	}
	defaultSvc <- s
}

func current() *Service {
	select {
	case s := <-defaultSvc:
		defaultSvc <- s
		return s
	default:
		return nil
	}
}

func responseUnavailable() error {
	return response.Unavailable("devicehub.plugin_inactive")
}

// GetDevice 按设备号取设备（含在线状态与最近上线时间）。包级函数不能叫 Device：会和 Device 类型同名冲突。
func GetDevice(ctx context.Context, deviceNo string) (*Device, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	var d Device
	if err := s.db.WithContext(ctx).Where("device_no = ?", deviceNo).Take(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// pointsOfByNo 按设备号取全部启用测点。
func (s *Service) pointsOfByNo(ctx context.Context, deviceNo string) []Point {
	var d Device
	if err := s.db.WithContext(ctx).Select("id").Where("device_no = ?", deviceNo).Take(&d).Error; err != nil {
		return nil
	}
	return s.pointsOf(d.ID)
}

// Points 取设备全部测点（含最近值缓存——实时看板/联动判断直接读它，不用逐点查询）。
func Points(ctx context.Context, deviceNo string) ([]Point, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.pointsOfByNo(ctx, deviceNo), nil
}

// Latest 取一个测点的最近值（缩放后的工程量）：联动判断用，如「温度超了才启动」。
// never=true 表示该测点从未采集成功。
func Latest(ctx context.Context, deviceNo, address string) (num float64, str string, never bool, err error) {
	s := current()
	if s == nil {
		return 0, "", true, responseUnavailable()
	}
	_, p, err := s.pointByAddress(ctx, deviceNo, address)
	if err != nil {
		return 0, "", true, err
	}
	if p.LastTS == nil {
		return 0, p.LastValueStr, true, nil
	}
	if p.LastValueNum != nil {
		return *p.LastValueNum, p.LastValueStr, false, nil
	}
	return 0, p.LastValueStr, false, nil
}

// SendCommand 由宿主服务端按「设备号+测点地址」下发指令（如 MES 下发电机启停）。
// rawValue 是驱动原始值（非工程量）；指令进台账异步执行，回执看 devicehub.command 事件或 Commands 查询。
func SendCommand(ctx context.Context, deviceNo, address, rawValue, operator string) (*Command, error) {
	s := current()
	if s == nil {
		return nil, responseUnavailable()
	}
	return s.HostCommandByAddress(ctx, deviceNo, address, rawValue, operator)
}

// History 查历史趋势（与 HTTP /history 同源同口径：≤48h 走原始表，更久走小时桶）。
func History(ctx context.Context, deviceNo, address string, from, to time.Time, bucket string) ([]HistoryRow, string, error) {
	s := current()
	if s == nil {
		return nil, "", responseUnavailable()
	}
	_, p, err := s.pointByAddress(ctx, deviceNo, address)
	if err != nil {
		return nil, "", err
	}
	return s.History(ctx, HistoryQuery{PointID: p.ID, From: from, To: to, Bucket: bucket})
}
