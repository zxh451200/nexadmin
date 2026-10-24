//go:build !unix

package dashboard

// diskTotal 在非 unix 系统上取不到磁盘总量，返回 0（前端显示 0 / 0）。
func diskTotal(string) uint64 { return 0 }
