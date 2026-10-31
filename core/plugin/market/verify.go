package market

// 包完整性校验：市场安装会紧接着 go build / npm build 下载来的源码，
// 等于在构建机上执行任意代码。因此「下载到的字节是不是目录声称的那个包」
// 必须在解压之前先确认——这里是唯一一处校验，安装链路的调用方无需自己实现。
//
// 校验数据来源（按优先级，见 verifyPackage）：
//  1. Entry.Checksum：目录里写死的 SHA-256（发布方离线算好填进目录）。
//  2. Entry.ChecksumURL：目录里声明的校验文件直链（sha256sum 格式）。
//  3. 官网下载端点的现成侧文件：<...>/plugins/<id>/download 自动派生 <...>/download.sha256，
//     不需要人工填字段——官网本来就在提供这个端点。
//  4. 三者都没有（GitHub zipball、无侧文件的普通直链）：见 verify.go 的策略注释。
//
// 校验值来自与包同一个下载通道时，只能证明「传输与缓存没被换掉」，不能证明发布者身份；
// 来源认证仍是 ed25519 签名（sign.go 与官网 /plugins/<id>/download.sig），不要把 SHA-256 当签名用。

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"nexadmin/core/i18n"
)

// EnvRequireChecksum 设为真值时，取不到任何校验值的条目一律拒绝安装（默认只告警放行）。
// 这是部署级开关：让“严格要求”由运维显式选择，而不是由可能被改写的目录条目逐条降级。
const EnvRequireChecksum = "NEXADMIN_MARKET_REQUIRE_CHECKSUM"

// 完整性校验的错误文案 key（core/i18n/locales/{zh-CN,en-US}.yaml 同步维护）。
const (
	keyChecksumMismatch    = "plugin.market.checksum_mismatch"
	keyChecksumInvalid     = "plugin.market.checksum_invalid"
	keyChecksumUnavailable = "plugin.market.checksum_unavailable"
	keyChecksumMissing     = "plugin.market.checksum_missing"
	keyChecksumUnverified  = "plugin.market.checksum_unverified"
)

var checksumRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// errChecksumNotFound 表示校验文件端点没有该资源（区别于网络失败）。
var errChecksumNotFound = errors.New("checksum file not found")

// IntegrityError 是完整性校验失败的错误：带 i18n key 与参数，上屏文案可本地化。
// 语言包未加载（CLI、单测）时按内置兜底文案渲染，保证日志里永远是句子而不是 key。
type IntegrityError struct {
	Key   string // i18n key
	Args  []any  // 渲染参数
	Cause error  // 底层错误（网络、读文件），仅用于日志与 errors.Is/Unwrap
}

func (e *IntegrityError) Error() string { return e.Localized(context.Background()) }

// Unwrap 让 errors.Is 能追到底层网络/文件错误。
func (e *IntegrityError) Unwrap() error { return e.Cause }

// Localized 按 ctx 的语言渲染文案；缺语言包时回退内置中文（与市场其它错误保持同一语言）。
func (e *IntegrityError) Localized(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if msg := i18n.T(ctx, e.Key, e.Args...); msg != e.Key {
		return msg
	}
	return fallbackMessage(e.Key, e.Args...)
}

// fallbackMessage 是语言包缺失时的兜底渲染，与 zh-CN.yaml 的同名 key 保持一致。
func fallbackMessage(key string, args ...any) string {
	tpl, ok := fallbackTemplates[key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, args...)
}

var fallbackTemplates = map[string]string{
	keyChecksumMismatch:    "插件包 %s 的 SHA-256 与目录声明不一致（期望 %s，实际 %s），已拒绝安装并删除下载文件",
	keyChecksumInvalid:     "目录条目 %s 的 checksum %q 不是合法的 SHA-256（64 位十六进制），已拒绝安装",
	keyChecksumUnavailable: "无法获取插件包 %s 的 SHA-256 校验文件 %s（%v），已拒绝安装",
	keyChecksumMissing:     "插件包 %s 没有可用的 SHA-256 校验值，已按 %s 拒绝安装；请在目录条目里补 checksum/checksum_url，或改用官网下载端点",
	keyChecksumUnverified:  "插件包 %s（来源 %s）没有可用的 SHA-256 校验值，安装内容未经完整性校验；建议补 checksum/checksum_url 或改用官网下载端点，严格模式可设 %s=1",
}

// ValidChecksum 报告 s 是否是合法的 SHA-256 十六进制串（大小写均可）。
func ValidChecksum(s string) bool { return checksumRe.MatchString(strings.TrimSpace(s)) }

