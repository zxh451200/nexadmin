package website

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nexadmin/core"
	"nexadmin/core/config"
	"nexadmin/core/i18n"
	"nexadmin/core/response"
)

// 媒体（logo / 封面 / 二维码）的大小上限来自「系统参数 > config.yaml > 内置默认」，
// 再被 mediaHardMaxMB 截顶；类型白名单由代码持有，前端提示与文件选择器都按它渲染。
// 这几条用 NewService(nil)：校验不过就落不了库，守卫一旦被删掉会在 s.db 上 panic 变红。

func f64(v float64) *float64 { return &v }

func withSettingsImageMB(t *testing.T, mb float64) {
	t.Helper()
	core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
		return core.UploadLimits{MaxImageMB: mb}, nil
	})
	t.Cleanup(func() { core.SetUploadLimitsSource(nil) })
}

func TestMediaLimitsSizeChain(t *testing.T) {
	mbBytes := func(v float64) int { return int(v * 1024 * 1024) }
	cases := []struct {
		name      string
		settings  *float64 // nil = 设置页没登记来源
		cfgMB     int      // config.yaml 的 upload.max_size_image_mb
		wantMB    float64
		wantBytes int
		wantText  string
	}{
		{name: "都没设：内置默认 2MB", wantMB: 2, wantBytes: mbBytes(2), wantText: "2MB"},
		{name: "config.yaml 生效", cfgMB: 4, wantMB: 4, wantBytes: mbBytes(4), wantText: "4MB"},
		{name: "设置页盖过 config.yaml", settings: f64(1.5), cfgMB: 4, wantMB: 1.5, wantBytes: mbBytes(1.5), wantText: "1.5MB"},
		{name: "设置页为 0 回落 config.yaml", settings: f64(0), cfgMB: 3, wantMB: 3, wantBytes: mbBytes(3), wantText: "3MB"},
		{name: "超过硬顶截到 5MB", settings: f64(20), wantMB: 5, wantBytes: mbBytes(5), wantText: "5MB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.settings != nil {
				withSettingsImageMB(t, *tc.settings)
			}
			s := NewService(nil)
			s.SetConfigImageMaxMB(tc.cfgMB)
			got := s.MediaLimits(context.Background())
			if got.MaxMB != tc.wantMB || got.MaxBytes != tc.wantBytes || got.Text() != tc.wantText {
				t.Errorf("MediaLimits = %vMB / %d bytes / %q，想要 %vMB / %d / %q",
					got.MaxMB, got.MaxBytes, got.Text(), tc.wantMB, tc.wantBytes, tc.wantText)
			}
		})
	}
}

func TestMediaLimitsTypesAreBitmaps(t *testing.T) {
	got := NewService(nil).MediaLimits(context.Background())
	if want := "PNG / JPEG / WebP / GIF"; got.TypesText() != want {
		t.Errorf("类型清单 = %q，想要 %q（改了要同步前端与语言包的说法）", got.TypesText(), want)
	}
	if len(got.Mimes) != 4 || got.Mimes[0] != "image/png" {
		t.Errorf("accept 用的 MIME 清单 = %v", got.Mimes)
	}
	// svg 可携带脚本，位图之外的类型永远进不来。
	for _, mime := range []string{"image/svg+xml", "image/avif"} {
		if got.allows(mime) {
			t.Errorf("%s 不该被允许", mime)
		}
	}
}

func TestSaveMediaRejectsByEffectiveLimits(t *testing.T) {
	ctx := context.Background()
	keyArgs := func(t *testing.T, err error) (string, []any) {
		t.Helper()
		var e *response.Error
		if !errors.As(err, &e) {
			t.Fatalf("期望 *response.Error，得到 %T %v", err, err)
		}
		return e.Key, e.Args
	}

	t.Run("超过生效上限", func(t *testing.T) {
		withSettingsImageMB(t, 0.001) // ≈1KB，让超限在单测里造得出来
		_, err := NewService(nil).SaveMedia(ctx, make([]byte, 2048))
		key, args := keyArgs(t, err)
		if key != "website.image_too_large" || len(args) != 1 || args[0] != "0.001MB" {
			t.Errorf("报错 = %s %v，想要 image_too_large 带生效上限 0.001MB", key, args)
		}
	})

	t.Run("位图之外的类型", func(t *testing.T) {
		_, err := NewService(nil).SaveMedia(ctx, []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><script/></svg>"))
		key, args := keyArgs(t, err)
		if key != "website.image_type_unsupported" || len(args) != 1 {
			t.Errorf("报错 = %s %v", key, args)
		}
		if got, ok := args[0].(string); !ok || !strings.Contains(got, "PNG") {
			t.Errorf("报错要带上类型清单，得到 %v", args)
		}
	})

	t.Run("空文件", func(t *testing.T) {
		_, err := NewService(nil).SaveMedia(ctx, nil)
		if key, _ := keyArgs(t, err); key != "website.file_empty" {
			t.Errorf("报错 = %s，想要 website.file_empty", key)
		}
	})
}

// 语言包里这两条必须带参渲染：上限一改，报错跟着变，不会停在写死的「2MB」。
// 谁把 %s 改回死值，Sprintf 的多余参数会露出来，用例就红。
func TestMediaErrorTextsRenderWithLimits(t *testing.T) {
	b, err := i18n.New([]config.Language{{Code: "zh-CN", Name: "简体中文"}, {Code: "en-US", Name: "English"}}, "zh-CN", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	i18n.SetDefault(b)
	t.Cleanup(func() { i18n.SetDefault(nil) })

	lim := NewService(nil).MediaLimits(context.Background())
	lim.MaxMB, lim.MaxBytes = 5, 5*1024*1024
	ctx := context.Background()
	for _, tc := range []struct {
		lang, key, arg, want string
	}{
		{"zh-CN", "website.image_too_large", lim.Text(), "图片不能超过 5MB"},
		{"en-US", "website.image_too_large", lim.Text(), "Images must not exceed 5MB."},
		{"zh-CN", "website.image_type_unsupported", lim.TypesText(), "只支持 PNG / JPEG / WebP / GIF 图片"},
		{"en-US", "website.image_type_unsupported", lim.TypesText(), "Only PNG / JPEG / WebP / GIF images are supported."},
	} {
		if got := i18n.T(i18n.WithLang(ctx, tc.lang), tc.key, tc.arg); got != tc.want {
			t.Errorf("%s %s = %q，想要 %q", tc.lang, tc.key, got, tc.want)
		}
	}
}
