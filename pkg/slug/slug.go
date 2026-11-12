// Package slug 是全站统一的 URL slug 生成器，对齐行业标准口径（WordPress / Django / GitHub 的公约数）：
//
//  1. NFKD 分解并去掉变音符号（é → e，ü → u）；
//  2. 转小写；
//  3. 连续的「非 [a-z0-9]」字符压成单个连字符；
//  4. 去掉首尾连字符；
//  5. 截到 maxLen，截完去掉尾巴上悬空的连字符；
//  6. 结果为空（纯中文等无法转写的输入）时回退到随机 base36 串，而不是把原始字符塞进 URL。
//
// 产出口径即 plugins/website 的 slugReg：^[a-z0-9][a-z0-9-]*$、首尾无连字符、≤64。
// 撞名的让号（-2、-3…）由各表的存储层负责——谁拥有唯一索引，谁负责让号。
package slug

import (
	"crypto/rand"
	"math/big"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxLen 是 slug 列的统一上限（各表 slug 列宽均为 64）。
const MaxLen = 64

// randomLen 是兜底随机串的长度：8 位 base36 ≈ 2.8e12 组合，撞名概率可忽略，还有让号机制兜底。
const randomLen = 8

// Slugify 按行业标准把任意标题压成 slug；无法转写时返回空串，由调用方决定兜底。
func Slugify(s string, maxLen int) string {
	s = norm.NFKD.String(strings.ToLower(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case unicode.Is(unicode.Mn, r):
			// 变音符号本体（NFKD 分解出来的 combining mark）直接丢弃
		default:
			// 空白、标点、CJK、emoji……一切非字母数字都当作分隔符，压成单个连字符
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return Clamp(strings.Trim(b.String(), "-"), maxLen, "")
}

// From 生成 slug；标题压不出拉丁字符（纯中文等）时回退随机串。
func From(s string, maxLen int) string {
	if out := Slugify(s, maxLen); out != "" {
		return out
	}
	return Random()
}

// Clamp 把 slug 截进列宽；截完去掉尾部悬空连字符；为空时回退 fallback（可传空串表示「交给调用方」）。
func Clamp(s string, maxLen int, fallback string) string {
	if len(s) > maxLen {
		s = strings.TrimRight(s[:maxLen], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}

// Random 是兜底 slug：小写 base36 随机串（不带前缀，形态与自然 slug 一致）。
func Random() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, randomLen)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			// crypto/rand 失败极罕见；退化为纳秒时间戳的 base36，仍然可用
			return clamp36(time.Now().UnixNano())
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out)
}

func clamp36(n int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, randomLen)
	for n > 0 && len(out) < randomLen {
		out = append(out, digits[n%36])
		n /= 36
	}
	for len(out) < randomLen {
		out = append(out, '0')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}
