package config

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/auth"
	tn "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "config" }

// Register 注册参数与设置页路由。
// 权限码：config:list|create|update|delete（参数），settings:list|update（设置页）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	svc := NewService(d.DB)
	svc.aiAllowPrivate = d.Config.AI.AllowPrivateBaseURL
	svc.log = d.Log
	svc.notifier = d.Notify
	d.Notify.SetSettings(svc.notifySettings) // 邮件 / 机器人渠道的配置来自设置页「通知渠道」分区
	h := &Handler{svc: svc}
	// 动态 manifest（core/static.go）用的公开站点信息：站点名与 Logo 来自设置页「站点」分区。
	core.SetPublicSiteSource(func(ctx context.Context) (core.SiteInfo, error) {
		p, err := svc.Public(ctx)
		if err != nil {
			return core.SiteInfo{}, err
		}
		return core.SiteInfo{Name: p.SiteName, LogoURL: p.LogoURL}, nil
	})
	if d.OnClose != nil {
		d.OnClose(func() { core.SetPublicSiteSource(nil) })
	}
	// 文件模块的上传限制来自这里（设置页「上传限制」，sys_config 的 upload.* 行）；模块之间只通过 core 的登记点通信。
	core.SetUploadLimitsSource(svc.uploadLimits)
	if d.OnClose != nil {
		d.OnClose(func() { core.SetUploadLimitsSource(nil) })
	}
	// 自助注册的运行时开关来自「参数设置」的 user.register_enabled（默认关闭）；
	// 行缺失 / 读库失败回退 YAML auth.allow_register。/auth/register 与 /auth/methods 都从这里取。
	auth.SetRegisterEnabledSource(svc.registerEnabledSourceFn(d.Config.Auth.AllowRegister))
	if d.OnClose != nil {
		d.OnClose(func() { auth.SetRegisterEnabledSource(nil) })
	}
	// 「登录验证码」的运行时开关来自「参数设置」的 login.captcha_enabled（出厂 true = 默认强制）；
	// 关闭后回落到自适应策略（连续失败达到阈值才要求）。/auth/login 与 /auth/methods 都从这里取。
	auth.SetLoginCaptchaSource(svc.loginCaptchaSourceFn(true))
	if d.OnClose != nil {
		d.OnClose(func() { auth.SetLoginCaptchaSource(nil) })
	}

	r.GET("/config/upload", h.GetUpload) // 上传组件读取限制，登录即可
	g := r.Group("/configs")
	g.GET("/upload", core.Perm("config:list"), h.GetUpload) // 固定路径先于 /:id
	g.PUT("/upload", core.Perm("config:update"), h.SaveUpload)
	g.GET("/upload-types", core.Perm("config:list"), h.UploadTypes)
	g.POST("/refresh-cache", core.Perm("config:update"), h.RefreshCache)
	g.GET("", core.Perm("config:list"), h.List)
	g.GET("/:id", core.Perm("config:list"), h.Get)
	g.POST("", core.Perm("config:create"), h.Create)
	g.PUT("/:id", core.Perm("config:update"), h.Update)
	g.DELETE("/:id", core.Perm("config:delete"), h.DeleteOne)
	g.DELETE("", core.Perm("config:delete"), h.DeleteMany)

	d.Public.GET("/settings/public", h.PublicSettings) // 匿名：登录页展示站点名称/Logo
	s := r.Group("/settings")
	s.GET("", core.Perm("settings:list"), h.Settings)
	s.PUT("/:section", core.Perm("settings:update"), h.SaveSection)
	s.POST("/:section/test", core.Perm("settings:update"), h.TestSection)
}

// 种子 ID 与前端 mock 一致：7300000000010000000 + 序号。
func seedID(n int) idgen.ID { return idgen.ID(7300000000010000000 + int64(n)) }

var defaultUploadTypes = []string{
	"image/jpeg", "image/png", "image/gif", "image/webp", "video/mp4", "video/webm",
	"application/pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".csv", ".txt", ".zip",
}

