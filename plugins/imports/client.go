package imports

// client.go 是宿主侧注册入口:业务模块在自己的 Register(d) 里调用 RegisterSource,
// 把「可导入的表 + 逐行落库闭包」交给导入中心。

import "nexadmin/pkg/idgen"

var _ = idgen.Next // 保持 idgen 依赖显性(当前插件内不直接发号,宿主 Handler 自行处理)
