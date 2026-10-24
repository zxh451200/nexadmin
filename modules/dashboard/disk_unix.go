//go:build unix

package dashboard

import "syscall"

// diskTotal 返回 path 所在磁盘的总字节数，失败返回 0。
func diskTotal(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return uint64(st.Blocks) * uint64(st.Bsize)
}
