package report

import (
	"regexp"
	"testing"
)

// 白名单护栏（2026-10-29 放开四张 sys_/plg_ 表时定下的规矩，后续新增表必须同时满足）：
//  1. 必须租户级（tenant_id），不允许 global —— 否则报表会跨租户；
//  2. 表名/列名不得命中 deny 列表；
//  3. 列名不得是个人信息或自由文本（账号、手机、邮箱、请求体、堆栈、UA…）。
//
// 管理员在列表页本来就能看到这些字段，报表只是把它变成可聚合视图；凡是「看过会给不出」的东西都不列。
func TestBuiltinSourcesAreTenantScopedAndPiiFree(t *testing.T) {
	pii := regexp.MustCompile(`(?i)(phone|mobile|email|mailbox|id_card|idno|openid|open_id|payer|recipient|receiver|username|passwd|password|token|secret|cookie|session|fingerprint|metadata|params|payload|response|stack|message|realname|nickname|avatar|address|user_agent|^ua$|^ip$|_ip$)`)

	raws := defaultRaw()
	if len(raws) < 7 {
		t.Fatalf("内置白名单只有 %d 张表，放开后应至少 7 张", len(raws))
	}
	for _, rs := range raws {
		if rs.Global {
			t.Errorf("%s：内置表不允许 global（会跨租户）", rs.Table)
		}
		if rs.TenantColumn != "tenant_id" {
			t.Errorf("%s：必须按 tenant_id 隔离，实际 %q", rs.Table, rs.TenantColumn)
		}
		if deniedTable(rs.Table) {
			t.Errorf("%s：表名被 deny 列表命中", rs.Table)
		}
		for _, c := range rs.Columns {
			if deniedColumn(c.Name) {
				t.Errorf("%s.%s：列名被 deny 列表命中", rs.Table, c.Name)
			}
			if pii.MatchString(c.Name) {
				t.Errorf("%s.%s：列名是个人信息或自由文本，不能进报表白名单", rs.Table, c.Name)
			}
		}
	}

	// 全量能编译通过（列名合法、类型合法、租户列没被当成可选列）
	reg := newRegistry(raws, func(table string, err error) {
		t.Errorf("%s 无法登记进白名单：%v", table, err)
	})
	if got := len(reg.List()); got != len(raws) {
		t.Errorf("白名单登记 %d 张，期望 %d 张", got, len(raws))
	}
}
