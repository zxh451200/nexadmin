package file

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/response"
	"nexadmin/core/storage"
)

const (
	apiBase   = "/api/v1"
	signedTTL = time.Hour // 私有文件签名链接有效期
)

// store 持有文件模块的存储：blobs 是对象存储（core/storage，本地目录或 S3），
// 上传中的临时文件始终在本地 <dir>/tmp（tus 分片先落本地，完成后再一次性交给 blobs）。
type store struct {
	root   string
	secret []byte
	blobs  storage.Storage
	// presignRedirect 为 true 时，私有文件的签名链接校验通过后 302 到存储的预签名 URL（默认由后端代理）。
	presignRedirect bool
}

// newStore 创建本地目录存储：<dir>/files/<租户>/<yyyymmdd>/<雪花ID><扩展名>。
func newStore(dataDir string) (*store, error) { return newStoreWith(dataDir, storage.Config{}) }

// newStoreWith 按 storage.* 配置创建存储；driver 为 s3 时对象放进 bucket，本地只留 tmp。
func newStoreWith(dataDir string, cfg storage.Config) (*store, error) {
	root := filepath.Join(dataDir, "uploads")
	for _, d := range []string{"files", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			return nil, fmt.Errorf("file: create upload dir: %w", err)
		}
	}
	secret, err := loadOrCreateSecret(filepath.Join(dataDir, "file.secret"))
	if err != nil {
		return nil, err
	}
	blobs, err := storage.New(cfg, filepath.Join(root, "files"))
	if err != nil {
		return nil, fmt.Errorf("file: init storage: %w", err)
	}
	return &store{root: root, secret: secret, blobs: blobs, presignRedirect: cfg.Driver == "s3" && cfg.S3.PresignRedirect}, nil
}

// loadOrCreateSecret 读取或生成签名密钥（权限 0600）。
func loadOrCreateSecret(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	s := []byte(hex.EncodeToString(buf))
	if err := os.WriteFile(path, s, 0o600); err != nil {
		return nil, fmt.Errorf("file: write secret: %w", err)
	}
	return s, nil
}

func (s *store) tmpPath(id string) string  { return filepath.Join(s.root, "tmp", id+".part") }
func (s *store) infoPath(id string) string { return filepath.Join(s.root, "tmp", id+".json") }

// finalRel 生成对象键：<租户>/<yyyymmdd>/<雪花ID><扩展名>。键里不含用户原始文件名（原名只存库）。
// 历史记录里的 storage_path 保持原样（旧的 <年月> 目录），仍可读取。
func finalRel(tenantID, id, name string, now time.Time) string {
	return storage.ObjectKey(tenantID, id, safeExt(name), now)
}

// isInline 决定 Content-Disposition：图片、视频、PDF 内联预览，其余一律附件下载。
func isInline(category, mimeType string) bool {
	return category == "image" || category == "video" || mimeType == "application/pdf"
}

// putOptions 生成写入对象时的 Content-Type / Content-Disposition（原名经 RFC 6266 编码，非 ASCII 也安全）。
func putOptions(name, mimeType, category, sha string) storage.PutOptions {
	disp := dispositionAttachment(name)
	if isInline(category, mimeType) {
		if d := mime.FormatMediaType("inline", map[string]string{"filename": name}); d != "" {
			disp = d
		}
	}
	return storage.PutOptions{ContentType: mimeType, ContentDisposition: disp, SHA256: sha}
}

// safeExt 只保留字母数字的扩展名，避免奇怪字符进入磁盘路径。
func safeExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if len(ext) > 10 {
		return ""
	}
	for _, r := range ext[min(1, len(ext)):] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return ext
}

// randomToken 生成公开文件路径里的随机串。
func randomToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sign 对“文件 ID + 过期时间”做 HMAC 签名。
func (s *store) sign(id string, exp int64) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(id + "." + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(m.Sum(nil))
}

// signedURL 生成私有文件的短期签名链接。
func (s *store) signedURL(id string) string {
	exp := time.Now().Add(signedTTL).Unix()
	return fmt.Sprintf("%s/files/%s/content?exp=%d&sig=%s", apiBase, id, exp, s.sign(id, exp))
}

