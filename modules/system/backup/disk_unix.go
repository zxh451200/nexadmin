//go:build !windows

package backup

import "syscall"

// diskFree 返回目录所在分区的可用字节数；读不到返回 false。
func diskFree(dir string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
