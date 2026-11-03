package core

import "testing"

// MBText 是报错与提示共用的写法：整 MB 不留小数尾巴。
func TestMBText(t *testing.T) {
	for _, tc := range []struct {
		bytes int64
		want  string
	}{
		{2 << 20, "2MB"},
		{2.5 * (1 << 20), "2.5MB"},
		{1 << 20, "1MB"},
		{50 << 20, "50MB"},
	} {
		if got := MBText(tc.bytes); got != tc.want {
			t.Errorf("MBText(%d) = %q，想要 %q", tc.bytes, got, tc.want)
		}
	}
}

// 上传上限的「参数优先 + 内置默认 + 硬顶」换算口径：各插件都走这里，不再各写一遍。
func TestResolveUploadCeiling(t *testing.T) {
	const def = int64(8 << 20)
	const ceiling = int64(50 << 20)
	mb := func(v float64) int64 { return int64(v * 1024 * 1024) }
	for _, tc := range []struct {
		name string
		mb   float64
		want int64
	}{
		{"未设（0）保留默认", 0, def},
		{"不限制（负数）也保留默认", -1, def},
		{"按参数换算", 20, mb(20)},
		{"小数按参数换算", 2.5, mb(2.5)},
		{"超过硬顶被截住", 1000, ceiling},
		{"恰好等于硬顶", 50, ceiling},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveUploadCeiling(tc.mb, def, ceiling); got != tc.want {
				t.Errorf("ResolveUploadCeiling(%v, %d, %d) = %d，想要 %d", tc.mb, def, ceiling, got, tc.want)
			}
		})
	}
}
