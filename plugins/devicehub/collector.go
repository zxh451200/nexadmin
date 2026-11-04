package devicehub

// 采集调度：每台启用的设备一个 goroutine（配置变更时重启该设备的 worker）。
//
// 两种采集形态：
//   - 轮询（Modbus）：按设备 pollIntervalMs 周期读全部启用测点；连续失败判 offline，恢复判 online。
//   - 推送（MQTT）：driver.PushDriver.Subscribe 一条通配订阅，收到帧即落库；离线靠 Ping 的静默判定。
//
// 与 ticket 的超时清扫器同一套模式：module.Start 起 goroutine，ctx 取消即整体退出；
// 降采样 sweeper 也在这个循环里按小时触发。

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"gorm.io/gorm"

	"nexadmin/pkg/idgen"

	"nexadmin/plugins/devicehub/driver"
)

type collector struct {
	svc *Service

	mu      sync.Mutex
	cancels map[idgen.ID]context.CancelFunc // deviceId → 该设备 worker 的取消函数
}

func newCollector(svc *Service) *collector {
	return &collector{svc: svc, cancels: map[idgen.ID]context.CancelFunc{}}
}

// Start 起全部启用设备的 worker；由 plugin.Start 调用。
func (c *collector) Start(ctx context.Context) {
	var devices []Device
	if err := c.svc.db.WithContext(ctx).Where("enabled = ?", true).Find(&devices).Error; err != nil {
		c.svc.logWarn("devicehub: load devices failed", "err", err.Error())
		return
	}
	for i := range devices {
		c.startDevice(ctx, &devices[i])
	}
}

// Reload 重启一台设备的 worker（设备/测点配置变更后调用）。
func (c *collector) Reload(ctx context.Context, deviceID idgen.ID) {
	c.stopDevice(deviceID)
	var d Device
	if err := c.svc.db.WithContext(ctx).Where("id = ? AND enabled = ?", deviceID, true).Take(&d).Error; err != nil {
		return // 已停用/不存在：worker 保持停止
	}
	c.startDevice(ctx, &d)
}

func (c *collector) stopDevice(deviceID idgen.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.cancels[deviceID]; ok {
		cancel()
		delete(c.cancels, deviceID)
	}
}

func (c *collector) startDevice(parent context.Context, d *Device) {
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	c.cancels[d.ID] = cancel
	c.mu.Unlock()
	go c.deviceLoop(ctx, d)
}

// deviceLoop 是单设备的采集循环；返回即 worker 退出（ctx 取消 / 无法构造驱动）。
func (c *collector) deviceLoop(ctx context.Context, d *Device) {
	drv, err := driver.New(d.Protocol, d.Endpoint, []byte(d.Config))
	if err != nil {
		c.svc.logWarn("devicehub: build driver failed", "deviceNo", d.DeviceNo, "err", err.Error())
		c.svc.setDeviceStatus(ctx, d, StatusOffline)
		return
	}
	if err := drv.Connect(ctx); err != nil {
		c.svc.logWarn("devicehub: connect failed", "deviceNo", d.DeviceNo, "err", err.Error())
		c.svc.setDeviceStatus(ctx, d, StatusOffline)
	}
	interval := devicePollInterval(d)

	// 推送型：订阅后阻塞等 ctx，不轮询。
	if push, ok := drv.(driver.PushDriver); ok {
		c.svc.setDeviceStatus(ctx, d, StatusOnline)
		_ = push.Subscribe(ctx, func(address string, v driver.Value) {
			c.svc.onReading(context.Background(), d, address, v, interval)
		})
		go c.offlineWatch(ctx, d, drv)
		<-ctx.Done()
		_ = drv.Close()
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer func() { _ = drv.Close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(ctx, d, drv)
		}
	}
}

