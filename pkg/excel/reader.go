package excel

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// ReadOptions 是读取限制，零值字段取默认值。这些限制用来防 zip 炸弹和资源耗尽：
// 声明大小与实际解压大小都会被检查（zip 头里的大小可以造假，所以实际读取时也限额）。
type ReadOptions struct {
	Sheet      string // 按名称选 sheet；为空则按 SheetIndex
	SheetIndex int    // 0 起，默认第一个

	MaxArchiveBytes int64 // 压缩包本身大小，默认 64MB
	MaxFiles        int   // 压缩包内条目数，默认 200
	MaxPartBytes    int64 // 单个 XML 部件解压后大小，默认 32MB
	MaxTotalBytes   int64 // 所有部件解压后累计大小，默认 64MB
	MaxRows         int   // 有内容的行的最大行号 / 行数，默认 100000
	MaxCols         int   // 有内容的单元格的最大列数，默认 256
	MaxDepth        int   // XML 嵌套深度，默认 32
	MaxStrings      int   // sharedStrings 条目数，默认 500000
}

func (o ReadOptions) withDefaults() ReadOptions {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def64 := func(v *int64, d int64) {
		if *v <= 0 {
			*v = d
		}
	}
	def64(&o.MaxArchiveBytes, 64<<20)
	def(&o.MaxFiles, 200)
	def64(&o.MaxPartBytes, 32<<20)
	def64(&o.MaxTotalBytes, 64<<20)
	def(&o.MaxRows, 100000)
	def(&o.MaxCols, 256)
	def(&o.MaxDepth, 32)
	def(&o.MaxStrings, 500000)
	return o
}

// Reader 读取一个 xlsx。非并发安全。
type Reader struct {
	opt    ReadOptions
	files  map[string]*zip.File
	used   int64 // 已解压字节（累计预算）
	sheets []sheetRef
	date   bool // date1904
	rels   map[string]string
}

type sheetRef struct{ name, part string }

// Open 校验压缩包并读取工作簿结构（sheet 列表）。r 通常是 multipart.File 或 bytes.Reader。
func Open(r io.ReaderAt, size int64, opts ReadOptions) (rd *Reader, err error) {
	defer recoverTo(&err)
	opt := opts.withDefaults()
	if size <= 0 || size > opt.MaxArchiveBytes {
		if size > opt.MaxArchiveBytes {
			return nil, fmt.Errorf("%w: archive larger than %d bytes", ErrTooLarge, opt.MaxArchiveBytes)
		}
		return nil, ErrNotXLSX
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotXLSX, err)
	}
	if len(zr.File) > opt.MaxFiles {
		return nil, fmt.Errorf("%w: more than %d files in archive", ErrTooLarge, opt.MaxFiles)
	}
	rd = &Reader{opt: opt, files: make(map[string]*zip.File, len(zr.File))}
	var declared uint64
	for _, f := range zr.File {
		key := strings.ToLower(path.Clean(strings.ReplaceAll(f.Name, "\\", "/")))
		key = strings.TrimPrefix(key, "/")
		if _, dup := rd.files[key]; dup {
			return nil, fmt.Errorf("%w: duplicate entry %q", ErrMalformed, f.Name)
		}
		rd.files[key] = f
		if f.FileInfo().IsDir() {
			continue
		}
		if declared += f.UncompressedSize64; declared < f.UncompressedSize64 || declared > uint64(opt.MaxTotalBytes)*4 {
			return nil, fmt.Errorf("%w: declared uncompressed size too large", ErrTooLarge)
		}
	}
	if err := rd.loadWorkbook(); err != nil {
		return nil, err
	}
	return rd, nil
}

func recoverTo(err *error) {
	if p := recover(); p != nil {
		*err = fmt.Errorf("%w: %v", ErrMalformed, p)
	}
}

// SheetNames 返回按工作簿顺序排列的 sheet 名称。
func (rd *Reader) SheetNames() []string {
	out := make([]string, len(rd.sheets))
	for i, s := range rd.sheets {
		out[i] = s.name
	}
	return out
}

// budgetReader 在累计解压量或单部件上限被突破时返回 ErrTooLarge。
type budgetReader struct {
	rc   io.ReadCloser
	rd   *Reader
	part int64
	name string
}

func (b *budgetReader) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	b.part += int64(n)
	b.rd.used += int64(n)
	if b.part > b.rd.opt.MaxPartBytes || b.rd.used > b.rd.opt.MaxTotalBytes {
		return 0, fmt.Errorf("%w: %s expands beyond limits", ErrTooLarge, b.name)
	}
	return n, err
}

func (b *budgetReader) Close() error { return b.rc.Close() }

// openPart 打开压缩包内的部件；不存在返回 (nil, nil)。
func (rd *Reader) openPart(name string) (io.ReadCloser, error) {
	f := rd.files[strings.ToLower(strings.TrimPrefix(path.Clean(name), "/"))]
	if f == nil {
		return nil, nil
	}
	if f.UncompressedSize64 > uint64(rd.opt.MaxPartBytes) {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrTooLarge, f.Name, rd.opt.MaxPartBytes)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return &budgetReader{rc: rc, rd: rd, name: f.Name}, nil
}

// mapErr 把 encoding/xml、zip 的底层错误归类。
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrTooLarge), errors.Is(err, ErrMalformed), errors.Is(err, ErrNotXLSX):
		return err
	}
	return fmt.Errorf("%w: %v", ErrMalformed, err)
}

// ReadAll 读取选定 sheet 的全部内容为字符串矩阵：下标 i 对应第 i+1 行，无内容的行为 nil，行内缺失的列补空串。
func ReadAll(r io.ReaderAt, size int64, opts ReadOptions) ([][]string, error) {
	rd, err := Open(r, size, opts)
	if err != nil {
		return nil, err
	}
	var out [][]string
	err = rd.ReadRows(func(row int, cells []Cell) error {
		for len(out) < row {
			out = append(out, nil)
		}
		line := make([]string, len(cells))
		for i, c := range cells {
			line[i] = c.String()
		}
		out[row-1] = line
		return nil
	})
	return out, err
}

// Kind 是单元格值类型。
type Kind uint8

const (
	KindEmpty Kind = iota
	KindString
	KindNumber
	KindBool
	KindDate
)

// Cell 是读出的单元格。Number 同时保留原始文本（避免 1.38e10 这类格式化），Date 为 UTC 墙上时钟。
type Cell struct {
	Kind Kind
	Str  string // String / Number（原始文本）
	Num  float64
	Bool bool
	Time time.Time
}

// String 返回单元格的文本形式：布尔为 TRUE/FALSE；日期为 2006-01-02，带时间时为 2006-01-02 15:04:05，纯时间为 15:04:05。
func (c Cell) String() string {
	switch c.Kind {
	case KindString, KindNumber:
		return c.Str
	case KindBool:
		if c.Bool {
			return "TRUE"
		}
		return "FALSE"
	case KindDate:
		switch {
		case c.Num < 1: // 纯时间值（序列值只有小数部分）
			return c.Time.Format("15:04:05")
		case c.Time.Hour() == 0 && c.Time.Minute() == 0 && c.Time.Second() == 0:
			return c.Time.Format("2006-01-02")
		}
		return c.Time.Format("2006-01-02 15:04:05")
	}
	return ""
}
