// Package setup 是初始化向导后端：检测“是否已初始化”、环境检查、连接测试、一次性执行初始化。
// 向导路由只在未初始化的进程里注册；完成后 /setup/* 一律 404（见设计文档 §17）。
package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	_ "time/tzdata" // 时区校验不依赖服务器上的 zoneinfo

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/response"
)

// Admin 是向导里填写的超管与站点信息；Password 是明文，用 auth.HashPassword 处理后再入库，不要写日志。
type Admin struct {
	Username string
	Password string
	SiteName string
	Language string
	Timezone string
}

// 下面几个接口由模块实现（core 会扫描已注册的模块），向导按顺序调用。
// ctx / db 已跳过租户隔离。所有写入必须幂等（出厂数据“不存在才插入，已存在一律不动”）。
// 建表不再由模块自己执行：框架 / 业务 / 插件的迁移 SQL 统一由 core/migrate 运行器按文件执行。

// Seeder 种入出厂数据。向导（init）时所有 Seeder 都会运行。
type Seeder interface{ Seed(db *gorm.DB) error }

// StartupSeeder 是可以在每次启动时运行的 Seeder：只补缺失的出厂项（菜单、字典、参数），
// 已齐全时必须几乎零开销（一次查询比对）。示例 / 演示数据（用户、公告、日志等）不要实现它，
// 否则客户删掉的行会在重启后又冒出来。
type StartupSeeder interface{ SeedOnStartup(db *gorm.DB) error }

// AdminCreator 创建超管（通常由用户模块实现）。
type AdminCreator interface {
	CreateAdmin(ctx context.Context, db *gorm.DB, a Admin) error
}

// AdminChecker 报告库里是否已有管理员，用于判断是否已初始化。
type AdminChecker interface {
	HasAdmin(db *gorm.DB) (bool, error)
}

// SiteWriter 把向导里填的站点名写入设置页「站点」分区（sys_setting.site.siteName，系统参数模块实现）。
// 登录页、顶栏、PWA manifest 显示的站点名都取自这个分区；config.yaml 的 app.name 只用于日志 / 默认名称。幂等：重复执行只覆盖站点名。
type SiteWriter interface {
	WriteSiteName(ctx context.Context, db *gorm.DB, siteName string) error
}

// Hooks 汇总初始化钩子。
type Hooks struct {
	Migrate      func(db *gorm.DB) error // 统一迁移运行器（core 装配）：框架 + 业务 + 已启用插件
	Seeders      []Seeder
	AdminCreator AdminCreator
	AdminChecker AdminChecker
	SiteWriter   SiteWriter // 可为空（系统参数模块被禁用时）：站点名只留在 app.name
}

// MarkerPath 是初始化标记文件。
func MarkerPath(cfg *config.Config) string { return filepath.Join(cfg.DataDir, ".initialized") }

// IsInitialized 判断系统是否已初始化。原则：宁可错判为已初始化，也不错判为未初始化。
//   - 标记文件存在 -> 已初始化
//   - config/config.yaml 不存在 -> 全新安装，什么都没配置过，未初始化
//   - 否则探测数据库：连不上、读不到、没有管理员检查器 -> 按已初始化处理；连得上且没有管理员 -> 未初始化
func IsInitialized(cfg *config.Config, checker AdminChecker, log *zap.Logger) bool {
	if _, err := os.Stat(MarkerPath(cfg)); err == nil {
		return true
	}
	if _, err := os.Stat(cfg.UserConfigPath); os.IsNotExist(err) {
		return false
	}
	if checker == nil {
		log.Warn("setup: no module provides AdminChecker, treating system as initialized")
		return true
	}
	db, err := database.Open(cfg.Database)
	if err != nil {
		log.Error("setup: cannot reach database, treating system as initialized", zap.Error(err))
		return true
	}
	defer database.Close(db)
	has, err := checker.HasAdmin(database.Platform(db))
	if err != nil {
		// 库可连通且一张表都没有（例如 reinit 清空之后）是明确的“未初始化”；其他读不到的情况仍按已初始化处理。
		if tables, terr := database.Platform(db).Migrator().GetTables(); terr == nil && len(tables) == 0 {
			return false
		}
		log.Error("setup: cannot read admin state, treating system as initialized", zap.Error(err))
		return true
	}
	return has
}

// Runner 持有向导运行所需的一切。
type Runner struct {
	Cfg    *config.Config
	Hooks  Hooks
	Log    *zap.Logger
	OnDone func() error // 初始化成功后调用（core 在这里切换到完整路由）
	Token  string       // 一次性初始化令牌，仅允许远程初始化时必须携带（X-Setup-Token）

	mu        sync.Mutex // 同一时刻只允许一个初始化在跑
	probeMu   sync.Mutex
	probeNext time.Time
}

