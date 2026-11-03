package im

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"unicode"

	"nexadmin/core"
)

// 附件上限的内置默认（系统参数「上传限制」没设时用这些值）。
const (
	maxUploadBytes       = 20 << 20 // 文档/压缩包类与请求体的缺省上限
	attachmentImageBytes = 8 << 20  // 图片类缺省上限
	attachmentTextBytes  = 2 << 20  // 纯文本：内容尺度，不当配额开放
	imUploadCeilingBytes = 50 << 20 // 传输层绝对硬顶，见 attachmentCeilingsFor
)

// attachmentCeilings 是一次上传请求生效的上限。
type attachmentCeilings struct {
	image   int64
	doc     int64
	text    int64
	maxBody int64 // 请求体硬上限，取三者最大值
}

func defaultAttachmentCeilings() attachmentCeilings {
	return attachmentCeilings{image: attachmentImageBytes, doc: maxUploadBytes, text: attachmentTextBytes}
}

// attachmentCeilingsFor 解析生效上限：系统参数「上传限制」> 内置默认，并按传输硬顶截顶
// （换算口径统一在 core.ResolveUploadCeiling）。图片类跟 upload.max_size_image_mb，
// 文档/压缩包跟 upload.max_size_file_mb；附件流式落存储不整读进内存，但仍截顶，免得有人把
// 参数调成离谱值后拿聊天附件接口当大文件搬运。文本不吃配置：它是内容尺度不是配额。
func attachmentCeilingsFor(ctx context.Context) attachmentCeilings {
	c := defaultAttachmentCeilings()
	if l, ok := core.UploadLimitsFromSettings(ctx); ok {
		c.image = core.ResolveUploadCeiling(l.MaxImageMB, c.image, imUploadCeilingBytes)
		c.doc = core.ResolveUploadCeiling(l.MaxFileMB, c.doc, imUploadCeilingBytes)
	}
	c.maxBody = max(attachmentTextBytes, max(c.image, c.doc))
	return c
}

// mbText 是给人看的上限写法，如 8MB、2.5MB（格式化口径在 core.MBText）。
func (c attachmentCeilings) mbText(bytes int64) string { return core.MBText(bytes) }

// AttachmentLimits 是附件上传的生效限制，前端按它渲染文件选择器（accept）。
type AttachmentLimits struct {
	Accept     string `json:"accept"`
	ImageBytes int64  `json:"imageBytes"`
	DocBytes   int64  `json:"docBytes"`
	TextBytes  int64  `json:"textBytes"`
	MaxBody    int64  `json:"maxBodyBytes"`
}

// AttachmentLimitsFor 返回当前生效的附件限制：大小来自系统参数「上传限制」（见 attachmentCeilingsFor），
// 类型来自服务端白名单。前端不再手抄，两处一起动才不会漂移。
func AttachmentLimitsFor(ctx context.Context) AttachmentLimits {
	c := attachmentCeilingsFor(ctx)
	return AttachmentLimits{Accept: attachmentAccept(), ImageBytes: c.image, DocBytes: c.doc, TextBytes: c.text, MaxBody: c.maxBody}
}

// attachmentAccept 是下发给前端文件选择器的 accept，与 attachmentRuleFor 的白名单同源：
// 后端不收的类型不在这里出现，前端不再自己手抄一份（有用例逐条比对两边）。
// .zip/.docx/.xlsx/.pptx 是同一 MIME（zip 外壳）按扩展名细分，所以四种都要列。
func attachmentAccept() string {
	return strings.Join([]string{
		"image/png", "image/jpeg", "image/gif", "image/webp",
		"application/pdf", "text/plain", ".zip", ".docx", ".xlsx", ".pptx",
	}, ",")
}

// attachmentSizeOK 是尺寸的放行判据：正好等于该类型上限通过，多一字节拒（<=0 视为没有内容）。
// 类型与尺寸分开判：两个原因要给出两个错误码，不能合成一句「上传失败」。
func attachmentSizeOK(rule attachmentRule, size int64) bool {
	return size > 0 && size <= rule.max
}

// attachmentRule 描述某类可上传文件的落盘扩展名与大小上限。
type attachmentRule struct {
	ext  string
	max  int64
	kind string
}

// attachmentRuleFor 按服务端探测到的 MIME（必要时结合原始文件名）决定是否接收。
// MIME 一律由内容嗅探得出，不信任客户端声明的 Content-Type 与扩展名。
func attachmentRuleFor(ceil attachmentCeilings, mime, filename string) (attachmentRule, bool) {
	switch strings.ToLower(mime) {
	case "image/png":
		return attachmentRule{".png", ceil.image, KindImage}, true
	case "image/jpeg":
		return attachmentRule{".jpg", ceil.image, KindImage}, true
	case "image/gif":
		return attachmentRule{".gif", ceil.image, KindImage}, true
	case "image/webp":
		return attachmentRule{".webp", ceil.image, KindImage}, true
	case "application/pdf":
		return attachmentRule{".pdf", ceil.doc, KindFile}, true
	case "text/plain":
		return attachmentRule{".txt", ceil.text, KindFile}, true
	case "application/zip":
		// Office 文档本质是 zip，按原始扩展名保留可读的扩展名。
		switch strings.ToLower(path.Ext(filename)) {
		case ".docx":
			return attachmentRule{".docx", ceil.doc, KindFile}, true
		case ".xlsx":
			return attachmentRule{".xlsx", ceil.doc, KindFile}, true
		case ".pptx":
			return attachmentRule{".pptx", ceil.doc, KindFile}, true
		}
		return attachmentRule{".zip", ceil.doc, KindFile}, true
	}
	return attachmentRule{}, false
}

// safeFilename 去掉路径与控制字符，只保留展示用文件名。
func safeFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "file"
	}
	return truncate(out, 120)
}

// attachmentMeta 生成消息 meta：{"attachment":{"token","name","size","mime"}}。
func attachmentMeta(att *Attachment) string {
	b, err := json.Marshal(map[string]any{"attachment": map[string]any{
		"token": att.Token, "name": att.Name, "size": att.Size, "mime": att.Mime,
	}})
	if err != nil {
		return ""
	}
	return string(b)
}
