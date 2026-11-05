package imports

// retry_csv.go 生成「待修复 CSV」：失败行按模板口径原样重导出（表头 = 模板表头，
// 用台账记录的提交语言生成），修正单元格后可直接再次上传导入。
// 与 errors.csv（原因明细）互补：一个告诉用户错在哪，一个让用户改完接着传。

import (
	"bytes"
	"context"
	"encoding/csv"
)

// BuildRetryCSV 生成可直接修复再导入的 CSV：表头与模板同口径（Label，按 ctx 语言），
// 数据行 = 失败行的原始单元格内容。没有原始数据的行（超出 MaxErrors 的）跳过——
// 这部分用户需要回到原始文件自行截取，errors.csv 里的行号就是定位依据。
func BuildRetryCSV(ctx context.Context, s Source, errs []RowError) []byte {
	var buf bytes.Buffer
	buf.WriteString(utf8BOM)
	w := csv.NewWriter(&buf)

	header, _ := templateCells(ctx, s)
	_ = w.Write(header)

	loc := localize(ctx, s)
	for _, e := range errs {
		if len(e.Data) == 0 {
			continue
		}
		row := make([]string, len(loc.Columns))
		for i, c := range loc.Columns {
			row[i] = e.Data[c.Key]
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.Bytes()
}
