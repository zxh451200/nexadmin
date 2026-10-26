//go:build windows

package backup

// diskFree 在 Windows 上不提供（避免引入额外依赖），页面显示“未知”。
func diskFree(string) (int64, bool) { return 0, false }
