package ticket

import "testing"

// 导出行数上限的生效值：出厂 5000，装配时由 config.yaml 的 ticket.export_limit 覆盖。
// 成对边界 + <=0 回落，确保「没装配置」与「装了但留空」得到同一个数。
func TestSetExportLimit(t *testing.T) {
	t.Cleanup(func() { setExportLimit(0) })

	if got := maxExportRows(); got != exportLimit {
		t.Fatalf("出厂生效值 = %d，想要 %d", got, exportLimit)
	}

	setExportLimit(10)
	if got := maxExportRows(); got != 10 {
		t.Errorf("设为 10 后生效值 = %d，想要 10", got)
	}
	// ExportTickets 的 limit<=0 分支就是取这个生效值，导出口径因此只有一处
	if got := exportLimit; got != 5000 {
		t.Errorf("出厂常量被改写：%d", got)
	}

	setExportLimit(0)
	if got := maxExportRows(); got != exportLimit {
		t.Errorf("0（未设置）= %d，想要回落 %d", got, exportLimit)
	}
	setExportLimit(-5)
	if got := maxExportRows(); got != exportLimit {
		t.Errorf("负数 = %d，想要回落 %d", got, exportLimit)
	}
	setExportLimit(5000)
	if got := maxExportRows(); got != 5000 {
		t.Errorf("显式写成出厂值 = %d", got)
	}
}
