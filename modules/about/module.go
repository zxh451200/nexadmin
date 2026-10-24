// Package about 是“关于”页：版本、构建时间、数据库引擎、Go 版本，以及后端依赖的开源许可证清单。
package about

import (
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

// PermView 与前端 route.tsx 一致。
const PermView = "about:view"

// Version 与 BuildTime 可在构建时注入：
//
//	go build -ldflags "-X nexadmin/modules/about.Version=1.0.0 -X nexadmin/modules/about.BuildTime=2026-10-01T08:30:00Z"
var (
	Version   = "0.1.0"
	BuildTime = ""
)

var startedAt = time.Now()

type module struct{}

func (module) Name() string { return "about" }

// Info 与前端 AboutInfo 一致。
type Info struct {
	Version   string `json:"version"`
	BuildTime string `json:"buildTime"`
	DBEngine  string `json:"dbEngine"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
}

// License 与前端 LicenseRow 一致。
type License struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	License string `json:"license"`
}

func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	db := d.DB
	r.GET("/about", core.Perm(PermView), func(c *gin.Context) {
		response.OK(c, Info{
			Version: Version, BuildTime: buildTime(), DBEngine: dbEngine(db),
			GoVersion: runtime.Version(), OS: runtime.GOOS + "/" + runtime.GOARCH,
		})
	})
	r.GET("/about/licenses", core.Perm(PermView), func(c *gin.Context) {
		response.OK(c, gin.H{"list": licenses()})
	})
}

// buildTime 优先用 ldflags 注入值，其次用 VCS 提交时间，都没有则退回进程启动时间。
func buildTime() string {
	if BuildTime != "" {
		return BuildTime
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.time" && s.Value != "" {
				return s.Value
			}
		}
	}
	return startedAt.UTC().Format(time.RFC3339)
}

// dbEngine 返回形如 "PostgreSQL 16.4" / "MySQL 8.4.0" / "MariaDB 10.11.6" 的字符串。
func dbEngine(db *gorm.DB) string {
	var v string
	switch db.Dialector.Name() {
	case "postgres":
		if err := db.Raw("SHOW server_version").Scan(&v).Error; err != nil {
			return "PostgreSQL"
		}
		v, _, _ = strings.Cut(v, " ")
		return "PostgreSQL " + v
	case "mysql":
		if err := db.Raw("SELECT VERSION()").Scan(&v).Error; err != nil {
			return "MySQL"
		}
		isMaria := strings.Contains(strings.ToLower(v), "mariadb")
		if isMaria { // 部分 MariaDB 版本号带 "5.5.5-" 兼容前缀，必须在按 "-" 截断之前去掉
			v = strings.TrimPrefix(v, "5.5.5-")
		}
		num, _, _ := strings.Cut(v, "-")
		if isMaria {
			return "MariaDB " + num
		}
		return "MySQL " + num
	}
	return db.Dialector.Name()
}

// knownLicenses 是直接依赖的许可证（人工核对，随 go.mod 变化时需要更新）；未收录的显示 Unknown。
var knownLicenses = map[string]string{
	"github.com/gin-gonic/gin":               "MIT",
	"github.com/go-sql-driver/mysql":         "MPL-2.0",
	"github.com/golang-jwt/jwt/v5":           "MIT",
	"github.com/redis/go-redis/v9":           "BSD-2-Clause",
	"github.com/spf13/viper":                 "MIT",
	"go.uber.org/zap":                        "MIT",
	"golang.org/x/crypto":                    "BSD-3-Clause",
	"gopkg.in/yaml.v3":                       "MIT AND Apache-2.0",
	"gorm.io/driver/mysql":                   "MIT",
	"gorm.io/driver/postgres":                "MIT",
	"gorm.io/gorm":                           "MIT",
	"github.com/jackc/pgx/v5":                "MIT",
	"github.com/bytedance/sonic":             "Apache-2.0",
	"github.com/go-playground/validator/v10": "MIT",
}

// licenses 列出编译进二进制的 Go 依赖（来自 debug.ReadBuildInfo）。
func licenses() []License {
	out := []License{}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return out
	}
	for _, m := range bi.Deps {
		lic := knownLicenses[m.Path]
		if lic == "" {
			lic = "Unknown"
		}
		out = append(out, License{Name: m.Path, Version: m.Version, License: lic})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