// NormalizeChecksum 校验收缩为小写十六进制；格式不合法返回错误。
func NormalizeChecksum(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !checksumRe.MatchString(s) {
		return "", fmt.Errorf("不是 64 位十六进制的 SHA-256: %q", s)
	}
	return strings.ToLower(s), nil
}

// ParseChecksumFile 解析 sha256sum 风格的校验文本：接受 "<hash>"、"<hash>  <文件名>"
// 与 "<hash> *<文件名>"，容忍前置注释行，也认得 `SHA256 (name) = <hash>` 形式
// ——取第一条 64 位十六进制记录，摘要算法不对（如 MD5）就当没有记录。
func ParseChecksumFile(text string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		for _, field := range strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "\\")) {
			if sum, err := NormalizeChecksum(field); err == nil {
				return sum, nil
			}
		}
	}
	return "", fmt.Errorf("校验文件里没有 SHA-256 记录: %q", firstLine(text))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return strings.TrimSpace(s)
}

// FileChecksum 返回文件的 SHA-256（小写十六进制）。
func FileChecksum(path string) (string, error) {
	sum, err := fileDigest(path)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

// sidecarChecksumURL 从官网市场下载端点派生校验文件地址：
// 只有路径以 /download 结尾（即 website 插件的 /plugins/:id/download）时才认得出来，
// 普通 zip 直链派生不出可信的侧文件，返回空串。
func sidecarChecksumURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Path, "/download") {
		return ""
	}
	u.Path = strings.TrimSuffix(u.Path, "/download") + "/download.sha256"
	return u.String()
}

// RequireChecksum 读取部署级严格开关。
func RequireChecksum() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvRequireChecksum))) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	}
	return false
}

// verifyPackage 在解压之前确认 zip 的 SHA-256 与可用的校验来源一致。
// srcURL 是实际下载地址（用于派生官网侧文件）。失败返回 *IntegrityError，
// 由调用方删除临时 zip——校验失败的包不会进入解压与安装。
func verifyPackage(ctx context.Context, zipPath, srcURL string, e Entry) error {
	want, err := expectedChecksum(ctx, e, srcURL)
	if err != nil {
		return err
	}
	got, err := FileChecksum(zipPath)
	if err != nil {
		return fmt.Errorf("读取下载文件失败: %w", err)
	}
	if want == "" {
		// 没有任何校验来源：默认放行但显式告警（不静默），严格开关打开则拒绝。
		if RequireChecksum() {
			return &IntegrityError{Key: keyChecksumMissing, Args: []any{e.ID, EnvRequireChecksum}}
		}
		msg := (&IntegrityError{Key: keyChecksumUnverified, Args: []any{e.ID, srcURL, EnvRequireChecksum}}).Localized(ctx)
		fmt.Fprintf(installWarn, "警告：%s\n", msg)
		return nil
	}
	if got != want {
		return &IntegrityError{Key: keyChecksumMismatch, Args: []any{e.ID, want, got}}
	}
	return nil
}

// expectedChecksum 解析“期望的 SHA-256”，空串表示没有可用的校验来源。
func expectedChecksum(ctx context.Context, e Entry, srcURL string) (string, error) {
	if s := strings.TrimSpace(e.Checksum); s != "" {
		sum, err := NormalizeChecksum(s)
		if err != nil {
			return "", &IntegrityError{Key: keyChecksumInvalid, Args: []any{e.ID, s}, Cause: err}
		}
		return sum, nil
	}
	if s := strings.TrimSpace(e.ChecksumURL); s != "" {
		sum, err := fetchChecksum(ctx, s)
		if err != nil {
			return "", &IntegrityError{Key: keyChecksumUnavailable, Args: []any{e.ID, s, err}, Cause: err}
		}
		return sum, nil
	}
	// 官网下载端点：侧文件是现成的，拿不到（404）才退回默认策略，其它错误一律拒绝。
	side := sidecarChecksumURL(srcURL)
	if side == "" {
		return "", nil
	}
	sum, err := fetchChecksum(ctx, side)
	switch {
	case err == nil:
		return sum, nil
	case errors.Is(err, errChecksumNotFound):
		return "", nil
	default:
		return "", &IntegrityError{Key: keyChecksumUnavailable, Args: []any{e.ID, side, err}, Cause: err}
	}
}

// fetchChecksum 拉取校验文件文本并解析出 SHA-256（与包下载同一套 HTTPS/跳转约束）。
func fetchChecksum(ctx context.Context, raw string) (string, error) {
	text, err := fetchChecksumText(ctx, raw)
	if err != nil {
		return "", err
	}
	sum, err := ParseChecksumFile(text)
	if err != nil {
		return "", err
	}
	return sum, nil
}
