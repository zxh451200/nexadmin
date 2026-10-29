// Package modules 汇总框架内置模块。每个内置模块在自己的 init() 里调用 core.RegisterBuiltin，
// 并在此文件加一行空导入，例如：
//
//	_ "nexadmin/modules/system/user"
//
// 这样 core 不需要反向依赖 modules（避免循环引用），main.go 也不用改。
package modules

import (
	_ "nexadmin/modules/about"
	_ "nexadmin/modules/ai"
	_ "nexadmin/modules/apimonitor"
	_ "nexadmin/modules/cachemonitor"
	_ "nexadmin/modules/clienterror"
	_ "nexadmin/modules/dashboard"
	_ "nexadmin/modules/exportcenter"
	_ "nexadmin/modules/file"
	_ "nexadmin/modules/log"
	_ "nexadmin/modules/message"
	_ "nexadmin/modules/notice"
	_ "nexadmin/modules/online"
	_ "nexadmin/modules/openapi"
	_ "nexadmin/modules/plugin"
	_ "nexadmin/modules/profile"
	_ "nexadmin/modules/recycle"
	_ "nexadmin/modules/region"
	_ "nexadmin/modules/sendlog"
	_ "nexadmin/modules/servermonitor"
	_ "nexadmin/modules/sso"
	_ "nexadmin/modules/system/backup"
	_ "nexadmin/modules/system/config"
	_ "nexadmin/modules/system/dept"
	_ "nexadmin/modules/system/dict"
	_ "nexadmin/modules/system/language"
	_ "nexadmin/modules/system/menu"
	_ "nexadmin/modules/system/post"
	_ "nexadmin/modules/system/role"
	_ "nexadmin/modules/system/tenant"
	_ "nexadmin/modules/system/user"
)
