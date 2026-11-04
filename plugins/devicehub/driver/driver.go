// Package driver 是设备协议驱动的最小接口面：上层（采集调度、指令下发）只认
// Connect / Read / Write / Close，不知道底下是 Modbus 寄存器还是 MQTT topic。
// 新协议（S7 / OPC UA）= 新增一个子包实现本接口 + service 里的注册项，不加架构。
package driver

import "context"

// Value 是一次读取的结果：数值型走 Num，字符串型走 Str（恰好一个有效）。
type Value struct {
	Num float64
	Str string
}

// Driver 是一台已配置设备的连接。
type Driver interface {
	// Connect 建立连接（幂等：已连接直接返回 nil）。
	Connect(ctx context.Context) error
	// Close 断开（幂等）。
	Close() error
	// Ping 探活：连接断了返回 error（采集循环据此标记 offline 并重连）。
	Ping() error
	// Read 读一个测点。address/dataType 的语义由具体驱动定义（见各实现头部注释）。
	Read(ctx context.Context, address, dataType string) (Value, error)
	// Write 写一个测点（指令下发）。raw 是界面上传来的原始字符串值。
	Write(ctx context.Context, address, dataType, raw string) error
}

// PushDriver 是「订阅推送」型驱动的可选扩展（MQTT）：设备/网关主动上报，
// 采集调度不轮询，收到一帧调一次 fn（address 为测点注册时的相对地址）。
type PushDriver interface {
	Subscribe(ctx context.Context, fn func(address string, v Value)) error
}

// Registry 是驱动工厂：按协议名构造驱动实例。service 启动时注册，配置变更时重建实例。
type Factory func(endpoint string, configJSON []byte) (Driver, error)

var factories = map[string]Factory{}

// Register 注册协议工厂（init 时调用；重复注册以后者为准，测试可覆盖）。
func Register(protocol string, f Factory) {
	factories[protocol] = f
}

// New 按协议名构造驱动；未注册的协议返回错误（配置里选了没有编译进去的协议）。
func New(protocol, endpoint string, configJSON []byte) (Driver, error) {
	f, ok := factories[protocol]
	if !ok {
		return nil, ErrUnknownProtocol{Protocol: protocol}
	}
	return f(endpoint, configJSON)
}

// ErrUnknownProtocol 表示协议名没有对应的驱动工厂。
type ErrUnknownProtocol struct{ Protocol string }

func (e ErrUnknownProtocol) Error() string { return "devicehub: unknown protocol " + e.Protocol }
