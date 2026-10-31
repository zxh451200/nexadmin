// Package idgen 是雪花 ID 生成器：41 位毫秒时间戳 + 10 位节点号 + 12 位序列号。
// 无路由、无表、不依赖 GORM（见设计文档 §4 / §7）。
package idgen

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	nodeBits = 10
	seqBits  = 12
	// MaxNode 是节点号上限（0-1023）。
	MaxNode = 1<<nodeBits - 1
	maxSeq  = 1<<seqBits - 1

	// epoch 为 2024-01-01 00:00:00 UTC 的毫秒时间戳。
	epoch int64 = 1704067200000
	// DefaultMaxRollback 是时钟回拨的容忍窗口，窗口内等待，超出则报错。
	DefaultMaxRollback = 5 * time.Millisecond
)

// ErrNotInitialized 表示默认生成器尚未 Init。
var ErrNotInitialized = errors.New("idgen: not initialized, call idgen.Init(nodeID) first")

// Generator 是并发安全的雪花 ID 生成器。
type Generator struct {
	mu          sync.Mutex
	node        int64
	last        int64
	seq         int64
	maxRollback int64 // 毫秒
	now         func() int64
}

// New 创建生成器，node 必须在 0-1023。
func New(node int64) (*Generator, error) {
	if node < 0 || node > MaxNode {
		return nil, fmt.Errorf("idgen: node id %d out of range 0-%d", node, MaxNode)
	}
	return &Generator{
		node:        node,
		maxRollback: DefaultMaxRollback.Milliseconds(),
		now:         func() int64 { return time.Now().UnixMilli() },
	}, nil
}

// Next 生成下一个 ID。时钟回拨超出容忍窗口时返回错误，不会产生重复 ID。
func (g *Generator) Next() (ID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	ts := g.now()
	if ts < g.last {
		back := g.last - ts
		if back > g.maxRollback {
			return 0, fmt.Errorf("idgen: clock moved backwards by %dms, refusing to generate", back)
		}
		time.Sleep(time.Duration(back) * time.Millisecond)
		if ts = g.now(); ts < g.last {
			return 0, errors.New("idgen: clock still behind after waiting, refusing to generate")
		}
	}
	if ts == g.last {
		g.seq = (g.seq + 1) & maxSeq
		if g.seq == 0 { // 本毫秒序列用尽，等下一毫秒
			deadline := time.Now().Add(50 * time.Millisecond)
			for ts <= g.last {
				if time.Now().After(deadline) {
					return 0, errors.New("idgen: timed out waiting for next millisecond")
				}
				time.Sleep(50 * time.Microsecond)
				ts = g.now()
			}
		}
	} else {
		g.seq = 0
	}
	if ts < epoch {
		return 0, errors.New("idgen: system clock is before epoch 2024-01-01")
	}
	g.last = ts
	return ID((ts-epoch)<<(nodeBits+seqBits) | g.node<<seqBits | g.seq), nil
}

var (
	defMu  sync.RWMutex
	defGen *Generator
)

// Init 设置默认生成器（可重复调用，后者覆盖前者）。
func Init(node int64) error {
	g, err := New(node)
	if err != nil {
		return err
	}
	defMu.Lock()
	defGen = g
	defMu.Unlock()
	return nil
}

// Node 返回当前进程实际使用的节点号（自动选号后也返回真实值）；未初始化时返回 0。
func Node() int64 {
	defMu.RLock()
	defer defMu.RUnlock()
	if defGen == nil {
		return 0
	}
	return defGen.node
}

// Next 用默认生成器生成 ID。
func Next() (ID, error) {
	defMu.RLock()
	g := defGen
	defMu.RUnlock()
	if g == nil {
		return 0, ErrNotInitialized
	}
	return g.Next()
}

// ID 是雪花 ID。数据库里是 BIGINT，JSON 里一律是字符串（避免前端 2^53 精度丢失）。
type ID int64

// String 返回十进制字符串。
func (i ID) String() string { return strconv.FormatInt(int64(i), 10) }

// MarshalJSON 把 ID 序列化为字符串。
func (i ID) MarshalJSON() ([]byte, error) { return []byte(`"` + i.String() + `"`), nil }

// UnmarshalJSON 同时接受 "123" 和 123；null 和空串视为 0。
func (i *ID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("idgen: invalid id %q", s)
	}
	*i = ID(v)
	return nil
}

// Value 实现 driver.Valuer，写库时按 BIGINT 处理。
func (i ID) Value() (driver.Value, error) { return int64(i), nil }

// Scan 实现 sql.Scanner。
func (i *ID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*i = 0
	case int64:
		*i = ID(v)
	case []byte:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return err
		}
		*i = ID(n)
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return err
		}
		*i = ID(n)
	default:
		return fmt.Errorf("idgen: cannot scan %T into ID", src)
	}
	return nil
}

// Parse 把字符串（如路由参数 :id）解析成 ID。
func Parse(s string) (ID, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("idgen: invalid id %q", s)
	}
	return ID(v), nil
}
