// Package plugins 汇总已安装的源码插件。
// 标记行之间的空导入由 `go run ./cmd/nexadmin plugin install|remove` 维护，请勿手改。
package plugins

import (
	// nexadmin:plugins:begin
	_ "nexadmin/plugins/assistant"
	_ "nexadmin/plugins/audit"
	_ "nexadmin/plugins/codegen"
	_ "nexadmin/plugins/cron"
	_ "nexadmin/plugins/formbuilder"
	_ "nexadmin/plugins/i18n-editor"
	_ "nexadmin/plugins/identity"
	_ "nexadmin/plugins/im"
	_ "nexadmin/plugins/imports"
	_ "nexadmin/plugins/payment"
	_ "nexadmin/plugins/report"
	_ "nexadmin/plugins/scim"
	_ "nexadmin/plugins/security"
	_ "nexadmin/plugins/ticket"
	_ "nexadmin/plugins/website"
	_ "nexadmin/plugins/workflow"
	// nexadmin:plugins:end
)
