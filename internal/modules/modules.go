// Package modules 汇总业务模块。每个业务模块在自己包的 init() 里调用 core.RegisterBuiltin，
// 标记行之间的空导入由 `nexadmin add module` 维护，请勿手改；cmd/server/main.go 已空导入本包。
package modules

import (
	// nexadmin:modules:begin
	_ "nexadmin/internal/modules/example"
	_ "nexadmin/internal/modules/importer"
	// nexadmin:modules:end
)
