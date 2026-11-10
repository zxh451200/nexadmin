package devicehub

// 采集调度：每台启用的设备一个 goroutine（配置变更时重启该设备的 worker）。
//
// 两种采集形态：
//   - 轮询（Modbus）：ticker 取设备 pollIntervalMs 与最短点级 poll_interval_ms 的较小值，
//     每轮只读已到期的启用测点；连续失败判 offline，恢复判 online。
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
		// 建连成功才算在线：先前无条件先标 online，broker 连不上的设备会先亮一下再被
		// offlineWatch（30s 巡检）翻回离线，状态在界面上闪一个假在线。
		if err := push.Subscribe(ctx, func(address string, v driver.Value) {
			c.svc.onReading(context.Background(), d, address, v, interval)
		}); err != nil {
			c.svc.logWarn("devicehub: subscribe failed", "deviceNo", d.DeviceNo, "err", err.Error())
			c.svc.setDeviceStatus(ctx, d, StatusOffline)
		} else {
			c.svc.setDeviceStatus(ctx, d, StatusOnline)
		}
		// 即使刚才订阅失败也起巡检：paho 会自动重连，重连成功后由 Ping/静默判定翻回在线。
		go c.offlineWatch(ctx, d, drv)
		<-ctx.Done()
		_ = drv.Close()
		return
	}

	// 轮询型：ticker 是这台设备唯一的调度心跳，周期取「设备默认间隔」与「最短点级间隔」的较小值；
	// 点级 pollIntervalMs 只决定这一轮读不读它（见 pollOnce），不单独起 ticker——
	// 一台设备一条连接，多 ticker 会在同一条链路上并发 IO。
	points := c.svc.pointsOf(d.ID)
	ticker := time.NewTicker(pollCycle(interval, points))
	defer ticker.Stop()
	defer func() { _ = drv.Close() }()
	// next 记录每个测点下一次到期时间，跨轮复用（deviceLoop 存活期间只有这一份，pollOnce 串行访问）。
	next := make(map[idgen.ID]time.Time, len(points))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(ctx, d, drv, points, next)
		}
	}
}

// pollCycle 计算轮询周期：设备默认间隔与所有启用测点里最小的点级间隔取小。
//
// 下限 200ms：ticker 驱动的是单条设备连接上的串行 IO（一次往返 + 落库写放大），
// 再快就变成对设备的高频轰炸；200ms 已是「单连接可承受 + 秒级监控够用」的折中，
// 同时兜住配置误填 1ms 这种把驱动打爆的值。
func pollCycle(def time.Duration, points []Point) time.Duration {
	const minCycle = 200 * time.Millisecond
	cycle := def
	for i := range points {
		if points[i].PollMs <= 0 {
			continue
		}
		if d := time.Duration(points[i].PollMs) * time.Millisecond; d < cycle {
			cycle = d
		}
	}
	if cycle < minCycle {
		cycle = minCycle
	}
	return cycle
}

// pollOnce 轮询一轮：只读已到期的启用测点，逐点落库。
//
// next 是「测点 → 下次到期时间」表，由 deviceLoop 创建并跨轮复用：ticker 按最短点级间隔跳，
// 长周期的测点在多数 tick 里还没到期，直接跳过。点级间隔 = PollMs > 0 ? PollMs : 设备默认间隔。
//
// 无论读取成功还是失败，尝试后都把 next 推到 now+该间隔：失败通常是瞬时的（单点寄存器错误、
// 网关抖动），每个 tick 都重试等于把故障期的实际采集频率抬到 tick 频率，反而压垮链路、刷满日志；
// 等一个周期再试，代价只是该点晚一个周期恢复。
//
// points 由 deviceLoop 在起 ticker 时取一次并缓存：测点/设备配置变更会 Reload 重启 worker，
// 缓存不会过期；放在这里每 tick 查库的话，200ms 周期下等于每台设备每秒查 5 次测点表。
func (c *collector) pollOnce(ctx context.Context, d *Device, drv driver.Driver, points []Point, next map[idgen.ID]time.Time) {
	if len(points) == 0 {
		return
	}
	def := devicePollInterval(d)
	fail, attempted := 0, 0
	for i := range points {
		select {
		case <-ctx.Done():
			return
		default:
		}
		p := &points[i]
		every := def
		if p.PollMs > 0 {
			every = time.Duration(p.PollMs) * time.Millisecond
		}
		now := time.Now()
		if due, ok := next[p.ID]; ok && now.Before(due) {
			continue
		}
		attempted++
		next[p.ID] = now.Add(every)
		v, err := drv.Read(ctx, p.Address, p.DataType)
		if err != nil {
			fail++
			c.svc.logWarn("devicehub: read failed", "deviceNo", d.DeviceNo, "point", p.Address, "err", err.Error())
			continue
		}
		c.svc.onReading(ctx, d, p.Address, v, def)
	}
	if attempted == 0 {
		return // 这轮没有到期的测点：无从判断链路，不动状态（也省掉每 tick 刷 last_seen）
	}
	// 一轮里全部失败大概率是断线：标记 offline；有成功就是 online。
	if fail == attempted {
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
		// 按 ts 取批（idx_plg_devhub_reading_ts）：条件与排序同列，扫描走该索引，
		// 不必回主键索引再排序；id 顺序对聚合结果无意义，桶按 Truncate(hour) 自算。
		if err := c.svc.db.WithContext(ctx).Where("ts < ?", cutoff).Order("ts").Limit(downsampleBatch).Find(&rows).Error; err != nil {
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
