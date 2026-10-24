// Package excel 是一个最小的 .xlsx 读写实现，只依赖标准库（archive/zip + encoding/xml）。
//
// 写（Writer）：多 sheet、表头加粗并冻结首行、列宽按内容估算、数字/字符串/布尔/日期类型正确、
// 字符串统一走 sharedStrings、过滤 XML 非法字符、公式注入防护，行数据流式写入 io.Writer。
// 读（Reader）：读取指定 sheet，解析 sharedStrings / inlineStr / 数字 / 布尔 / 日期（按单元格样式判断），
// 稀疏行列补空，合并单元格只取左上值；对 zip 炸弹、实体膨胀、超大行列做限制。
//
// 局限：没有用真实 Excel / WPS 打开验证过；不支持公式计算（读缓存值）、图片、图表、加密文件、.xls。
package excel

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	// ErrNotXLSX 表示输入不是合法的 xlsx 压缩包（不是 zip，或缺少必要部件）。
	ErrNotXLSX = errors.New("excel: not a valid xlsx file")
	// ErrMalformed 表示 XML 内容损坏或含有被禁止的结构（如 DTD）。
	ErrMalformed = errors.New("excel: malformed xlsx content")
	// ErrTooLarge 表示超出大小、文件数、行数、列数等资源限制。
	ErrTooLarge = errors.New("excel: xlsx exceeds resource limits")
)

// Excel 的硬上限。
const (
	MaxSheetRows  = 1048576
	MaxSheetCols  = 16384
	MaxCellRunes  = 32767 // 单元格文本最大字符数
	maxSheetName  = 31
	excelEpochDay = 25569 // 1970-01-01 对应的 1900 日期系统序列值
)

// colName 把 0 起的列号转成 A、B、…、AA。
func colName(i int) string {
	var b [8]byte
	n := len(b)
	for i++; i > 0; i = (i - 1) / 26 {
		n--
		b[n] = byte('A' + (i-1)%26)
	}
	return string(b[n:])
}

// parseRef 解析 "B12" 为 (列 0 起, 行 1 起)；格式不对返回 ok=false。
func parseRef(ref string) (col, row int, ok bool) {
	i := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A') + 1
		i++
		if col > MaxSheetCols+1 {
			return 0, 0, false
		}
	}
	if i == 0 || i == len(ref) {
		return 0, 0, false
	}
	r, err := strconv.Atoi(ref[i:])
	if err != nil || r < 1 {
		return 0, 0, false
	}
	return col - 1, r, true
}

// validXMLRune 按 XML 1.0 Char 产生式判断。
func validXMLRune(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD ||
		(r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
}

// cleanText 删除 XML 不允许的控制字符与非法 UTF-8，并按 Excel 上限截断。
func cleanText(s string) string {
	ok := utf8.RuneCountInString(s) <= MaxCellRunes && utf8.ValidString(s)
	if ok {
		for _, r := range s {
			if !validXMLRune(r) {
				ok = false
				break
			}
		}
	}
	if ok {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s { // 非法 UTF-8 字节会被解码成 U+FFFD，这里一并丢弃
		if r == utf8.RuneError || !validXMLRune(r) {
			continue
		}
		if n++; n > MaxCellRunes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeText 转义 XML 文本。\r 写成字符引用，避免被解析器规范化为 \n。
func escapeText(s string) string {
	if !strings.ContainsAny(s, "&<>\"'\r") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '\r':
			b.WriteString("&#13;")
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// timeToSerial 把时间转成 Excel 序列值（取墙上时钟，不做时区换算）。
// 1900-03-01 之前的日期受 Excel 1900 闰年错误影响，不支持（ok=false）。
func timeToSerial(t time.Time) (float64, bool) {
	y, m, d := t.Date()
	if y < 1900 || (y == 1900 && m < time.March) || y > 9999 {
		return 0, false
	}
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400
	sec := t.Hour()*3600 + t.Minute()*60 + t.Second()
	return float64(day+excelEpochDay) + float64(sec)/86400, true
}

// serialToTime 把序列值还原成时间（UTC 墙上时钟）。date1904 为 1904 日期系统。
func serialToTime(v float64, date1904 bool) (time.Time, bool) {
	if v < 0 || v >= 2958466 { // 9999-12-31 之后
		return time.Time{}, false
	}
	if date1904 {
		v += 1462
	}
	if v < 61 { // 含 1900-02-29 这个 Excel 虚构的日期；0 <= v < 1 多为纯时间值
		return time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC).Add(time.Duration(v*86400+0.5) * time.Second), true
	}
	days := int64(v)
	secs := int64((v-float64(days))*86400 + 0.5)
	return time.Unix((days-excelEpochDay)*86400+secs, 0).UTC(), true
}
