package config

import (
	"reflect"
	"testing"
	"time"
)

// 每个 ratelimit.* 配置项都必须能通过 Rule(name) 取到：新增字段却忘了在 Rule 的 switch 里加 case，
// 会让这条限流悄悄不生效（dashboard_layout 就出现过）。
func TestRateLimitRuleCoversEveryField(t *testing.T) {
	rl := RateLimit{Enabled: true}
	v := reflect.ValueOf(&rl).Elem()
	ty := v.Type()
	n := 0
	for i := 0; i < ty.NumField(); i++ {
		name := ty.Field(i).Tag.Get("mapstructure")
		if name == "" || name == "enabled" || ty.Field(i).Type != reflect.TypeOf(Rule{}) {
			continue
		}
		n++
		want := Rule{Limit: 100 + i, Window: time.Duration(i+1) * time.Minute}
		v.Field(i).Set(reflect.ValueOf(want))
		if got := rl.Rule(name); got != want {
			t.Errorf("Rule(%q) = %+v，期望 %+v：Rule() 的 switch 缺少这个 case", name, got, want)
		}
	}
	if n == 0 {
		t.Fatal("没有找到任何限流规则字段")
	}
}
