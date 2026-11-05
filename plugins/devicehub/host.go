package devicehub

// 宿主视角的查询辅助：按 deviceNo / address 定位（宿主没有雪花 ID，只有设备号与测点地址）。

import (
	"context"

	"nexadmin/core/response"
)

// pointByAddress 按设备号+测点地址定位测点。
func (s *Service) pointByAddress(ctx context.Context, deviceNo, address string) (*Device, *Point, error) {
	var d Device
	if err := s.db.WithContext(ctx).Where("device_no = ?", deviceNo).Take(&d).Error; err != nil {
		return nil, nil, response.NotFound("devicehub.device_not_found")
	}
	var p Point
	if err := s.db.WithContext(ctx).Where("device_id = ? AND address = ?", int64(d.ID), address).Take(&p).Error; err != nil {
		return nil, nil, response.NotFound("devicehub.point_not_found")
	}
	return &d, &p, nil
}

// HostCommandByAddress 宿主/网关侧按「设备号+测点地址」下发指令（不经 UI 的 pointID 选择）。
// 权限语义与 HTTP 侧一致：调用方是宿主服务端代码，操作日志由调用方负责；指令仍进 plg_devhub_command 台账。
func (s *Service) HostCommandByAddress(ctx context.Context, deviceNo, address, rawValue, operator string) (*Command, error) {
	d, p, err := s.pointByAddress(ctx, deviceNo, address)
	if err != nil {
		return nil, err
	}
	if p.RW != RWBoth {
		return nil, response.BadRequest("devicehub.point_readonly")
	}
	return s.CreateCommand(ctx, CommandInput{
		DeviceNo: d.DeviceNo, PointID: p.ID, RawValue: rawValue,
		OperatorName: operator,
	})
}
