// Package idcard 解析中国大陆居民身份证号码（GB 11643-1999）：归一化、格式与校验位
// 验证、出生日期/性别/归属地区划码提取，以及 15 位升 18 位。
//
// 本包不依赖 core：区划码到名称由调用方注入 RegionFunc（框架侧通常传 core/region.Name），
// 这样 core、plugins、派生项目任意一层都能引用，也不会出现 pkg→core 的反向依赖。
//
// 边界：本包只能判断「号码自己合不合规则」。核验「确有其人、与姓名是否一致」
// 必须走实名核验服务，本包不含该能力，也不要假装能替代它。
package idcard

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Reason 是校验失败的机器码，由调用方映射到自己的文案（后端 i18n key 或前端 locales）。
type Reason string

const (
	ReasonEmpty    Reason = "empty"
	ReasonLength   Reason = "length"
	ReasonChars    Reason = "chars"
	ReasonRegion   Reason = "region"
	ReasonBirthday Reason = "birthday"
	ReasonFuture   Reason = "birthday_future"
	ReasonChecksum Reason = "checksum"
)

// 性别取值，与业务层的展示文案解耦。
const (
	SexMale   = "male"
	SexFemale = "female"
)

// RegionFunc 解析 6 位区划码，返回最深命中层级的省/市/县路径与层级（1 省 / 2 地 / 3 县）。
// ok=false 只用于连省级都认不出的码。
//
// 刻意不把"查不到县级"当错误：区划数据粒度、撤县设区、快照年份都会造成这种情况，
// 号码合法性由校验位与出生日期决定，区划只是附带信息。
type RegionFunc func(code string) (path []string, level int, ok bool)

// Info 是解析成功的结果。Number 是归一化后的 18 位号码，含可能的 15 位升级。
type Info struct {
	Number      string
	Legacy      bool // 由 15 位号码升级而来（15 位本身没有校验位）
	RegionCode  string
	RegionNames []string // 注入 RegionFunc 且命中时才有值，最深只到实际命中的层级
	RegionLevel int      // 1 省 / 2 地 / 3 县
	Birthday    time.Time
	Sex         string
	CheckBit    string
}

// AgeAt 按 now 计算周岁；生日当天算已满岁，未到则减一岁。
func (i Info) AgeAt(now time.Time) int {
	age := now.Year() - i.Birthday.Year()
	birthdayThisYear := time.Date(now.Year(), i.Birthday.Month(), i.Birthday.Day(), 0, 0, 0, 0, now.Location())
	if now.Before(birthdayThisYear) {
		age--
	}
	if age < 0 {
		return 0
	}
	return age
}

// Error 带机器码，调用方按 Reason 出文案，不要匹配 Error 字符串。
type Error struct {
	Reason Reason
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return "idcard: " + string(e.Reason)
	}
	return "idcard: " + string(e.Reason) + " (" + e.Detail + ")"
}

// ReasonOf 取 Parse/Upgrade 返回值的机器码；不是本包的错误时返回空串。
func ReasonOf(err error) Reason {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

var (
	weights  = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	checkSet = [11]byte{'1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2'}
)

// Normalize 去掉空白与连字符、把全角数字转半角、把末位 x 转大写。
// 其它位置的字母保持不变，交给校验环节报 chars，避免把错误号码"修"成另一个号码。
func Normalize(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\u3000' || r == '-' || r == '_':
			continue
		case r >= '０' && r <= '９':
			b.WriteByte(byte('0' + (r - '０')))
		case r == 'ｘ' || r == 'Ｘ':
			b.WriteByte('X')
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 0 && s[len(s)-1] == 'x' {
		s = s[:len(s)-1] + "X"
	}
	return s
}

// Checksum 计算 17 位号码体的校验位；长度不符或含非数字返回错误。
func Checksum(body string) (byte, error) {
	if len(body) != 17 {
		return 0, &Error{Reason: ReasonLength, Detail: strconv.Itoa(len(body))}
	}
	sum := 0
	for i := 0; i < 17; i++ {
		d := body[i] - '0'
		if d > 9 {
			return 0, &Error{Reason: ReasonChars, Detail: body}
		}
		sum += int(d) * weights[i]
	}
	return checkSet[sum%11], nil
}

// Upgrade 归一化后把 15 位号码补成 18 位；已是 18 位的原样返回。
// 它只补校验位，不校验区划码与出生日期。
func Upgrade(raw string) (string, error) {
	n, _, err := to18(raw)
	return n, err
}

// Parse 归一化并校验号码：长度 → 字符 → 区划码 → 出生日期 → 未来日期 → 校验位。
// region 传 nil 时跳过区划码存在性校验，只按号码规则判定。
func Parse(raw string, region RegionFunc) (Info, error) {
	n, legacy, err := to18(raw)
	if err != nil {
		return Info{}, err
	}
	info := Info{Number: n, Legacy: legacy, RegionCode: n[:6], CheckBit: n[17:]}

	if region != nil {
		path, level, ok := region(info.RegionCode)
		if !ok {
			return Info{}, &Error{Reason: ReasonRegion, Detail: info.RegionCode}
		}
		info.RegionNames, info.RegionLevel = path, level
	}

	birthday, err := time.Parse("20060102", n[6:14])
	if err != nil || birthday.Year() < 1900 {
		return Info{}, &Error{Reason: ReasonBirthday, Detail: n[6:14]}
	}
	if birthday.After(time.Now().UTC()) {
		return Info{}, &Error{Reason: ReasonFuture, Detail: n[6:14]}
	}
	info.Birthday = birthday

	if !legacy {
		want, err := Checksum(n[:17])
		if err != nil {
			return Info{}, err
		}
		if want != n[17] {
			return Info{}, &Error{Reason: ReasonChecksum, Detail: string(want)}
		}
	}

	info.Sex = SexFemale
	if (n[16]-'0')%2 == 1 {
		info.Sex = SexMale
	}
	return info, nil
}

// to18 归一化并补齐成 18 位，legacy 标记是否由 15 位升级而来。
func to18(raw string) (number string, legacy bool, err error) {
	n := Normalize(raw)
	if n == "" {
		return "", false, &Error{Reason: ReasonEmpty}
	}
	switch len(n) {
	case 18:
		for i := 0; i < 17; i++ {
			if n[i] < '0' || n[i] > '9' {
				return "", false, &Error{Reason: ReasonChars, Detail: n}
			}
		}
		if last := n[17]; last != 'X' && (last < '0' || last > '9') {
			return "", false, &Error{Reason: ReasonChars, Detail: n}
		}
		return n, false, nil
	case 15:
		for i := 0; i < 15; i++ {
			if n[i] < '0' || n[i] > '9' {
				return "", false, &Error{Reason: ReasonChars, Detail: n}
			}
		}
		body := n[:6] + "19" + n[6:]
		check, err := Checksum(body)
		if err != nil {
			return "", false, err
		}
		return body + string(check), true, nil
	default:
		return "", false, &Error{Reason: ReasonLength, Detail: strconv.Itoa(len(n))}
	}
}
