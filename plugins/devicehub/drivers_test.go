package devicehub

import (
	"errors"
	"testing"

	"nexadmin/plugins/devicehub/driver"
)

// 服务层的 validProtocols（配置页可选的协议）与驱动注册表必须是同一份事实：
// 只在前者里加名字、忘了在 drivers.go 接线，go build / go test 都不会响，
// 现场看到的是「协议选了、设备存了、就是采不到数」。这条用例把两者对齐钉住。
func TestEveryAcceptedProtocolIsRegistered(t *testing.T) {
	var unknown driver.ErrUnknownProtocol
	for protocol := range validProtocols {
		_, err := driver.New(protocol, "127.0.0.1:502", nil)
		if errors.As(err, &unknown) {
			t.Errorf("协议 %q 在 validProtocols 里可选，但没有驱动注册——drivers.go 少了一行空白导入", protocol)
		}
	}

	// 反向：没接线的协议必须仍然判成「未知协议」，否则上面那条等于没判。
	if _, err := driver.New("no-such-protocol", "127.0.0.1:502", nil); !errors.As(err, &unknown) {
		t.Errorf("未注册协议应当返回 ErrUnknownProtocol，实际 %v", err)
	}
}