// NewRunner 创建 Runner 并生成一次性令牌。
func NewRunner(cfg *config.Config, hooks Hooks, log *zap.Logger, onDone func() error) *Runner {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &Runner{Cfg: cfg, Hooks: hooks, Log: log, OnDone: onDone, Token: hex.EncodeToString(b)}
}

// PrintBanner 在控制台打印醒目的未初始化提示、向导地址和一次性令牌。
func (r *Runner) PrintBanner(w io.Writer) {
	fmt.Fprintf(w, "\n================ NexAdmin 尚未初始化 ================\n")
	fmt.Fprintf(w, "  向导地址:   http://localhost:%d/admin/setup\n", r.Cfg.Server.Port)
	fmt.Fprintf(w, "  初始化令牌: %s\n", r.Token)
	if r.Cfg.Setup.AllowRemote {
		fmt.Fprintf(w, "  已允许远程初始化：远程访问需在请求头 X-Setup-Token 携带上述令牌\n")
	} else {
		fmt.Fprintf(w, "  仅允许本机访问向导（setup.allow_remote=false）\n")
	}
	fmt.Fprintf(w, "=====================================================\n\n")
}

// Register 在 g（/api/v1/setup）下注册向导路由。
func (r *Runner) Register(g *gin.RouterGroup) {
	g.Use(r.guard)
	g.GET("/env", r.env)
	g.POST("/test-db", r.testDB)
	g.POST("/test-cache", r.testCache)
	g.POST("/test-s3", r.probeGuard, r.testS3)
	g.POST("/test-ai", r.probeGuard, r.testAI)
	g.POST("/execute", r.execute)
}

// viaProxy：请求带有任何常见的转发头，就当作经过了反向代理。这些头由客户端自带也可能，所以只能“更严”：
// 命中即不再按“本机”放行（宁可误伤本机 curl 带了转发头的情形，也不让同机 nginx 把远程用户当成回环）。
func viaProxy(h http.Header) bool {
	for _, k := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded", "X-Forwarded-Host"} {
		if h.Get(k) != "" {
			return true
		}
	}
	return false
}

// isLocal：对端地址（RemoteAddr，不用 ClientIP —— 转发头可伪造）是回环，且请求没有经过代理。
func isLocal(req *http.Request) bool {
	if viaProxy(req.Header) {
		return false
	}
	host, _, _ := net.SplitHostPort(req.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (r *Runner) guard(c *gin.Context) {
	if isLocal(c.Request) {
		c.Next()
		return
	}
	if !r.Cfg.Setup.AllowRemote {
		response.Fail(c, response.Forbidden("setup.local_only", r.Cfg.Server.Port))
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Setup-Token")), []byte(r.Token)) != 1 {
		response.Fail(c, response.Forbidden("setup.token_invalid"))
		return
	}
	c.Next()
}

func writable(dir string) (bool, string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err.Error()
	}
	f, err := os.CreateTemp(dir, ".wtest-*")
	if err != nil {
		return false, err.Error()
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true, "./" + dir
}

func (r *Runner) env(c *gin.Context) {
	type check struct {
		Key    string `json:"key"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail,omitempty"`
	}
	dataOK, dataDetail := writable(r.Cfg.DataDir)
	confOK, confDetail := writable(filepath.Dir(r.Cfg.UserConfigPath))
	response.OK(c, gin.H{"checks": []check{
		{"dataWritable", dataOK, dataDetail},
		{"configWritable", confOK, confDetail},
		{"port", true, fmt.Sprint(r.Cfg.Server.Port)},
		{"engines", true, "MySQL / MariaDB / PostgreSQL"},
	}})
}

func (r *Runner) testDB(c *gin.Context) {
	var req dbReq
	if !response.BindJSON(c, &req) {
		return
	}
	cfg, err := req.toConfig(r.Cfg.Database.NodeID)
	if err != nil {
		response.OK(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	// 勾选“自动创建”时库可能还不存在，只测到服务器这一层。
	version, err := database.Probe(ctx, cfg, req.AutoCreate)
	if err != nil {
		response.OK(c, gin.H{"ok": false, "message": redact(err.Error(), req.Password)})
		return
	}
	response.OK(c, gin.H{"ok": true, "message": "ok", "version": version})
}

func (r *Runner) testCache(c *gin.Context) {
	var req cacheReq
	if !response.BindJSON(c, &req) {
		return
	}
	ch, err := cache.New(req.toConfig())
	if err != nil {
		response.OK(c, gin.H{"ok": false, "message": redact(err.Error(), req.Password)})
		return
	}
	_ = ch.Close()
	response.OK(c, gin.H{"ok": true, "message": "ok"})
}
