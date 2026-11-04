package tenant

import (
	"testing"

	"nexadmin/pkg/idgen"
)

// 平台级调用方判据的单源用例：auth.User.IsPlatform（随会话给前端）、core.IsPlatformCaller
// （中间件按请求挡）、以及通知侧「这条站内信该不该发给 ta」都必须与此一致。
// 把 tid == 0 这一档删掉，前端就会把平台自己的非超管坐席判成只读；
// 把 super 这一档删掉，超管跨租户操作时前端会跟着变灰。
func TestIsPlatformCallerID(t *testing.T) {
	cases := []struct {
		name  string
		tid   idgen.ID
		super bool
		want  bool
	}{
		{"超管在别的租户里操作", 7, true, true},
		{"默认租户的普通账号", DefaultID, false, true},
		{"默认租户的超管", DefaultID, true, true},
		{"普通租户的普通账号", 7, false, false},
		// 账号没有租户归属：与解析链的兜底同口径，算平台内（不是「查不到就当没启用」）。
		{"账号未标租户", 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPlatformCallerID(tc.tid, tc.super); got != tc.want {
				t.Errorf("IsPlatformCallerID(%d, super=%v) = %v, want %v", tc.tid, tc.super, got, tc.want)
			}
		})
	}
}
