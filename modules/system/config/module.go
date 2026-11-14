package config

import (
	"context"
	"fmt"
	"strconv"
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
	svc.authCfg = d.Config.Auth
	svc.smsProvider = d.Config.SMS.Provider
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
	// 「验证码」的运行时总开关来自设置页「安全与登录 → 登录安全」分区的 captchaEnabled（出厂开启）：
	// 密码登录、自助注册、匿名获取短信验证码三条链路同一个闸门，后台保存后下一次请求即生效。
	// 注入的 fallback 只在读库失败时兜底，刻意保持 true（fail-closed）：拿不到配置时宁可多要一次
	// 验证码，也不给脚本敞门。/auth/login、/auth/register、/auth/sms-code 与 /auth/methods 都从这里取。
	auth.SetCaptchaEnabledSource(svc.captchaEnabledSourceFn(true))
	if d.OnClose != nil {
		d.OnClose(func() { auth.SetCaptchaEnabledSource(nil) })
	}
	// 「验证码位数」同样来自参数设置（login.captcha_length，出厂 5，范围 4-8）：
	// 参数页改完对之后新生成的验证码立即生效，无需重启。
	auth.SetLoginCaptchaLengthSource(svc.captchaLengthSourceFn(auth.CaptchaLenDefault))
	if d.OnClose != nil {
		d.OnClose(func() { auth.SetLoginCaptchaLengthSource(nil) })
	}
	// 设置页「登录方式」分区的开关 / 凭据 / 回调地址 / 匹配规则 / 出站代理：每次请求现读库，
	// 保存后下一次请求即生效。没保存过的项（或找不到库）回退 config.yaml——见 auth_runtime.go。
	auth.SetProviderOverrideSource(svc.providerOverrides)
	auth.SetLoginMethodsSource(svc.loginMethods)
	auth.SetOAuthProxySource(svc.oauthProxy)
	if d.OnClose != nil {
		d.OnClose(func() {
			auth.SetProviderOverrideSource(nil)
			auth.SetLoginMethodsSource(nil)
			auth.SetOAuthProxySource(nil)
		})
	}

	r.GET("/config/upload", h.GetUpload) // 上传组件读取限制，登录即可
	g := r.Group("/configs")
	g.GET("/upload", core.Perm("config:list"), h.GetUpload) // 固定路径先于 /:id
	g.PUT("/upload", core.Perm("config:update"), core.RequirePlatform(), h.SaveUpload)
	g.GET("/upload-types", core.Perm("config:list"), h.UploadTypes)
	g.POST("/refresh-cache", core.Perm("config:update"), core.RequirePlatform(), h.RefreshCache)
	g.GET("", core.Perm("config:list"), h.List)
	g.GET("/:id", core.Perm("config:list"), h.Get)
	g.POST("", core.Perm("config:create"), core.RequirePlatform(), h.Create)
	g.PUT("/:id", core.Perm("config:update"), core.RequirePlatform(), h.Update)
	g.DELETE("/:id", core.Perm("config:delete"), core.RequirePlatform(), h.DeleteOne)
	g.DELETE("", core.Perm("config:delete"), core.RequirePlatform(), h.DeleteMany)

	d.Public.GET("/settings/public", h.PublicSettings) // 匿名：登录页展示站点名称/Logo
	s := r.Group("/settings")
	s.GET("", core.Perm("settings:list"), h.Settings)
	s.PUT("/:section", core.Perm("settings:update"), core.RequirePlatform(), h.SaveSection)
	s.POST("/:section/test", core.Perm("settings:update"), core.RequirePlatform(), h.TestSection)
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
	{9, "登录验证码位数", "login.captcha_length", strconv.Itoa(auth.CaptchaLenDefault), TypeNumber, true,
		// 出厂值与上下限都取自 core/auth：改一处三处（默认值、保存校验、运行时钳制）不会分家。
		fmt.Sprintf("%d-%d 位；改后对新生成的验证码生效", auth.CaptchaLenMin, auth.CaptchaLenMax)},
	{7, "登录最大重试次数", "login.max_retry", "5", TypeNumber, false, ""},
	{8, "账号锁定时长(分钟)", "login.lock_minutes", "30", TypeNumber, false, ""},
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
// siteName / logoUrl 提供，默认 logo 也早已统一为 /icon.svg），security.ip_whitelist 同样没有任何
// 代码读取它（超管端没有 IP 白名单闸），这里一并幂等清掉，让参数页与生效配置一致。
// login.captcha_enabled 是旧版「登录验证码」开关，现已迁到设置页「登录安全」分区的 captchaEnabled，
// 留着只会让管理员改到一个没人读的值，同样清掉。
func (m module) SeedOnStartup(db *gorm.DB) error {
	db = db.WithContext(tn.Skip(db.Statement.Context))
	dead := []string{"site.name", "site.logo", "security.ip_whitelist", "login.captcha_enabled"}
	if err := db.Where("config_key IN ?", dead).Delete(&Config{}).Error; err != nil {
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
