package auth

import (
	"encoding/json"
	"testing"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 载荷那张脸要有真用例：Platform 是前端判断「给不给写入口 / 整页是不是只读」的唯一依据。
// 少了这几档，判据被改坏时只有 core 的中间件测试会红，前端与通知侧的漂移没人报——
// 例如把 super 这一支删掉，超管在租户 7 里操作时后端仍放行、前端却把按钮全灰掉。
func TestNormalizeMarksPlatformCallers(t *testing.T) {
	cases := []struct {
		name   string
		super  bool
		tid    idgen.ID
		want   bool
		inJSON bool // 前端能不能真拿到这个字段（json:"-" 就代表拿不到）
	}{
		{"超管在别的租户里操作", true, 7, true, true},
		{"默认租户的普通账号", false, tenant.DefaultID, true, true},
		{"普通租户管理员", false, 7, false, true},
		{"账号未标租户（解析链兜底算平台内）", false, 0, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := normalize(&User{ID: 1, Username: "u", TenantID: tc.tid, Super: tc.super})
			if u.Platform != tc.want {
				t.Errorf("Platform = %v, want %v（super=%v, tenant=%d）", u.Platform, tc.want, tc.super, tc.tid)
			}
			// Super 不外泄，Platform 必须外泄：前端只有拿到它才做得出只读态。
			b, err := json.Marshal(u)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if _, ok := got["super"]; ok {
				t.Error("载荷里出现了 super 字段：超管标记不该随 /auth/me 下发")
			}
			if v, ok := got["platform"]; !ok {
				t.Error("载荷里没有 platform 字段：前端只能靠 perms.includes('*') 近似，会误伤平台自己的非超管坐席")
			} else if v != tc.want {
				t.Errorf("载荷里的 platform = %v, want %v", v, tc.want)
			}
		})
	}
}
