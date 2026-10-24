package ctl

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/config"
	"nexadmin/core/database"
)

// 本文件是“调用本机 mysqldump / pg_dump / mysql / psql”的唯一实现：reinit 的备份、
// 备份模块（modules/system/backup）、restore 子命令共用，命令行参数在这里构造并做注入校验。
// 密码只通过环境变量（MYSQL_PWD / PGPASSWORD）传给子进程，不进命令行、不进日志、不进错误文本。

// LookPath 用来探测工具是否存在；生产用 exec.LookPath，单测传假实现。
type LookPath func(file string) (string, error)

// ToolMissingError 表示本机找不到所需的命令行工具。
type ToolMissingError struct{ Names []string }

func (e *ToolMissingError) Error() string {
	return "本机未找到 " + strings.Join(e.Names, " / ") + "，请安装对应数据库客户端工具并加入 PATH"
}

// ToolSpec 是一条待执行的外部命令。Env 里有密码，所以不要打印整个结构体；用 String()。
type ToolSpec struct {
	Name   string
	Args   []string
	Env    []string // 额外环境变量（含密码）
	Secret string   // 用来从错误输出里抹掉密码
}

// String 只含命令和参数（不含环境变量），可以安全写日志。
func (t ToolSpec) String() string { return strings.Join(append([]string{t.Name}, t.Args...), " ") }

func (t ToolSpec) redact(s string) string {
	if t.Secret != "" {
		s = strings.ReplaceAll(s, t.Secret, "***")
	}
	return strings.TrimSpace(s)
}

// connIdent 允许主机名、IPv4/IPv6、账号、库名常见字符；不得以 "-" 开头，避免被当作命令行选项（参数注入）。
var connIdent = regexp.MustCompile(`^[A-Za-z0-9_:][A-Za-z0-9_.:@%+\-]{0,127}$`)

// ValidateConn 检查连接参数能否安全地放进命令行。
func ValidateConn(c config.Database) error {
	for _, f := range []struct{ name, v string }{{"host", c.Host}, {"user", c.User}, {"name", c.Name}} {
		if !connIdent.MatchString(f.v) {
			return fmt.Errorf("database.%s 含有不允许出现在命令行里的字符，已拒绝调用外部工具", f.name)
		}
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("database.port %d 超出范围", c.Port)
	}
	return nil
}

// Engine 返回写进文件名的引擎名：mysql | mariadb | postgres。
func Engine(c config.Database) string {
	if c.Type == "mariadb" {
		return "mariadb"
	}
	if k, _ := database.Normalize(c.Type); k == "postgres" {
		return "postgres"
	}
	return "mysql"
}

func isPostgres(c config.Database) bool { return Engine(c) == "postgres" }

func pickTool(look LookPath, names []string) (string, error) {
	if look == nil {
		look = exec.LookPath
	}
	for _, n := range names {
		if _, err := look(n); err == nil {
			return n, nil
		}
	}
	return "", &ToolMissingError{Names: names}
}

// DumpTool 构造导出命令（mysqldump / mariadb-dump / pg_dump）；找不到工具返回 *ToolMissingError。
func DumpTool(c config.Database, look LookPath) (ToolSpec, error) {
	if err := ValidateConn(c); err != nil {
		return ToolSpec{}, err
	}
	port := strconv.Itoa(c.Port)
	if isPostgres(c) {
		name, err := pickTool(look, []string{"pg_dump"})
		if err != nil {
			return ToolSpec{}, err
		}
		return ToolSpec{Name: name, Secret: c.Password, Env: []string{"PGPASSWORD=" + c.Password},
			Args: []string{"-h", c.Host, "-p", port, "-U", c.User, "-w", "--clean", "--if-exists", "--no-owner", c.Name}}, nil
	}
	name, err := pickTool(look, []string{"mysqldump", "mariadb-dump"})
	if err != nil {
		return ToolSpec{}, err
	}
	return ToolSpec{Name: name, Secret: c.Password, Env: []string{"MYSQL_PWD=" + c.Password},
		Args: []string{"-h", c.Host, "-P", port, "-u", c.User, "--single-transaction", "--routines", "--default-character-set=utf8mb4", c.Name}}, nil
}

