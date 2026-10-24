package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scope 取值：framework / business / plugin:<id>。
const (
	ScopeFramework = "framework"
	ScopeBusiness  = "business"
	ScopePrefix    = "plugin:" // 插件 scope = ScopePrefix + 插件 id
)

// Targets apply only when independent tenant databases are enabled.
const (
	TargetPlatform = "platform"
	TargetTenant   = "tenant"
)

// EnvDir 指定框架迁移目录（migrations/framework），业务迁移目录取它旁边的 business。
const EnvDir = "NEXADMIN_MIGRATIONS_DIR"

// Source 描述一个 scope 的迁移目录：Root 下按引擎分子目录（mysql / mariadb / postgres）。
type Source struct {
	Scope    string
	Root     string
	Required bool   // true：找不到目录就报错（框架迁移）；false：没有就当没有迁移（业务 / 插件）
	Target   string // platform | tenant; custom sources must classify independent-mode migrations
}

// SourceProvider 由模块实现，用来声明自己带的迁移目录（例如已授权的插件）。
type SourceProvider interface{ MigrationSources() []Source }

// File 是一个迁移文件。
type File struct {
	Scope    string
	Name     string // 文件名，同时是版本号
	Path     string
	SQL      string
	Checksum string
}

// Checksum 返回内容的 sha256；统一换行符，避免 git autocrlf 造成误报。
func Checksum(sql string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(sql, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// EngineDir 返回 root 下当前引擎的目录；mariadb 没有专属目录时回退 mysql。目录不存在返回 ("", false)。
func EngineDir(root, engine string) (string, bool) {
	try := []string{engine}
	if engine == "mariadb" {
		try = append(try, "mysql")
	}
	for _, e := range try {
		d := filepath.Join(root, e)
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d, true
		}
	}
	return "", false
}

// Discover 读出所有 scope 的迁移文件：scope 按 sources 顺序，scope 内按文件名排序。
func Discover(engine string, sources []Source) ([]File, error) {
	var out []File
	for _, s := range sources {
		dir, ok := EngineDir(s.Root, engine)
		if !ok {
			if s.Required {
				return nil, fmt.Errorf("migrate: 找不到 %s 迁移目录 %s（引擎 %s；请在项目根目录运行，或设置 %s）",
					s.Scope, filepath.Join(s.Root, engine), engine, EnvDir)
			}
			continue
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("migrate: 读取 %s: %w", dir, err)
		}
		var names []string
		for _, e := range ents {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			p := filepath.Join(dir, n)
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("migrate: 读取 %s: %w", p, err)
			}
			out = append(out, File{Scope: s.Scope, Name: n, Path: p, SQL: string(b), Checksum: Checksum(string(b))})
		}
	}
	return out, nil
}

// Roots 找到框架与业务迁移的根目录：NEXADMIN_MIGRATIONS_DIR > 当前目录及上级 > 可执行文件所在目录及上级。
// 业务目录固定是框架目录旁边的 business。
func Roots() (framework, business string, err error) {
	var cands []string
	if d := os.Getenv(EnvDir); d != "" {
		cands = append(cands, d)
	}
	var starts []string
	if wd, e := os.Getwd(); e == nil {
		starts = append(starts, wd)
	}
	if exe, e := os.Executable(); e == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, dir := range starts {
		for i := 0; i < 6; i++ {
			cands = append(cands, filepath.Join(dir, "migrations", "framework"))
			dir = filepath.Dir(dir)
		}
	}
	for _, c := range cands {
		if st, e := os.Stat(c); e == nil && st.IsDir() {
			return c, filepath.Join(filepath.Dir(c), "business"), nil
		}
	}
	return "", "", fmt.Errorf("migrate: 找不到框架迁移目录 migrations/framework（请在项目根目录运行，或设置 %s）", EnvDir)
}

// DefaultSources 返回框架 + 业务两个 scope 的目录。
func DefaultSources() ([]Source, error) {
	fw, biz, err := Roots()
	if err != nil {
		return nil, err
	}
	return []Source{
		{Scope: ScopeFramework, Root: fw, Required: true},
		{Scope: ScopeBusiness, Root: biz},
	}, nil
}
