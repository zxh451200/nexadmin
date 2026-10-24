package excel

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// FormulaGuard 是公式注入防护策略。文本以 = + - @ 、制表符、回车开头时，Excel/WPS 在用户编辑单元格
// 或另存为 CSV 后可能把它当公式执行（CSV 注入）。
//
//   - GuardTextStyle（默认）：单元格始终按字符串类型（t="s"）写入，不会成为 <f> 公式；同时套用带 quotePrefix 的文本样式，
//     等价于用户手输 '=1+1，编辑时也不会被当公式。值保持原样、读回无损。取舍：若下游把 xlsx 转成 CSV 再用别的软件打开，
//     样式信息丢失，防护失效。
//   - GuardPrefix：在文本前加一个字面量单引号，转成 CSV 后依然安全（OWASP 建议做法）。取舍：数据被改写，
//     Excel 里会看到这个单引号，读回得到 "'=…"，不适合需要无损往返的场景。
type FormulaGuard int

const (
	GuardTextStyle FormulaGuard = iota
	GuardPrefix
)

// Options 是 Writer 的选项，零值可用。
type Options struct {
	FormulaGuard FormulaGuard
	// MaxStringBytes 限制 sharedStrings 表累计占用的字节数（该表要到最后才能写出，必须常驻内存），默认 64MB。
	MaxStringBytes int
}

// Validation 给某一列的数据行加下拉列表校验（仅提示/限制输入，不影响读取）。
type Validation struct {
	Col      int      // 0 起的列号
	List     []string // 候选值；拼接后不能超过 255 字符
	FirstRow int      // 起止行（1 起），LastRow 为 0 时取 FirstRow+999
	LastRow  int
}

// SheetOptions 是单个 sheet 的选项。
type SheetOptions struct {
	// Header 非空时作为第 1 行写出：加粗，并冻结首行。
	Header      []string
	Validations []Validation
}

const (
	sampleRows   = 200 // 列宽按表头 + 前 N 行估算；<cols> 必须出现在 <sheetData> 之前，所以这些行先缓冲
	maxColWidth  = 60.0
	minColWidth  = 8.0
	maxIntExact  = 999999999999999 // 超过 15 位的整数按文本写，避免精度丢失（如雪花 ID）
	defaultSSMax = 64 << 20
)

// 样式索引，与 stylesXML 中 cellXfs 的顺序一致。
const (
	styleDefault = iota
	styleHeader
	styleDate
	styleDateTime
	styleQuote
)

type cellv struct {
	col   int
	kind  byte // 's' 字符串(sst 下标) 'n' 数字 'b' 布尔 'd' 日期(数字+日期样式)
	idx   int
	num   string
	style int
}

// Writer 写一个 xlsx。同一时刻只有一个 sheet 处于写入状态（zip 条目是顺序的）。
// 非并发安全。写完必须 Close，否则文件不完整。
type Writer struct {
	zw       *zip.Writer
	opt      Options
	sst      map[string]int
	sstList  []string
	sstBytes int
	names    map[string]bool
	sheets   []string
	cur      *Sheet
	err      error
	closed   bool
}

// NewWriter 创建 Writer，内容直接流式写入 w（不会 Close w）。
func NewWriter(w io.Writer, opts ...Options) *Writer {
	x := &Writer{zw: zip.NewWriter(w), sst: map[string]int{}, names: map[string]bool{}}
	if len(opts) > 0 {
		x.opt = opts[0]
	}
	if x.opt.MaxStringBytes <= 0 {
		x.opt.MaxStringBytes = defaultSSMax
	}
	return x
}

// Sheet 是一个正在写入的工作表。
type Sheet struct {
	x       *Writer
	bw      *bufio.Writer
	opts    SheetOptions
	row     int
	pending [][]cellv
	widths  []float64
	started bool
	done    bool
}

func (x *Writer) uniqueName(name string) string {
	r := strings.NewReplacer("[", "_", "]", "_", ":", "_", "*", "_", "?", "_", "/", "_", "\\", "_")
	name = strings.Trim(r.Replace(cleanText(name)), "'")
	if rs := []rune(name); len(rs) > maxSheetName {
		name = string(rs[:maxSheetName])
	}
	if strings.TrimSpace(name) == "" {
		name = "Sheet" + strconv.Itoa(len(x.sheets)+1)
	}
	base, n := name, 1
	for x.names[strings.ToLower(name)] {
		n++
		suffix := "_" + strconv.Itoa(n)
		rs := []rune(base)
		if len(rs)+len(suffix) > maxSheetName {
			rs = rs[:maxSheetName-len(suffix)]
		}
		name = string(rs) + suffix
	}
	x.names[strings.ToLower(name)] = true
	return name
}

