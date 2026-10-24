package ai

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// RedactOptions 控制发往上游模型前要脱敏哪些敏感字段。零值 = 全部关闭；默认配置（DefaultRedact）全部开启。
// 脱敏是单向的：占位符不会在返回内容里还原。
type RedactOptions struct {
	Phone    bool `json:"phone"`
	Email    bool `json:"email"`
	IDCard   bool `json:"idCard"`
	BankCard bool `json:"bankCard"`
}

// DefaultRedact 是默认配置：四类全开。
func DefaultRedact() RedactOptions {
	return RedactOptions{Phone: true, Email: true, IDCard: true, BankCard: true}
}

// Any 报告是否至少开启一类。
func (o RedactOptions) Any() bool { return o.Phone || o.Email || o.IDCard || o.BankCard }

// 占位符。校验“模型输出里不应出现占位符”时也用这几个。
const (
	MaskPhone    = "[PHONE]"
	MaskEmail    = "[EMAIL]"
	MaskIDCard   = "[ID_CARD]"
	MaskBankCard = "[BANK_CARD]"
)

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	// 18 位身份证（末位可为 X）；15 位老号码不处理（易与普通数字混淆）。
	idCardRe = regexp.MustCompile(`[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[0-9Xx]`)
	// 银行卡：连续 13~19 位，或每 4 位一组、用空格 / 短横分隔（最后一组 1~4 位）。再用 Luhn 校验降低误伤。
	bankPlainRe  = regexp.MustCompile(`\d{13,19}`)
	bankGroupRe  = regexp.MustCompile(`\d{4}(?:[ \-]\d{4}){2,3}(?:[ \-]\d{1,4})?`)
	phoneRe      = regexp.MustCompile(`(?:\+?86[ \-]?)?1[3-9]\d(?:[ \-]?\d{4}){2}`)
	idCardWeight = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	idCardCheck  = "10X98765432"
)

// Redact 按选项把文本里的敏感字段换成固定占位符。处理顺序是 邮箱 → 身份证 → 银行卡 → 手机号：
// 长的、校验更严的先处理，避免手机号规则吃掉身份证 / 银行卡的一部分。数字类匹配要求前后不是数字，
// 避免从更长的数字串里切出一段。
func Redact(s string, o RedactOptions) string {
	if s == "" || !o.Any() {
		return s
	}
	if o.Email {
		s = emailRe.ReplaceAllString(s, MaskEmail)
	}
	if o.IDCard {
		s = replaceDigits(s, idCardRe, validIDCard, MaskIDCard)
	}
	if o.BankCard {
		s = replaceDigits(s, bankPlainRe, validLuhn, MaskBankCard)
		s = replaceDigits(s, bankGroupRe, validLuhn, MaskBankCard)
	}
	if o.Phone {
		s = replaceDigits(s, phoneRe, nil, MaskPhone)
	}
	return s
}

// ContainsMask 报告文本里是否含脱敏占位符（用于丢弃模型把占位符当成检索关键字的输出）。
func ContainsMask(s string) bool {
	for _, m := range []string{MaskPhone, MaskEmail, MaskIDCard, MaskBankCard} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// replaceDigits 替换 re 的匹配：前后不能紧邻数字，且 valid（可为 nil）通过。
func replaceDigits(s string, re *regexp.Regexp, valid func(string) bool, mask string) string {
	idx := re.FindAllStringIndex(s, -1)
	if len(idx) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range idx {
		start, end := m[0], m[1]
		if start < last || digitBefore(s, start) || digitAfter(s, end) || (valid != nil && !valid(s[start:end])) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(mask)
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func digitBefore(s string, i int) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r >= '0' && r <= '9'
}

func digitAfter(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r >= '0' && r <= '9'
}

// validIDCard 校验 18 位身份证的校验位。
func validIDCard(s string) bool {
	if len(s) != 18 {
		return false
	}
	sum := 0
	for i := 0; i < 17; i++ {
		sum += int(s[i]-'0') * idCardWeight[i]
	}
	want := idCardCheck[sum%11]
	got := s[17]
	if got == 'x' {
		got = 'X'
	}
	return got == want
}

// validLuhn 对（去掉空格 / 短横的）数字串做 Luhn 校验，长度需在 13~19 位。
func validLuhn(s string) bool {
	digits := make([]int, 0, len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if alt {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}
