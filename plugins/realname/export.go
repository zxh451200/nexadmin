package realname

// 台账 xlsx 导出：与 devicehub/ticket 同款 pkg/excel 封装。台账本身不存明文
// （姓名/号码只落脱敏串），导出文件同样只有脱敏串——不给明文开第二条出口。

import (
	"context"
	"io"

	"nexadmin/pkg/excel"
)

// WriteVerificationsXLSX 把台账行写成 xlsx。
func (s *Service) WriteVerificationsXLSX(ctx context.Context, w io.Writer, rows []Verification) error {
	x := excel.NewWriter(w)
	sh, err := x.NewSheet("verifications", excel.SheetOptions{Header: []string{
		"time", "id_type", "number_masked", "name_masked", "status", "reason", "provider", "latency_ms", "operator", "remark",
	}})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := sh.WriteRow([]any{
			r.CreatedAt.Format("2006-01-02 15:04:05"),
			r.IdType, r.NumberMasked, r.NameMasked, r.Status, r.Reason, r.Provider, r.LatencyMs, r.OperatorName, r.Remark,
		}...); err != nil {
			return err
		}
	}
	return x.Close()
}
