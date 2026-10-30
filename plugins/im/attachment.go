package im

import (
	"encoding/json"
	"path"
	"strings"
	"unicode"
)

// maxUploadBytes 是附件请求体的硬上限（20MB），更细的类型/大小限制见 attachmentRuleFor。
const maxUploadBytes = 20 << 20

// attachmentRule 描述某类可上传文件的落盘扩展名与大小上限。
type attachmentRule struct {
	ext  string
	max  int64
	kind string
}

// attachmentRuleFor 按服务端探测到的 MIME（必要时结合原始文件名）决定是否接收。
// MIME 一律由内容嗅探得出，不信任客户端声明的 Content-Type 与扩展名。
func attachmentRuleFor(mime, filename string) (attachmentRule, bool) {
	switch strings.ToLower(mime) {
	case "image/png":
		return attachmentRule{".png", 8 << 20, KindImage}, true
	case "image/jpeg":
		return attachmentRule{".jpg", 8 << 20, KindImage}, true
	case "image/gif":
		return attachmentRule{".gif", 8 << 20, KindImage}, true
	case "image/webp":
		return attachmentRule{".webp", 8 << 20, KindImage}, true
	case "application/pdf":
		return attachmentRule{".pdf", maxUploadBytes, KindFile}, true
	case "text/plain":
		return attachmentRule{".txt", 2 << 20, KindFile}, true
	case "application/zip":
		// Office 文档本质是 zip，按原始扩展名保留可读的扩展名。
		switch strings.ToLower(path.Ext(filename)) {
		case ".docx":
			return attachmentRule{".docx", maxUploadBytes, KindFile}, true
		case ".xlsx":
			return attachmentRule{".xlsx", maxUploadBytes, KindFile}, true
		case ".pptx":
			return attachmentRule{".pptx", maxUploadBytes, KindFile}, true
		}
		return attachmentRule{".zip", maxUploadBytes, KindFile}, true
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
