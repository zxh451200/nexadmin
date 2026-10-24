package tenant

import (
	"sync"
	"time"

	"nexadmin/pkg/idgen"
)

// Meter 是按租户的用量缓存（如已用存储）：汇总查询结果缓存一小段时间，写入/删除时增量修正，
// 这样上传热路径不必每次 SUM 整张表。多实例部署时各实例最多偏差一个 TTL。
type Meter struct {
	ttl time.Duration
	now func() time.Time
	mu  sync.Mutex
	m   map[idgen.ID]meterEntry
}

type meterEntry struct {
	v   int64
	exp time.Time
}

// NewMeter 创建缓存 ttl 的用量表。
func NewMeter(ttl time.Duration) *Meter {
	return &Meter{ttl: ttl, now: time.Now, m: map[idgen.ID]meterEntry{}}
}

// StorageMeter 缓存各租户已用存储（字节），文件模块与租户用量接口共用同一份。
var StorageMeter = NewMeter(30 * time.Second)

// Get 返回缓存值；过期或不存在时调用 load 汇总并缓存。load 出错不缓存。
func (m *Meter) Get(id idgen.ID, load func() (int64, error)) (int64, error) {
	t := m.now()
	m.mu.Lock()
	if e, ok := m.m[id]; ok && t.Before(e.exp) {
		m.mu.Unlock()
		return e.v, nil
	}
	m.mu.Unlock()
	v, err := load()
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.m[id] = meterEntry{v: v, exp: t.Add(m.ttl)}
	m.mu.Unlock()
	return v, nil
}

// Add 对已缓存的值做增量修正（未缓存则忽略，下次 Get 会重新汇总）；结果不会小于 0。
func (m *Meter) Add(id idgen.ID, delta int64) {
	m.mu.Lock()
	if e, ok := m.m[id]; ok {
		if e.v += delta; e.v < 0 {
			e.v = 0
		}
		m.m[id] = e
	}
	m.mu.Unlock()
}

// Invalidate 丢弃某租户的缓存值。
func (m *Meter) Invalidate(id idgen.ID) {
	m.mu.Lock()
	delete(m.m, id)
	m.mu.Unlock()
}