// NewSheet 开始一个新 sheet；上一个 sheet 会自动结束。名称中的非法字符会被替换、超长截断、重名加后缀。
func (x *Writer) NewSheet(name string, opts SheetOptions) (*Sheet, error) {
	if x.err != nil {
		return nil, x.err
	}
	if x.closed {
		return nil, fmt.Errorf("excel: writer is closed")
	}
	if x.cur != nil {
		if err := x.cur.finish(); err != nil {
			return nil, err
		}
	}
	name = x.uniqueName(name)
	fw, err := x.zw.CreateHeader(&zip.FileHeader{
		Name: "xl/worksheets/sheet" + strconv.Itoa(len(x.sheets)+1) + ".xml", Method: zip.Deflate, Modified: time.Unix(0, 0).UTC(),
	})
	if err != nil {
		x.err = err
		return nil, err
	}
	x.sheets = append(x.sheets, name)
	s := &Sheet{x: x, bw: bufio.NewWriterSize(fw, 32<<10), opts: opts}
	x.cur = s
	if len(opts.Header) > 0 {
		cells := make([]cellv, 0, len(opts.Header))
		for i, h := range opts.Header {
			c, err := x.stringCell(i, h, styleHeader)
			if err != nil {
				return nil, err
			}
			cells = append(cells, c)
		}
		s.addRow(cells, h2w(opts.Header))
	}
	return s, nil
}

func h2w(hs []string) []float64 {
	w := make([]float64, len(hs))
	for i, h := range hs {
		w[i] = textWidth(h) + 1 // 加粗略宽
	}
	return w
}

func (x *Writer) intern(s string) (int, error) {
	if i, ok := x.sst[s]; ok {
		return i, nil
	}
	if x.sstBytes += len(s) + 16; x.sstBytes > x.opt.MaxStringBytes {
		x.err = fmt.Errorf("%w: shared strings exceed %d bytes", ErrTooLarge, x.opt.MaxStringBytes)
		return 0, x.err
	}
	x.sst[s] = len(x.sstList)
	x.sstList = append(x.sstList, s)
	return len(x.sstList) - 1, nil
}

func dangerousPrefix(s string) bool {
	return s != "" && strings.IndexByte("=+-@\t\r", s[0]) >= 0
}

func (x *Writer) stringCell(col int, s string, style int) (cellv, error) {
	s = cleanText(s)
	if dangerousPrefix(s) {
		if x.opt.FormulaGuard == GuardPrefix {
			s = "'" + s
		} else if style == styleDefault {
			style = styleQuote
		}
	}
	i, err := x.intern(s)
	return cellv{col: col, kind: 's', idx: i, style: style}, err
}

// convert 把 Go 值转成单元格；ok=false 表示空单元格。
func (x *Writer) convert(col int, v any) (c cellv, ok bool, err error) {
	num := func(s string) (cellv, bool, error) { return cellv{col: col, kind: 'n', num: s}, true, nil }
	switch t := v.(type) {
	case nil:
		return c, false, nil
	case string:
		c, err = x.stringCell(col, t, styleDefault)
		return c, true, err
	case []byte:
		c, err = x.stringCell(col, string(t), styleDefault)
		return c, true, err
	case bool:
		n := "0"
		if t {
			n = "1"
		}
		return cellv{col: col, kind: 'b', num: n}, true, nil
	case int, int8, int16, int32, int64:
		n := reflectInt(t)
		if n > maxIntExact || n < -maxIntExact {
			c, err = x.stringCell(col, strconv.FormatInt(n, 10), styleDefault)
			return c, true, err
		}
		return num(strconv.FormatInt(n, 10))
	case uint, uint8, uint16, uint32, uint64:
		n := reflectUint(t)
		if n > maxIntExact {
			c, err = x.stringCell(col, strconv.FormatUint(n, 10), styleDefault)
			return c, true, err
		}
		return num(strconv.FormatUint(n, 10))
	case float32:
		return x.convert(col, float64(t))
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return c, false, nil
		}
		return num(strconv.FormatFloat(t, 'g', -1, 64))
	case time.Time:
		return x.timeCell(col, t)
	case *time.Time:
		if t == nil {
			return c, false, nil
		}
		return x.timeCell(col, *t)
	default:
		c, err = x.stringCell(col, fmt.Sprint(v), styleDefault)
		return c, true, err
	}
}

func (x *Writer) timeCell(col int, t time.Time) (cellv, bool, error) {
	if t.IsZero() {
		return cellv{}, false, nil
	}
	serial, ok := timeToSerial(t)
	if !ok { // 1900-03-01 之前：按 ISO 文本写
		c, err := x.stringCell(col, t.Format("2006-01-02 15:04:05"), styleDefault)
		return c, true, err
	}
	style := styleDateTime
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		style = styleDate
	}
	return cellv{col: col, kind: 'd', num: strconv.FormatFloat(serial, 'f', -1, 64), style: style}, true, nil
}

// textWidth 估算显示宽度：全角/CJK/emoji 记 2，其余 1；多行取最长行。
func textWidth(s string) float64 {
	best, cur := 0, 0
	for _, r := range s {
		switch {
		case r == '\n':
			best, cur = max(best, cur), 0
		case r >= 0x1100 && (r <= 0x115F || (r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE30 && r <= 0xFE6F) || (r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6) || (r >= 0x1F300 && r <= 0x1FAFF)):
			cur += 2
		default:
			cur++
		}
	}
	return float64(max(best, cur))
}

func (x *Writer) cellWidth(c cellv) float64 {
	switch c.kind {
	case 's':
		return textWidth(x.sstList[c.idx])
	case 'd':
		if c.style == styleDate {
			return 10
		}
		return 19
	case 'b':
		return 5
	default:
		return float64(utf8.RuneCountInString(c.num))
	}
}

// 下半部分（行写出、结束、静态部件）见 writer_sheet.go。
