package user

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"nexadmin/core"
	"nexadmin/core/response"
)

// 用户导入的文件上限接系统参数「上传限制」：只能往小调（两档各自封顶），
// 报错里带出的数字必须是生效值，不能停在内置默认。
func TestResolveImportLimits(t *testing.T) {
	mb := func(v float64) int64 { return int64(v * 1024 * 1024) }
	ctx := context.Background()
	withFileMB := func(t *testing.T, v float64) {
		t.Helper()
		core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
			return core.UploadLimits{MaxFileMB: v}, nil
		})
		t.Cleanup(func() { core.SetUploadLimitsSource(nil) })
	}

	t.Run("没登记设置页来源时用内置默认", func(t *testing.T) {
		core.SetUploadLimitsSource(nil)
		got := resolveImportLimits(ctx)
		if got.csv != importMaxBytes || got.xlsx != importMaxXLSXBytes {
			t.Errorf("上限 = %+v，想要 csv %d / xlsx %d", got, importMaxBytes, importMaxXLSXBytes)
		}
	})

	t.Run("参数收紧两档一起生效", func(t *testing.T) {
		withFileMB(t, 1)
		got := resolveImportLimits(ctx)
		if got.csv != mb(1) || got.xlsx != mb(1) {
			t.Errorf("上限 = %+v，想要两档都是 1MB", got)
		}
	})

	t.Run("参数为 0 或放大都回到各自封顶", func(t *testing.T) {
		withFileMB(t, 0)
		if got := resolveImportLimits(ctx); got.csv != importMaxBytes || got.xlsx != importMaxXLSXBytes {
			t.Errorf("0（不限制）= %+v，想要内置默认", got)
		}
		withFileMB(t, 500)
		got := resolveImportLimits(ctx)
		if got.csv != importMaxBytes || got.xlsx != importMaxXLSXBytes {
			t.Errorf("500MB 被放大成 %+v，导入两档应分别封顶在 %d / %d", got, importMaxBytes, importMaxXLSXBytes)
		}
	})

	// 成对边界：收紧到 1MB 后，恰好 1MB 的 CSV 放行，多一字节拒，且报错说 1MB 不说 2MB。
	t.Run("成对边界与报错数字随参数走", func(t *testing.T) {
		withFileMB(t, 1)
		lim := resolveImportLimits(ctx)
		keyOf := func(err error) (string, []any) {
			t.Helper()
			var e *response.Error
			if !errors.As(err, &e) {
				t.Fatalf("期望 *response.Error，得到 %v", err)
			}
			return e.Key, e.Args
		}

		justFits := strings.Repeat("a", int(lim.csv))
		if kind, err := sniffImport(lim, "in.csv", int64(len(justFits)), bytes.NewReader([]byte(justFits))); err != nil || kind != "csv" {
			t.Errorf("恰好 1MB 的 CSV：kind=%q err=%v，应当放行", kind, err)
		}

		oneOver := append([]byte(justFits), 'b')
		_, err := sniffImport(lim, "in.csv", int64(len(oneOver)), bytes.NewReader(oneOver))
		key, args := keyOf(err)
		if key != "user.import.file_too_large" || len(args) != 1 || args[0] != "1MB" {
			t.Errorf("超一字节：报错 = %s %v，想要 user.import.file_too_large 带 1MB", key, args)
		}
	})
}
