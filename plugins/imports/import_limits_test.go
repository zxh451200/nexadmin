package imports

import (
	"context"
	"testing"

	"nexadmin/core"
)

// 导入上限：系统参数「上传限制」的普通文件上限只能把它往小调；放大顶不开 MaxBytes（整读进内存）。
func TestImportMaxBytes(t *testing.T) {
	ctx := context.Background()
	mb := func(v float64) int64 { return int64(v * 1024 * 1024) }
	withFileMB := func(t *testing.T, v float64) {
		t.Helper()
		core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
			return core.UploadLimits{MaxFileMB: v}, nil
		})
		t.Cleanup(func() { core.SetUploadLimitsSource(nil) })
	}

	t.Run("没登记设置页来源时用 MaxBytes", func(t *testing.T) {
		core.SetUploadLimitsSource(nil)
		if got := importMaxBytes(ctx); got != MaxBytes {
			t.Errorf("importMaxBytes = %d，想要 %d", got, MaxBytes)
		}
	})

	t.Run("参数收紧生效", func(t *testing.T) {
		withFileMB(t, 5)
		if got := importMaxBytes(ctx); got != mb(5) {
			t.Errorf("importMaxBytes = %d，想要 5MB", got)
		}
		// 成对边界：5MB 的导入放行，多一字节拒（handler 用的就是这个比较）
		if !(mb(5) <= importMaxBytes(ctx)) {
			t.Errorf("恰好 5MB 被拒")
		}
		if !(mb(5)+1 > importMaxBytes(ctx)) {
			t.Errorf("5MB+1 被放行")
		}
	})

	t.Run("参数为 0 或超内存天花板都回到 MaxBytes", func(t *testing.T) {
		withFileMB(t, 0)
		if got := importMaxBytes(ctx); got != MaxBytes {
			t.Errorf("0（不限制）= %d，想要出厂默认 %d", got, MaxBytes)
		}
		withFileMB(t, 500)
		if got := importMaxBytes(ctx); got != MaxBytes {
			t.Errorf("500MB 被放大成 %d，导入通道顶格应是 %d", got, MaxBytes)
		}
	})
}
