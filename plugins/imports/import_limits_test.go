package imports

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/config"
	"nexadmin/core/i18n"
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

// 报错必须报「本次生效」的那个上限：上限能收紧到 5MB，文案就不能停在出厂的 20MB。
// 断的是整句：词条改回没有 %s 的死句子，Sprintf 会挂上 %!(EXTRA ...)，整句相等就对不上；
// 参数从 importMaxBytes(ctx) 换成 MaxBytes，或者两边数不一致，同样红。
func TestImportTooLargeTextNamesTheEffectiveCeiling(t *testing.T) {
	b, err := i18n.New([]config.Language{{Code: "zh-CN", Name: "简体中文"}, {Code: "en-US", Name: "English"}}, "zh-CN", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(nil) })

	ctx := context.Background()
	core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
		return core.UploadLimits{MaxFileMB: 5}, nil
	})
	t.Cleanup(func() { core.SetUploadLimitsSource(nil) })

	eff := core.MBText(importMaxBytes(ctx))
	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "导入文件不能超过 " + eff},
		{"en-US", "Import files must not exceed " + eff},
	} {
		if got := i18n.T(i18n.WithLang(ctx, tc.lang), "imports.file_too_large", eff); got != tc.want {
			t.Errorf("%s 报错 = %q，想要 %q", tc.lang, got, tc.want)
		}
	}

	// 服务层那条兜底（整读进内存的绝对天花板）报的也是它自己拦下的那个数，不是生效值。
	hard := core.MBText(MaxBytes)
	if eff == hard {
		t.Fatalf("生效上限与出厂上限都是 %q，这条分不开两个口径", hard)
	}
	if got := i18n.T(i18n.WithLang(ctx, "zh-CN"), "imports.file_too_large", hard); got != "导入文件不能超过 "+hard {
		t.Errorf("出厂上限那条报错 = %q，想要 %q", got, "导入文件不能超过 "+hard)
	}
}

// 打真 handler：拒绝时上屏的那句必须写「本次生效」的上限（5MB），不是出厂的 20MB。
// 把参数换成 MaxBytes、或整条退回没有参数的死句子，这里都会红。
func TestRunRejectsOversizedFileWithTheEffectiveCeiling(t *testing.T) {
	b, err := i18n.New([]config.Language{{Code: "zh-CN", Name: "简体中文"}}, "zh-CN", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(nil) })

	core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
		return core.UploadLimits{MaxFileMB: 5}, nil
	})
	t.Cleanup(func() { core.SetUploadLimitsSource(nil) })

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", "big.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(make([]byte, 5*1024*1024+1)); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("source", "unused"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/plugins/imports", body)
	c.Request.Header.Set("Content-Type", mw.FormDataContentType())
	// 大小闸在取 svc / 查来源之前就落下，所以空 handler 足够。
	(&handler{}).run(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s，想要 400", w.Code, w.Body.String())
	}
	if got, want := w.Body.String(), "导入文件不能超过 5MB"; !bytes.Contains(w.Body.Bytes(), []byte(want)) {
		t.Errorf("响应 = %q，想要带上生效上限的那句 %q", got, want)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("20MB")) {
		t.Errorf("响应里还写着出厂上限 20MB：%s", w.Body.String())
	}
}
