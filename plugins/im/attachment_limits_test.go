package im

import (
	"context"
	"testing"

	"nexadmin/core"
)

// IM 附件的单文件上限跟随系统参数「上传限制」，未设时保持内置默认，并按传输硬顶截顶。

func withImageFileMB(t *testing.T, imageMB, fileMB float64) {
	t.Helper()
	core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) {
		return core.UploadLimits{MaxImageMB: imageMB, MaxFileMB: fileMB}, nil
	})
	t.Cleanup(func() { core.SetUploadLimitsSource(nil) })
}

func TestAttachmentCeilingsFor(t *testing.T) {
	mb := func(v float64) int64 { return int64(v * 1024 * 1024) }
	ctx := context.Background()

	t.Run("未登记设置页时保持内置默认", func(t *testing.T) {
		core.SetUploadLimitsSource(nil)
		c := attachmentCeilingsFor(ctx)
		if c.image != mb(8) || c.doc != mb(20) || c.text != mb(2) || c.maxBody != mb(20) {
			t.Errorf("默认上限 = %+v，想要 image 8MB doc 20MB text 2MB body 20MB", c)
		}
	})

	t.Run("图片与文档各自跟随参数", func(t *testing.T) {
		withImageFileMB(t, 2, 40)
		c := attachmentCeilingsFor(ctx)
		if c.image != mb(2) || c.doc != mb(40) || c.text != mb(2) {
			t.Errorf("上限 = %+v，想要 image 2MB doc 40MB text 2MB（文本不吃配置）", c)
		}
		if c.maxBody != mb(40) {
			t.Errorf("请求体硬顶 = %d，应随最大的那档放到 40MB", c.maxBody)
		}
		rule, ok := attachmentRuleFor(c, "image/png", "a.png")
		if !ok || rule.max != mb(2) {
			t.Errorf("png 规则 = %+v ok=%v，应使用生效的图片上限", rule, ok)
		}
	})

	t.Run("参数为 0 表示不限制，保持内置默认而不是放开", func(t *testing.T) {
		withImageFileMB(t, 0, 0)
		c := attachmentCeilingsFor(ctx)
		if c.image != mb(8) || c.doc != mb(20) {
			t.Errorf("上限 = %+v，0 应回落到内置默认", c)
		}
	})

	t.Run("离谱参数被传输硬顶截住", func(t *testing.T) {
		withImageFileMB(t, 1000, 1000)
		c := attachmentCeilingsFor(ctx)
		if c.image != mb(50) || c.doc != mb(50) || c.maxBody != mb(50) {
			t.Errorf("上限 = %+v，应截到 %d 字节", c, int64(imUploadCeilingBytes))
		}
	})

	t.Run("报错里的数字就是生效上限", func(t *testing.T) {
		withImageFileMB(t, 2.5, 0)
		c := attachmentCeilingsFor(ctx)
		if got := c.mbText(c.image); got != "2.5MB" {
			t.Errorf("mbText = %q，想要 2.5MB", got)
		}
		if got := c.mbText(mb(20)); got != "20MB" {
			t.Errorf("mbText = %q，想要 20MB", got)
		}
	})

	// 成对边界：正好等于上限放行、多一字节拒。两条同时成立才说明闸是真的，不是空转。
	t.Run("成对边界：等于上限通过，多一字节拒", func(t *testing.T) {
		withImageFileMB(t, 2, 30)
		c := attachmentCeilingsFor(ctx)
		for _, tc := range []struct{ mime, name string }{{"image/png", "a.png"}, {"application/pdf", "a.pdf"}} {
			rule, ok := attachmentRuleFor(c, tc.mime, tc.name)
			if !ok {
				t.Fatalf("%s 应当可收", tc.mime)
			}
			if !attachmentSizeOK(rule, rule.max) {
				t.Errorf("%s：恰好上限 %d 字节被拒", tc.mime, rule.max)
			}
			if attachmentSizeOK(rule, rule.max+1) {
				t.Errorf("%s：超上限 1 字节（%d）被放行", tc.mime, rule.max+1)
			}
			if attachmentSizeOK(rule, 0) {
				t.Errorf("%s：空文件被放行", tc.mime)
			}
		}
	})
}