// RestoreTool 构造导入命令（mysql / mariadb / psql）；SQL 从标准输入读。
func RestoreTool(c config.Database, look LookPath) (ToolSpec, error) {
	if err := ValidateConn(c); err != nil {
		return ToolSpec{}, err
	}
	port := strconv.Itoa(c.Port)
	if isPostgres(c) {
		name, err := pickTool(look, []string{"psql"})
		if err != nil {
			return ToolSpec{}, err
		}
		return ToolSpec{Name: name, Secret: c.Password, Env: []string{"PGPASSWORD=" + c.Password},
			Args: []string{"-h", c.Host, "-p", port, "-U", c.User, "-w", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-d", c.Name}}, nil
	}
	name, err := pickTool(look, []string{"mysql", "mariadb"})
	if err != nil {
		return ToolSpec{}, err
	}
	return ToolSpec{Name: name, Secret: c.Password, Env: []string{"MYSQL_PWD=" + c.Password},
		Args: []string{"-h", c.Host, "-P", port, "-u", c.User, "--default-character-set=utf8mb4", c.Name}}, nil
}

// ---- 文件名 ----

var (
	backupNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,150}\.sql\.gz$`)
	nonVersion   = regexp.MustCompile(`[^A-Za-z0-9.]+`)
)

// SafeBackupName 判断是不是合法的备份文件名：只有字母数字和 ._-，以 .sql.gz 结尾，没有路径分隔符和 ".."。
func SafeBackupName(name string) bool {
	return backupNameRe.MatchString(name) && !strings.Contains(name, "..") && filepath.Base(name) == name
}

// BackupPath 把文件名放进备份目录；不合法的名字（路径穿越等）返回错误。
func BackupPath(dir, name string) (string, error) {
	if !SafeBackupName(name) {
		return "", fmt.Errorf("非法备份文件名 %q", name)
	}
	p := filepath.Join(dir, name)
	if rel, err := filepath.Rel(dir, p); err != nil || rel != name {
		return "", fmt.Errorf("非法备份文件名 %q", name)
	}
	return p, nil
}

// BackupFileName 生成 nexadmin-<引擎>-v<框架版本>-<时间>.sql.gz；版本号里的特殊字符会被替换。
func BackupFileName(engine, version string, t time.Time) string {
	return BackupFileNameStamp(engine, version, t.Format("20060102-150405"))
}

// BackupFileNameStamp 同 BackupFileName，但时间戳由调用方给定（yyyymmdd-hhmmss）。
func BackupFileNameStamp(engine, version, stamp string) string {
	v := strings.Trim(nonVersion.ReplaceAllString(version, "-"), "-.")
	for strings.Contains(v, "..") {
		v = strings.ReplaceAll(v, "..", ".")
	}
	if v == "" {
		v = "dev"
	}
	return fmt.Sprintf("nexadmin-%s-v%s-%s.sql.gz", engine, v, stamp)
}

// ---- 导出 / 导入 ----

// DumpResult 是一次导出的结果。
type DumpResult struct {
	Path   string
	Size   int64 // 压缩后的文件大小
	SHA256 string
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// DumpGzip 执行导出命令，把输出 gzip 压缩写入 path（先写 .tmp 再改名，权限 0600，目录 0700），
// 同时计算 sha256 并写 path.sha256 旁路文件。失败时不留下半成品。
func DumpGzip(ctx context.Context, t ToolSpec, path string) (DumpResult, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return DumpResult{}, err
	}
	_ = os.Chmod(dir, 0o700)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return DumpResult{}, err
	}
	h, cnt := sha256.New(), &countWriter{}
	gz := gzip.NewWriter(io.MultiWriter(f, h, cnt))
	raw := &countWriter{}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.Name, t.Args...)
	cmd.Env = append(os.Environ(), t.Env...)
	cmd.Stdout, cmd.Stderr = io.MultiWriter(gz, raw), &limitedBuffer{b: &stderr, max: 4096}
	runErr := cmd.Run()
	if runErr == nil && raw.n == 0 {
		runErr = errors.New("导出内容为空")
	}
	closeErr := gz.Close()
	if syncErr := f.Sync(); closeErr == nil {
		closeErr = syncErr
	}
	if cerr := f.Close(); closeErr == nil {
		closeErr = cerr
	}
	if runErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if runErr != nil {
			return DumpResult{}, fmt.Errorf("%s: %w: %s", t.Name, runErr, t.redact(stderr.String()))
		}
		return DumpResult{}, closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return DumpResult{}, err
	}
	_ = os.Chmod(path, 0o600)
	sum := hex.EncodeToString(h.Sum(nil))
	if err := WriteSidecar(path, sum); err != nil {
		return DumpResult{}, err
	}
	return DumpResult{Path: path, Size: cnt.n, SHA256: sum}, nil
}

// limitedBuffer 只保留前 max 字节，避免工具输出大量错误撑爆内存。
type limitedBuffer struct {
	b   *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.b.Len(); room > 0 {
		l.b.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// RestoreGzip 把 gzip 压缩的 SQL 文件通过标准输入交给导入命令。
func RestoreGzip(ctx context.Context, t ToolSpec, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("不是有效的 gzip 文件: %w", err)
	}
	defer gz.Close()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, t.Name, t.Args...)
	cmd.Env = append(os.Environ(), t.Env...)
	cmd.Stdin, cmd.Stderr = gz, &limitedBuffer{b: &stderr, max: 4096}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", t.Name, err, t.redact(stderr.String()))
	}
	return nil
}

// ---- sha256 ----

// SidecarPath 是备份文件旁边记录 sha256 的文件。
func SidecarPath(path string) string { return path + ".sha256" }

// WriteSidecar 写 "<sha256>  <文件名>"（与 sha256sum 格式一致），权限 0600。
func WriteSidecar(path, sum string) error {
	return os.WriteFile(SidecarPath(path), []byte(sum+"  "+filepath.Base(path)+"\n"), 0o600)
}

// ReadSidecar 读取旁路文件里的 sha256。
func ReadSidecar(path string) (string, error) {
	b, err := os.ReadFile(SidecarPath(path))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("%s 格式不正确", SidecarPath(path))
	}
	return strings.ToLower(fields[0]), nil
}

// FileSHA256 计算文件的 sha256（十六进制小写）。
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySHA256 校验文件内容与期望的 sha256 一致。
func VerifySHA256(path, want string) error {
	got, err := FileSHA256(path)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(strings.TrimSpace(want)))) != 1 {
		return fmt.Errorf("sha256 校验失败：文件实际为 %s，期望 %s（文件可能已损坏或被篡改）", got, want)
	}
	return nil
}
