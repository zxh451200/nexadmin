package devicehub

// drivers.go 是协议驱动的唯一起效接线：每个协议子包在自己的 init() 里往 driver 注册表
// 报一次名（driver.Register），而 init 只在包被导入时才跑。
//
// 漏掉任何一行都不是编译错误，也不会有任何测试变红——表现是设备配得好好的、界面也让你选
// 这个协议，但采集协程和指令下发拿到的永远是「未知协议」，现场只能看到「采不到数」。
import (
	_ "nexadmin/plugins/devicehub/driver/modbus"
	_ "nexadmin/plugins/devicehub/driver/mqtt"
	_ "nexadmin/plugins/devicehub/driver/opcua"
	_ "nexadmin/plugins/devicehub/driver/s7"
)
