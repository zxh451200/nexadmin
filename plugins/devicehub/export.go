package devicehub

// 历史数据 xlsx 导出：与 ticket/export 同款 pkg/excel 封装。
// 导出的是「缩放后工程量」的 min/max/avg 三列——与趋势图同源同口径。

import (
	"context"
	"io"

	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// WriteHistoryXLSX 把历史行写成 xlsx（表头中英跟随请求语言，P0 固定双语并列）。
func (s *Service) WriteHistoryXLSX(ctx context.Context, w io.Writer, device *Device, point *Point, rows []HistoryRow) error {
	x := excel.NewWriter(w)
	sh, err := x.NewSheet("history", excel.SheetOptions{Header: []string{
		"device_no", "point", "unit", "ts", "min", "max", "avg",
	}})
	if err != nil {
		return err
	}
	for _, r := range rows {
		vals := []any{
			device.DeviceNo, point.Name, point.Unit,
			r.TS.Format("2006-01-02 15:04:05"),
			round3(r.Min), round3(r.Max), round3(r.Avg),
		}
		if err := sh.WriteRow(vals...); err != nil {
			return err
		}
	}
	return x.Close()
}

func round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

// pointAndDeviceByNo 供导出取设备与测点（一次查询两样）。
func (s *Service) pointAndDeviceByNo(ctx context.Context, deviceNo string, pointID idgen.ID) (*Device, *Point, error) {
	var d Device
	if err := s.db.WithContext(ctx).Where("device_no = ?", deviceNo).Take(&d).Error; err != nil {
		return nil, nil, err
	}
	var p Point
	if err := s.db.WithContext(ctx).Where("id = ? AND device_id = ?", int64(pointID), int64(d.ID)).Take(&p).Error; err != nil {
		return nil, nil, err
	}
	return &d, &p, nil
}
