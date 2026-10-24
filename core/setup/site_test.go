package setup

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// 站点名写入设置页「站点」分区（上限 60 字，与设置页一致），不能为空。
func TestValidateSiteNameBounds(t *testing.T) {
	r := testRunner(t)
	for name, tc := range map[string]struct {
		site string
		ok   bool
	}{
		"normal":    {"我的后台", true},
		"60 runes":  {strings.Repeat("站", 60), true},
		"61 runes":  {strings.Repeat("站", 61), false},
		"empty":     {"", false},
		"blank":     {"   ", false},
		"trim edge": {"  " + strings.Repeat("a", 60) + "  ", true},
	} {
		req := baseReq()
		req.Admin.SiteName = tc.site
		if _, err := r.validate(req); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", name, err, tc.ok)
		}
	}
}

type fakeSiteWriter struct{ got []string }

func (f *fakeSiteWriter) WriteSiteName(_ context.Context, _ *gorm.DB, name string) error {
	f.got = append(f.got, name)
	return nil
}

// Hooks.SiteWriter 是可选钩子：core 把实现了 SiteWriter 的模块（系统参数模块）装配进来。
var _ SiteWriter = (*fakeSiteWriter)(nil)
