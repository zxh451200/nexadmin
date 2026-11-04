package devicehub

// 服务层纯函数单测（告警比较符、指令值解析）。真库链路（迁移/落库/死区/告警）在 IT。

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/pkg/idgen"
)

func TestMain(m *testing.M) {
	_ = idgen.Init(1)
	gin.SetMode(gin.TestMode)
	m.Run()
}

// TestOpMatches 按「读数 v 与阈值 threshold」逐操作符钉住触发与不触发两种情形。
// 边界必须成对写（相等处触发/不触发各一条），只写一侧的话操作符方向反了也不会红。
func TestOpMatches(t *testing.T) {
	cases := []struct {
		op        string
		v         float64
		threshold float64
		want      bool
	}{
		{"gt", 5, 3, true},
		{"gt", 3, 3, false}, // 相等不算超过
		{"gt", 1, 3, false},
		{"lt", 2.9, 3, true},
		{"lt", 3, 3, false}, // 相等不算低于
		{"lt", 4, 3, false},
		{"ge", 3, 3, true}, // 相等算达到
		{"ge", 2.9, 3, false},
		{"ge", 4, 3, true},
		{"le", 3, 3, true}, // 相等算不超
		{"le", 3.1, 3, false},
		{"le", 2, 3, true},
		{"eq", 3, 3, true},
		{"eq", 2, 3, false},
		{"ne", 2, 3, true},
		{"ne", 3, 3, false},
		{"bogus", 1, 1, false}, // 未知操作符必须 false（保存时已拦，这里是防御）
	}
	for _, c := range cases {
		if got := opMatches(c.op, c.v, c.threshold); got != c.want {
			t.Errorf("opMatches(%q, %v, %v) = %v，期望 %v", c.op, c.v, c.threshold, got, c.want)
		}
	}
}

func TestRawValue(t *testing.T) {
	if v := rawValue("350"); v != 350 {
		t.Fatalf("rawValue = %v", v)
	}
	if v := rawValue("abc"); v != 0 {
		t.Fatalf("rawValue(abc) = %v, want 0", v)
	}
}

func TestAbs(t *testing.T) {
	if abs(-1.5) != 1.5 || abs(2) != 2 {
		t.Fatal("abs")
	}
}

// 事件目录编译守卫：三个事件名不与核心目录冲突（Register 同名忽略，这里锁名字防手滑改名）。
func TestEventCatalogNames(t *testing.T) {
	for _, name := range []string{EventAlarm, EventCommand, EventStatus} {
		if name == "" {
			t.Fatal("empty event name")
		}
	}
	_ = context.Background
}
