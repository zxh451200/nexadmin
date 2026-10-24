//go:build !linux

package servermonitor

import (
	"runtime"
	"time"
)

// 非 Linux 系统：只提供 Go 运行时和基础信息，系统级指标返回 0 / 空。

func readRaw() rawSample               { return rawSample{at: time.Now()} }
func readLoad() (l1, l5, l15 float64)  { return }
func readMemory() (total, used uint64) { return }
func readDisks() []Disk                { return []Disk{} }
func osName() string                   { return runtime.GOOS }