// verify 校验签名链接。
func (s *store) verify(id, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(s.sign(id, exp)), []byte(sig))
}

// classify 与前端 mock 的分类规则一致：image / video / doc / other。
func classify(mimeType, name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasPrefix(mimeType, "image/") || hasExt(n, ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg"):
		return "image"
	case strings.HasPrefix(mimeType, "video/") || hasExt(n, ".mp4", ".webm", ".mov"):
		return "video"
	case strings.HasPrefix(mimeType, "text/") || containsAny(mimeType, "pdf", "word", "excel", "sheet", "presentation", "document") ||
		hasExt(n, ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt"):
		return "doc"
	}
	return "other"
}

func hasExt(name string, exts ...string) bool {
	for _, e := range exts {
		if strings.HasSuffix(name, e) {
			return true
		}
	}
	return false
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// checkContent 按真实文件头校验类型（不信扩展名和 Content-Type），返回用于记录的 MIME。
// kind 是 image / video / file。
func checkContent(kind, declared, name string, head []byte) (string, error) {
	sniffed := http.DetectContentType(head)
	base, _, _ := mime.ParseMediaType(sniffed)
	switch kind {
	case "image":
		if strings.HasPrefix(base, "image/") {
			return base, nil
		}
		if looksSVG(head) {
			return "image/svg+xml", nil
		}
		return "", response.New(http.StatusUnsupportedMediaType, "file.content_not_image")
	case "video":
		if strings.HasPrefix(base, "video/") || base == "application/ogg" {
			return base, nil
		}
		// mov 等 ftyp 封装 Go 嗅探不识别，单独放行
		if len(head) >= 12 && string(head[4:8]) == "ftyp" {
			if safeDeclaredMIME(declared) {
				return declared, nil
			}
			return "video/quicktime", nil
		}
		return "", response.New(http.StatusUnsupportedMediaType, "file.content_not_video")
	}
	// 普通文件：不允许把 HTML/脚本当文档存进来（同域下会变成 XSS）
	if base == "text/html" {
		return "", response.New(http.StatusUnsupportedMediaType, "file.html_forbidden")
	}
	if strings.HasPrefix(base, "image/") || strings.HasPrefix(base, "video/") || base == "application/pdf" {
		return base, nil
	}
	// 客户端声明的类型只在安全可用时采信：HTML/SVG/脚本会被同域直出成页面。
	if safeDeclaredMIME(declared) {
		return declared, nil
	}
	if byExt := mime.TypeByExtension(filepath.Ext(name)); byExt != "" && safeDeclaredMIME(byExt) {
		return byExt, nil
	}
	return base, nil
}

// safeDeclaredMIME 判断客户端声明类型能否直接作为响应类型：页面/脚本类一律不采信。
func safeDeclaredMIME(v string) bool {
	if v == "" {
		return false
	}
	m, _, err := mime.ParseMediaType(v)
	if err != nil || m == "" {
		return false
	}
	switch strings.ToLower(m) {
	case "text/html", "application/xhtml+xml", "image/svg+xml",
		"application/javascript", "text/javascript", "application/x-javascript", "application/ecmascript":
		return false
	}
	return true
}

func looksSVG(head []byte) bool {
	s := strings.ToLower(strings.TrimSpace(string(head)))
	return strings.HasPrefix(s, "<svg") || (strings.HasPrefix(s, "<?xml") && strings.Contains(s, "<svg"))
}

// typeAllowed 与前端 precheck 一致：白名单为空表示不限制；支持 image/*、.ext、完整 MIME。
func typeAllowed(allowed []string, declared, name string) bool {
	if len(allowed) == 0 {
		return true
	}
	lname := strings.ToLower(name)
	ld := strings.ToLower(declared)
	for _, a := range allowed {
		p := strings.ToLower(strings.TrimSpace(a))
		switch {
		case p == "":
		case strings.HasSuffix(p, "/*"):
			if strings.HasPrefix(ld, strings.TrimSuffix(p, "*")) {
				return true
			}
		case strings.HasPrefix(p, "."):
			if strings.HasSuffix(lname, p) {
				return true
			}
		case ld == p:
			return true
		}
	}
	return false
}