var uploadTypeOptions = []TypeOption{
	{"image/jpeg", "JPEG (image/jpeg)"}, {"image/png", "PNG (image/png)"}, {"image/gif", "GIF (image/gif)"},
	{"image/webp", "WebP (image/webp)"}, {"image/svg+xml", "SVG (image/svg+xml)"}, {"video/mp4", "MP4 (video/mp4)"},
	{"video/webm", "WebM (video/webm)"}, {"application/pdf", "PDF (application/pdf)"}, {".doc", "Word (.doc)"},
	{".docx", "Word (.docx)"}, {".xls", "Excel (.xls)"}, {".xlsx", "Excel (.xlsx)"}, {".ppt", "PowerPoint (.ppt)"},
	{".pptx", "PowerPoint (.pptx)"}, {".zip", "ZIP (.zip)"}, {".txt", "Text (.txt)"},
}

var factoryRows = []struct {
	n                int
	name, key, value string
	typ              string
	public           bool
	remark           string
}{
	{3, "版权信息", "site.copyright", "© 2026 NexAdmin", TypeString, true, "页脚展示"},
	{4, "用户初始密码", "user.default_password", "Nex@123456", TypeString, false, "新建用户及重置密码时使用"},
	{5, "开放注册", "user.register_enabled", "false", TypeBoolean, true, ""},
	{6, "登录验证码", "login.captcha_enabled", "true", TypeBoolean, true, "开启=登录必须先过图形验证码；关闭=连续失败达到阈值后才要求"},
	{7, "登录最大重试次数", "login.max_retry", "5", TypeNumber, false, ""},
	{8, "账号锁定时长(分钟)", "login.lock_minutes", "30", TypeNumber, false, ""},
	{10, "IP 白名单", "security.ip_whitelist", `["10.0.0.0/8","192.168.0.0/16"]`, TypeJSON, false, "仅超管端生效"},
	{11, "图片上传大小上限(MB)", KeyUploadImage, "0", TypeNumber, true, "0 或留空表示不限制"},
	{12, "视频上传大小上限(MB)", KeyUploadVideo, "0", TypeNumber, true, "0 或留空表示不限制"},
	{13, "普通文件上传大小上限(MB)", KeyUploadFile, "0", TypeNumber, true, "0 或留空表示不限制"},
	{14, "允许上传的文件类型", KeyUploadTypes, strings.Join(defaultUploadTypes, ","), TypeString, true, "逗号分隔的 MIME 或扩展名"},
}

// WriteSiteName 实现 setup.SiteWriter：初始化向导 / init --config 把站点名写入设置页「站点」分区。
// 只覆盖 siteName，其余字段（Logo、版权等）保持库里已有值或默认值；可重复执行。
func (module) WriteSiteName(ctx context.Context, db *gorm.DB, siteName string) error {
	if strings.TrimSpace(siteName) == "" {
		return nil
	}
	_, err := NewService(db.WithContext(tn.Skip(ctx))).SaveSection(ctx, "site", map[string]any{"siteName": siteName})
	return err
}

// Seed 幂等种入出厂参数，按参数键判断，已存在一律不动。
func (module) Seed(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(context.Background()))
	now := time.Now().UTC()
	rows := factoryRows
	for _, r := range rows {
		c := &Config{Name: r.name, Key: r.key, Value: r.value, Type: r.typ, IsPublic: r.public, Remark: r.remark}
		c.ID = seedID(r.n)
		if err := seedConfig(db, c, now.Add(-time.Duration(90-r.n*4)*24*time.Hour)); err != nil {
			return err
		}
	}
	return nil
}

// SeedOnStartup 实现 setup.StartupSeeder：一次查询比对出厂参数键，齐全就返回；缺项才走完整的幂等 Seed。
// 老版本出厂参数里的 site.name / site.logo 是无消费的死配置（站点信息实际由设置页「站点」分区的
// siteName / logoUrl 提供，默认 logo 也早已统一为 /icon.svg），这里幂等清掉，让参数页与生效配置一致。
func (m module) SeedOnStartup(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(db.Statement.Context))
	if err := db.Where("config_key IN ?", []string{"site.name", "site.logo"}).Delete(&Config{}).Error; err != nil {
		return err
	}
	var keys []string
	if err := db.Model(&Config{}).Pluck("config_key", &keys).Error; err != nil {
		return err
	}
	have := make(map[string]bool, len(keys))
	for _, k := range keys {
		have[k] = true
	}
	for _, r := range factoryRows {
		if !have[r.key] {
			return m.Seed(db)
		}
	}
	return nil
}
