package region

import (
	"context"
	"testing"

	"nexadmin/core"
)

// 区划数据源上限：系统参数「上传限制」的普通文件上限只能把它往小调，放大顶不开内置 16MB。
// 上传与在线拉取共用同一口径（各自传入自己的内置值），所以两处都走 regionMaxBytes。
func TestRegionMaxBytes(t *testing.T) {
	ctx := context.Background()
	mb := func(v float64) int64 { return int64(v * 1024 * 1024) }
	withFileMB := func(t *testing.T, v float64) {
		t.Helper()
		core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
			return core.UploadLimits{MaxFileMB: v}, nil
		})
		t.Cleanup(func() { core.SetUploadLimitsSource(nil) })
	}

	t.Run("没登记设置页来源时用内置值", func(t *testing.T) {
		core.SetUploadLimitsSource(nil)
		for _, builtin := range []int64{maxUploadSize, maxSourceSize} {
			if got := regionMaxBytes(ctx, builtin); got != builtin {
				t.Errorf("regionMaxBytes(%d) = %d，想要 %d", builtin, got, builtin)
			}
		}
	})

	t.Run("收紧生效，报错里的 MB 数跟着变", func(t *testing.T) {
		withFileMB(t, 4)
		got := regionMaxBytes(ctx, maxUploadSize)
		if got != mb(4) {
			t.Errorf("上限 = %d，想要 4MB", got)
		}
		// 成对边界：恰好 4MB 放行、4MB+1 拒（handler 用的就是这个比较）
		if !(mb(4) <= got) {
			t.Errorf("恰好 4MB 被拒")
		}
		if !(mb(4)+1 > got) {
			t.Errorf("4MB+1 被放行")
		}
		if text := core.MBText(got); text != "4MB" {
			t.Errorf("报错参数 = %q，想要 4MB（region.source_too_large 用它）", text)
		}
	})

	// 参数允许小数 MB：整除会把 0.5MB 显示成「0MB」，所以报错走 core.MBText。
	t.Run("小数上限的报错写法不退化成 0MB", func(t *testing.T) {
		withFileMB(t, 0.5)
		got := regionMaxBytes(ctx, maxUploadSize)
		if got != mb(0.5) {
			t.Fatalf("上限 = %d，想要 %d", got, mb(0.5))
		}
		if text := core.MBText(got); text != "0.5MB" {
			t.Errorf("报错参数 = %q，想要 0.5MB", text)
		}
	})

	t.Run("参数为 0 或放大都不越内置封顶", func(t *testing.T) {
		withFileMB(t, 0)
		if got := regionMaxBytes(ctx, maxSourceSize); got != maxSourceSize {
			t.Errorf("0（不限制）= %d，想要内置 %d", got, maxSourceSize)
		}
		withFileMB(t, 512)
		if got := regionMaxBytes(ctx, maxSourceSize); got != maxSourceSize {
			t.Errorf("512MB 被放大成 %d，快照通道应封顶在 %d", got, maxSourceSize)
		}
	})
}
