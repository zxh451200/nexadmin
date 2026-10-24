// Package ctl 实现 nexadmin-server 的内置子命令：start / stop / restart / status / reinit（设计文档 §17）。
// 逻辑只写在这里（Go，跨平台）；deploy/ 下的脚本只是调用它的薄壳。
// 本包不依赖 core，core 反过来用这里的 ShutdownToken / TokenOK / IsLoopback 提供关停接口。
package ctl

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"time"
)

// TokenEnv 是 start 把关停令牌交给后台进程用的环境变量（不放命令行参数，避免 ps 看到）。
const TokenEnv = "NEXADMIN_SHUTDOWN_TOKEN"

// ShutdownPath 是仅回环可访问、带令牌保护的关停接口；令牌放在请求头 ShutdownHeader 里。
const (
	ShutdownPath   = "/api/v1/system/shutdown"
	ShutdownHeader = "X-Shutdown-Token"
)

// PidFile 是 data/run/nexadmin.pid 的内容（JSON，权限 0600）；Token 就是关停令牌。
type PidFile struct {
	PID     int       `json:"pid"`
	Port    int       `json:"port"`
	Token   string    `json:"token"`
	Started time.Time `json:"started"`
}

// PidPath 返回 pid 文件路径。
func PidPath(dataDir string) string { return filepath.Join(dataDir, "run", "nexadmin.pid") }

// WritePid 写 pid 文件，权限 0600。
func WritePid(path string, p PidFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // 文件已存在时 WriteFile 不会改权限
}

// ReadPid 读 pid 文件；文件不存在时返回的错误满足 os.IsNotExist。
func ReadPid(path string) (PidFile, error) {
	var p PidFile
	b, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return PidFile{}, err
	}
	return p, nil
}

// NewToken 生成 128 位随机令牌。
func NewToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// TokenOK 用常量时间比较令牌；want 为空（没有配置令牌）一律不通过。
func TokenOK(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// IsLoopback 判断 http.Request.RemoteAddr 是否来自本机回环地址（直接看对端地址，不信任转发头）。
func IsLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
