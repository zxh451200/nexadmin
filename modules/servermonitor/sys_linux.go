//go:build linux

package servermonitor

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func readRaw() rawSample {
	s := rawSample{at: time.Now()}
	// /proc/stat 第一行：cpu user nice system idle iowait irq softirq steal ...
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		if line, _, _ := strings.Cut(string(b), "\n"); strings.HasPrefix(line, "cpu ") {
			f := strings.Fields(line)[1:]
			var total, idle uint64
			for i, v := range f {
				n, _ := strconv.ParseUint(v, 10, 64)
				if i >= 8 { // guest 已计入 user，不重复统计
					break
				}
				total += n
				if i == 3 || i == 4 { // idle + iowait
					idle += n
				}
			}
			s.cpuTotal, s.cpuBusy, s.hasCPU = total, total-idle, true
		}
	}
	// /proc/net/dev：累计收发字节，跳过回环
	if fh, err := os.Open("/proc/net/dev"); err == nil {
		defer fh.Close()
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			name, rest, ok := strings.Cut(sc.Text(), ":")
			if !ok || strings.TrimSpace(name) == "lo" {
				continue
			}
			f := strings.Fields(rest)
			if len(f) < 9 {
				continue
			}
			in, _ := strconv.ParseUint(f[0], 10, 64)
			out, _ := strconv.ParseUint(f[8], 10, 64)
			s.netIn += in
			s.netOut += out
			s.hasNet = true
		}
	}
	return s
}

func readLoad() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	if len(f) >= 3 {
		l1, _ = strconv.ParseFloat(f[0], 64)
		l5, _ = strconv.ParseFloat(f[1], 64)
		l15, _ = strconv.ParseFloat(f[2], 64)
	}
	return
}

// readMemory 返回总量与已用（总量 - MemAvailable），单位字节。
func readMemory() (total, used uint64) {
	fh, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer fh.Close()
	var avail uint64
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		key, rest, _ := strings.Cut(sc.Text(), ":")
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		kb, _ := strconv.ParseUint(f[0], 10, 64)
		switch key {
		case "MemTotal":
			total = kb * 1024
		case "MemAvailable":
			avail = kb * 1024
		}
	}
	if avail > total {
		avail = total
	}
	return total, total - avail
}

// 只统计真实文件系统，去掉 proc/sysfs/tmpfs/容器 overlay 等虚拟挂载。
var realFS = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "vfat": true, "ntfs": true, "f2fs": true, "jfs": true, "reiserfs": true}

func readDisks() []Disk {
	fh, err := os.Open("/proc/mounts")
	if err != nil {
		return []Disk{}
	}
	defer fh.Close()
	seen := map[string]bool{}
	disks := []Disk{}
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		seen[f[0]] = true // 同一设备多处挂载（bind mount）只算一次
		var st syscall.Statfs_t
		if err := syscall.Statfs(f[1], &st); err != nil || st.Blocks == 0 {
			continue
		}
		bs := uint64(st.Bsize)
		totalB := st.Blocks * bs
		usedB := (st.Blocks - st.Bfree) * bs
		disks = append(disks, Disk{Mount: f[1], TotalGB: round2(float64(totalB) / gb), UsedGB: round2(float64(usedB) / gb)})
	}
	return disks
}

// osName 优先用 /etc/os-release 的 PRETTY_NAME，例如 "Kylin Linux Advanced Server V10"。
func osName() string {
	b, err := os.ReadFile("/etc/os-release")
	if err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				return strings.Trim(v, `"`)
			}
		}
	}
	return runtime.GOOS
}
