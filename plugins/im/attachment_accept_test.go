package im

import (
	"strings"
	"testing"
)

// accept 必须与 attachmentRuleFor 的白名单同一套：后端不收的不许出现在选择器里，
// 后端收的也不能漏（漏了会让人以为传不了）。改白名单时这条用例会逼着两处一起改。
func TestAttachmentAcceptMatchesWhitelist(t *testing.T) {
	acc := attachmentAccept()
	ceil := defaultAttachmentCeilings()

	// 白名单里的 MIME 都要能被 accept 表达：zip 外壳用扩展名表达，单列。
	for _, mime := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain"} {
		if _, ok := attachmentRuleFor(ceil, mime, "a.bin"); !ok {
			t.Errorf("%s 在 accept 里，但白名单不收它", mime)
		}
		if !strings.Contains(acc, mime) {
			t.Errorf("accept 少了白名单收的 %s：%s", mime, acc)
		}
	}
	for _, ext := range []string{".zip", ".docx", ".xlsx", ".pptx"} {
		if _, ok := attachmentRuleFor(ceil, "application/zip", "a"+ext); !ok {
			t.Errorf("%s 在 accept 里，但白名单按扩展名不收它", ext)
		}
		if !strings.Contains(acc, ext) {
			t.Errorf("accept 少了 zip 外壳的 %s：%s", ext, acc)
		}
	}

	// 反向：白名单不收的类型不许出现在 accept 里（svg 可携带脚本，exe 是载荷）。
	for _, bad := range []string{"text/html", "image/svg+xml", "application/x-msdownload", "video/mp4", ".svg", ".exe"} {
		if _, ok := attachmentRuleFor(ceil, bad, "a.bin"); ok {
			t.Errorf("%s 不该被白名单收", bad)
		}
		if strings.Contains(acc, bad) {
			t.Errorf("accept 列了白名单不收的 %s：%s", bad, acc)
		}
	}
}