// pollOnce 轮询一轮：读全部启用测点，逐点落库。
func (c *collector) pollOnce(ctx context.Context, d *Device, drv driver.Driver) {
	points := c.svc.pointsOf(d.ID)
	if len(points) == 0 {
		return
	}
	interval := devicePollInterval(d)
	fail := 0
	for i := range points {
		select {
		case <-ctx.Done():
			return
		default:
		}
		p := &points[i]
		v, err := drv.Read(ctx, p.Address, p.DataType)
		if err != nil {
			fail++
			c.svc.logWarn("devicehub: read failed", "deviceNo", d.DeviceNo, "point", p.Address, "err", err.Error())
			continue
		}
		c.svc.onReading(ctx, d, p.Address, v, interval)
	}
	// 一轮里全部失败大概率是断线：标记 offline；有成功就是 online。
	if fail == len(points) {
		if err := drv.Ping(); err != nil {
			c.svc.setDeviceStatus(ctx, d, StatusOffline)
			return
		}
	}
	c.svc.setDeviceStatus(ctx, d, StatusOnline)
}

// offlineWatch 是推送型设备的离线巡检：MQTT 静默超时把设备打回 offline（恢复由帧到达体现）。
func (c *collector) offlineWatch(ctx context.Context, d *Device, drv driver.Driver) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := drv.Ping(); err != nil {
				c.svc.setDeviceStatus(ctx, d, StatusOffline)
			} else {
				c.svc.setDeviceStatus(ctx, d, StatusOnline)
			}
		}
	}
}

func devicePollInterval(d *Device) time.Duration {
	// 设备级默认间隔存在 config JSON 里；驱动层已解析过，这里从 config 再解一次（小 JSON，开销可忽略）
	var cfg struct {
		PollIntervalMs int `json:"pollIntervalMs"`
	}
	_ = jsonUnmarshal(d.Config, &cfg)
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 5000
	}
	return time.Duration(cfg.PollIntervalMs) * time.Millisecond
}

func jsonUnmarshal(raw string, v any) error {
	return json.Unmarshal([]byte(raw), v)
}

// 批量降采样：把 48h 前的原始读数按小时聚合进 _agg，然后删除原始行。
// 用 Go 层聚合而不是 SQL group by：三引擎的日期截断语法不同，Go 只写一遍。
func (c *collector) downsampleLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.downsampleOnce(ctx)
		}
	}
}

const downsampleBatch = 5000

func (c *collector) downsampleOnce(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Hour)
	for {
		var rows []Reading
		if err := c.svc.db.WithContext(ctx).Where("ts < ?", cutoff).Order("id").Limit(downsampleBatch).Find(&rows).Error; err != nil {
			c.svc.logWarn("devicehub: downsample load failed", "err", err.Error())
			return
		}
		if len(rows) == 0 {
			return
		}
		type bucketKey struct {
			pointID idgen.ID
			bucket  time.Time
		}
		type agg struct {
			min, max, sum float64
			cnt           int
		}
		acc := map[bucketKey]*agg{}
		for _, r := range rows {
			if r.NumValue == nil {
				continue
			}
			k := bucketKey{r.PointID, r.TS.Truncate(time.Hour)}
			a := acc[k]
			if a == nil {
				a = &agg{min: *r.NumValue, max: *r.NumValue, sum: *r.NumValue, cnt: 1}
				acc[k] = a
			} else {
				a.min = min64(a.min, *r.NumValue)
				a.max = max64(a.max, *r.NumValue)
				a.sum += *r.NumValue
				a.cnt++
			}
		}
		err := c.svc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for k, a := range acc {
				row := ReadingAgg{PointID: k.pointID, Bucket: k.bucket, NumMin: a.min, NumMax: a.max, NumAvg: a.sum / float64(a.cnt), Cnt: a.cnt}
				if err := tx.Where("point_id = ? AND bucket = ?", k.pointID, k.bucket).
					Assign(ReadingAgg{NumMin: a.min, NumMax: a.max, NumAvg: a.sum / float64(a.cnt), Cnt: a.cnt}).
					FirstOrCreate(&row).Error; err != nil {
					return err
				}
			}
			ids := make([]idgen.ID, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			return tx.Where("id IN ?", ids).Delete(&Reading{}).Error
		})
		if err != nil {
			c.svc.logWarn("devicehub: downsample write failed", "err", err.Error())
			return
		}
		if len(rows) < downsampleBatch {
			return
		}
	}
}

func min64(a, b float64) float64 {
	if b < a {
		return b
	}
	return a
}

func max64(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}
