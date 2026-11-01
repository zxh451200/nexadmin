package ticket

// 附件杀毒是内容嗅探之后的第二道闸，做成可插拔而不是硬编码：
// 引擎（ClamAV、厂商 API）在企业内是外部依赖，本仓库不带、也不该带。
// 没注册扫描器时这道闸整体跳过（不读文件、不占内存），启动时把「未接入」写进日志，
// 免得运维以为有防护。

import (
	"context"
	"sync"
)

// Scanner 是外部杀毒引擎的接入面。Scan 返回错误即拒绝该附件。
// 拿到的是完整字节流，所以实现方可自由分片、计算哈希或转投异步队列。
type Scanner interface {
	// Name 出现在日志与拒绝原因里，便于排查是哪套引擎拦的。
	Name() string
	Scan(ctx context.Context, filename, mime string, data []byte) error
}

var (
	scannerMu sync.RWMutex
	scanner   Scanner
)

// RegisterScanner 注册全局附件扫描器；传 nil 表示撤下（回落到只做内容嗅探）。
func RegisterScanner(s Scanner) {
	scannerMu.Lock()
	scanner = s
	scannerMu.Unlock()
}

// ScannerName 返回当前扫描器名字，未接入时是 "none"（启动日志与运维接口用它自证）。
func ScannerName() string {
	s := currentScanner()
	if s == nil {
		return "none"
	}
	return s.Name()
}

func currentScanner() Scanner {
	scannerMu.RLock()
	defer scannerMu.RUnlock()
	return scanner
}

// scanAttachment 是要不要扫、拿什么去扫的唯一决策点。
func scanAttachment(ctx context.Context, filename, mime string, data []byte) error {
	s := currentScanner()
	if s == nil {
		return nil
	}
	return s.Scan(ctx, filename, mime, data)
}
