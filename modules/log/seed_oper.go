package log

import (
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
)

type operSeed struct {
	module, typ, operator, ip, location, status string
	costMs                                      int
	method, url                                 string
	minutesAgo                                  int
	errorStack                                  string
}

// 与前端 oper-log.mock.ts 一致的演示操作日志（用户名和 IP 都是虚构的）。
var operSeeds = []operSeed{
	{"用户管理", "create", "admin", "10.20.1.15", "广东省深圳市", "success", 86, "POST", "/api/v1/users", 4, ""},
	{"角色管理", "grant", "admin", "10.20.1.15", "广东省深圳市", "success", 132, "PUT", "/api/v1/roles/1830000000000000101/menus", 18, ""},
	{"菜单管理", "sort", "zhang.wei", "10.20.3.42", "北京市", "success", 54, "PUT", "/api/v1/menus/sort", 35, ""},
	{"部门管理", "update", "li.na", "192.168.8.21", "上海市", "success", 71, "PUT", "/api/v1/depts/1830000000000000203", 62, ""},
	{"用户管理", "export", "admin", "10.20.1.15", "广东省深圳市", "success", 640, "GET", "/api/v1/users/export", 95, ""},
	{"字典管理", "update", "wang.fang", "172.16.5.9", "浙江省杭州市", "failed", 312, "PUT", "/api/v1/dict/data/1830000000000000304", 140,
		"duplicate dict value \"1\"\n  at dict.Service.Update(service.go:128)\n  at dict.Handler.UpdateData(handler.go:64)"},
	{"参数配置", "update", "admin", "10.20.1.15", "广东省深圳市", "success", 48, "PUT", "/api/v1/configs/1830000000000000402", 190, ""},
	{"通知公告", "create", "zhao.lei", "192.168.8.37", "江苏省南京市", "success", 163, "POST", "/api/v1/notices", 260, ""},
	{"文件管理", "delete", "li.na", "192.168.8.21", "上海市", "success", 97, "DELETE", "/api/v1/files/1830000000000000511", 330, ""},
	{"租户管理", "create", "admin", "10.20.1.15", "广东省深圳市", "failed", 405, "POST", "/api/v1/tenants", 420,
		"field \"domain\" must be unique\n  at tenant.Service.Create(service.go:77)\n  at tenant.Handler.Create(handler.go:41)"},
	{"岗位管理", "sort", "zhang.wei", "10.20.3.42", "北京市", "success", 39, "PUT", "/api/v1/posts/sort", 515, ""},
	{"用户管理", "update", "sun.yue", "10.20.7.8", "四川省成都市", "success", 91, "PUT", "/api/v1/users/1830000000000000601", 640, ""},
	{"用户管理", "other", "admin", "10.20.1.15", "广东省深圳市", "success", 118, "POST", "/api/v1/users/1830000000000000601/reset-password", 780, ""},
	{"角色管理", "delete", "wang.fang", "172.16.5.9", "浙江省杭州市", "failed", 226, "DELETE", "/api/v1/roles/1830000000000000102", 905,
		"role is still assigned to 3 users\n  at role.Service.Delete(service.go:203)\n  at role.Handler.Delete(handler.go:88)"},
	{"菜单管理", "create", "admin", "10.20.1.15", "广东省深圳市", "success", 77, "POST", "/api/v1/menus", 1100, ""},
	{"部门管理", "create", "li.na", "192.168.8.21", "上海市", "success", 66, "POST", "/api/v1/depts", 1320, ""},
	{"字典管理", "import", "zhao.lei", "192.168.8.37", "江苏省南京市", "success", 1480, "POST", "/api/v1/dict/data/import", 1600, ""},
	{"通知公告", "update", "zhao.lei", "192.168.8.37", "江苏省南京市", "success", 109, "PUT", "/api/v1/notices/1830000000000000702", 1900, ""},
	{"参数配置", "update", "admin", "10.20.1.15", "广东省深圳市", "success", 52, "PUT", "/api/v1/configs/1830000000000000403", 2300, ""},
	{"租户管理", "update", "admin", "10.20.1.15", "广东省深圳市", "success", 121, "PUT", "/api/v1/tenants/1830000000000000801", 2900, ""},
	{"用户管理", "delete", "sun.yue", "10.20.7.8", "四川省成都市", "failed", 18, "DELETE", "/api/v1/users?ids=1830000000000000601", 3600,
		"cannot delete the built-in administrator\n  at user.Service.Delete(service.go:156)\n  at user.Handler.DeleteBatch(handler.go:112)"},
	{"文件管理", "create", "li.na", "192.168.8.21", "上海市", "success", 2210, "POST", "/api/v1/files/upload", 4500, ""},
	{"岗位管理", "create", "wang.fang", "172.16.5.9", "浙江省杭州市", "success", 58, "POST", "/api/v1/posts", 6200, ""},
	{"角色管理", "create", "admin", "10.20.1.15", "广东省深圳市", "success", 84, "POST", "/api/v1/roles", 8800, ""},
	{"用户管理", "export", "zhang.wei", "10.20.3.42", "北京市", "success", 702, "GET", "/api/v1/users/export", 12500, ""},
	{"部门管理", "delete", "admin", "10.20.1.15", "广东省深圳市", "success", 63, "DELETE", "/api/v1/depts/1830000000000000204", 20000, ""},
}

func (r operSeed) params() string {
	var v any
	switch {
	case r.method == "GET":
		v = map[string]any{"keyword": "", "page": 1, "pageSize": 10}
	case r.typ == "delete":
		v = map[string]any{"ids": []string{"1830000000000000500"}}
	default:
		v = map[string]any{"name": r.module, "status": "1", "remark": "demo"}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (r operSeed) response() string {
	if r.status == StatusSuccess {
		return `{"code":0,"message":"ok","data":null}`
	}
	b, _ := json.Marshal(map[string]any{"code": 500, "message": strings.SplitN(r.errorStack, "\n", 2)[0]})
	return string(b)
}

// seedOper 幂等：默认租户下已有操作日志就跳过。
func seedOper(db *gorm.DB) error {
	db = db.WithContext(tenant.Skip(db.Statement.Context))
	var c int64
	if err := db.Model(&OperLog{}).Where("tenant_id = ?", int64(tenant.DefaultID)).Count(&c).Error; err != nil || c > 0 {
		return err
	}
	now := time.Now().UTC()
	for _, r := range operSeeds {
		t := now.Add(-time.Duration(r.minutesAgo) * time.Minute)
		l := OperLog{Module: r.module, Type: r.typ, Operator: r.operator, IP: r.ip, Location: r.location, Status: r.status,
			CostMs: r.costMs, Method: r.method, URL: r.url, Params: r.params(), Response: r.response(), ErrorStack: r.errorStack}
		l.TenantID, l.CreatedAt, l.UpdatedAt = tenant.DefaultID, t, t
		if err := db.Create(&l).Error; err != nil {
			return err
		}
	}
	return nil
}
