// Package config 加载 config/default.yaml（框架默认值）+ config/config.yaml（用户覆盖项）。
package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"nexadmin/core/storage"
)

type Config struct {
	App struct {
		Env      string `mapstructure:"env"` // dev | production
		Name     string `mapstructure:"name"`
		Timezone string `mapstructure:"timezone"`
		// AutoMigrate 启动时自动执行待迁移的 SQL（默认 true）。生产环境可设 false：有待执行迁移就拒绝启动，
		// 改由 `nexadmin-server migrate` 手动执行。
		AutoMigrate bool `mapstructure:"auto_migrate"`
	} `mapstructure:"app"`
	Server struct {
		Port int `mapstructure:"port"`
		// CORSOrigins 是允许跨域的 Origin 白名单（精确匹配，不支持通配符）。空 = 同源，生产不输出 CORS 头；
		// 开发环境（app.env=dev）无论是否配置都放行前端开发服务器。
		CORSOrigins []string `mapstructure:"cors_origins"`
		// TrustedProxies 是可信反向代理的 IP / CIDR 列表。默认空 = 不信任任何代理，c.ClientIP() 只用 TCP 对端地址。
		// 只有对端地址落在此列表里，才会采信 X-Forwarded-For / X-Real-IP，并取其中最右一个不在列表内的地址作为真实客户端 IP。
		// 反向代理部署必须配置，否则所有 IP 限流、登录日志、IP 白名单看到的都是代理地址。
		TrustedProxies []string `mapstructure:"trusted_proxies"`
	} `mapstructure:"server"`
	Database  Database   `mapstructure:"database"`
	Cache     Cache      `mapstructure:"cache"`
	Languages []Language `mapstructure:"languages"`
	Auth      Auth       `mapstructure:"auth"`
	SMS       struct {
		Provider string `mapstructure:"provider"` // log | webhook | aliyun | tencent；空 = 不启用
		Template string `mapstructure:"template"` // 验证码模板标识（云厂商模板 ID / webhook 的 template 字段）
		Webhook  struct {
			URL    string `mapstructure:"url"`
			Secret string `mapstructure:"secret"`
		} `mapstructure:"webhook"`
		Aliyun struct {
			AccessKeyID     string `mapstructure:"access_key_id"`
			AccessKeySecret string `mapstructure:"access_key_secret"`
			SignName        string `mapstructure:"sign_name"`
			Endpoint        string `mapstructure:"endpoint"`
		} `mapstructure:"aliyun"`
		Tencent struct {
			SecretID       string              `mapstructure:"secret_id"`
			SecretKey      string              `mapstructure:"secret_key"`
			SDKAppID       string              `mapstructure:"sdk_app_id"`
			SignName       string              `mapstructure:"sign_name"`
			Region         string              `mapstructure:"region"`
			Endpoint       string              `mapstructure:"endpoint"`
			NotifyTemplate string              `mapstructure:"notify_template"`
			ParamOrder     map[string][]string `mapstructure:"param_order"`
		} `mapstructure:"tencent"`
	} `mapstructure:"sms"`
	Upload struct {
		MaxSizeImageMB int `mapstructure:"max_size_image_mb"`
		MaxSizeVideoMB int `mapstructure:"max_size_video_mb"`
		MaxSizeFileMB  int `mapstructure:"max_size_file_mb"`
	} `mapstructure:"upload"`
	// Storage 是文件存储：driver local（默认，data/uploads）或 s3（S3 兼容）；s3.* 字符串支持 ${ENV}。
	Storage storage.Config `mapstructure:"storage"`
	Message struct {
		RetentionDays int `mapstructure:"retention_days"`
	} `mapstructure:"message"`
	// Job 是异步任务运行器（core/job）：并发 worker 数、结果文件保留天数、每用户进行中任务上限；0 = 用默认值。
	Job struct {
		Workers    int `mapstructure:"workers"`
		KeepDays   int `mapstructure:"keep_days"`
		MaxPerUser int `mapstructure:"max_per_user"`
	} `mapstructure:"job"`
	// Export 是导出行为：匹配行数不超过 sync_limit 时同步直出，超过则前端改走导出中心的异步任务；0 = 默认 5000。
	Export struct {
		SyncLimit int `mapstructure:"sync_limit"`
	} `mapstructure:"export"`
	// Log 是日志模块（modules/log）：export_max_rows 是操作日志 / 登录日志服务端导出的行数上限，超过则拒绝并提示缩小范围；<=0 = 默认 50000。
	Log struct {
		ExportMaxRows int `mapstructure:"export_max_rows"`
	} `mapstructure:"log"`
	Setup struct {
		AllowRemote bool `mapstructure:"allow_remote"`
	} `mapstructure:"setup"`
	AI struct {
		BaseURL string `mapstructure:"base_url"`
		APIKey  string `mapstructure:"api_key"`
		Model   string `mapstructure:"model"`
		// AllowPrivateBaseURL 允许 base_url 指向私网 / 回环地址（Ollama、vLLM、内网网关）。默认 false；
		// 链路本地与云元数据地址（169.254.0.0/16、fd00:ec2::254 等）无论如何都拒绝。
		AllowPrivateBaseURL bool `mapstructure:"allow_private_base_url"`
	} `mapstructure:"ai"`
	DisabledModules []string  `mapstructure:"disabled_modules"`
	RateLimit       RateLimit `mapstructure:"ratelimit"`
	// Backup 是数据库备份（modules/system/backup）：目录、保留策略、每天定时。后三项只是初始值，
	// 页面上保存后以数据库里的设置为准（多实例共享）。
	Backup Backup `mapstructure:"backup"`
	// Metrics 是 /metrics（Prometheus 文本格式）：enabled 默认 true 但仅回环地址可访问；配置 token 后必须带 Bearer。
	Metrics struct {
		Enabled bool   `mapstructure:"enabled"`
		Token   string `mapstructure:"token"`
	} `mapstructure:"metrics"`
	// Region 是省市区数据（modules/region）：在线同步的默认数据源，页面上的「立即同步」预填它。
	Region struct {
		// SourceURL 默认拉取的 HTTPS 地址；空则只能用随包发布的内置数据。
		SourceURL string `mapstructure:"source_url"`
		// SourceLicense 是该数据源声明的许可证，同步时随快照一起记录，仅用于展示与溯源。
		SourceLicense string `mapstructure:"source_license"`
	} `mapstructure:"region"`
	// Secret 是静态加密（core/secret）：主密钥来自 NEXADMIN_SECRET_KEY 或 data/secret.key。
	// algorithm 默认 aes-gcm；sm4-gcm 为国密选项（新密文 enc:v2:，旧 v1 密文仍可解）。
	Secret struct {
		Algorithm string `mapstructure:"algorithm"`
	} `mapstructure:"secret"`

	// 以下由 Load 填充，不来自 yaml。
	DefaultConfigPath string `mapstructure:"-"`
	UserConfigPath    string `mapstructure:"-"`
	DataDir           string `mapstructure:"-"`
	LocalesDir        string `mapstructure:"-"`
}

type Database struct {
	Type      string    `mapstructure:"type"` // mysql | mariadb | postgres
	Host      string    `mapstructure:"host"`
	Port      int       `mapstructure:"port"`
	User      string    `mapstructure:"user"`
	Password  string    `mapstructure:"password"`
	Name      string    `mapstructure:"name"`
	NodeID    int       `mapstructure:"node_id"` // 雪花 ID 节点号 0-1023
	Isolation Isolation `mapstructure:"isolation"`
}

// Isolation 只从服务器配置读取，连接信息不存入租户表。
type Isolation struct {
	Enabled        bool                      `mapstructure:"enabled"`
	AutoMigrate    bool                      `mapstructure:"auto_migrate"` // startup tenant migrations additionally require app.auto_migrate
	MaxPools       int                       `mapstructure:"max_pools"`
	MaxOpen        int                       `mapstructure:"max_open"`
	MaxIdle        int                       `mapstructure:"max_idle"`
	Lifetime       time.Duration             `mapstructure:"lifetime"`
	Tables         []string                  `mapstructure:"tables"`
	PlatformTables []string                  `mapstructure:"platform_tables"`
	Tenants        map[string]TenantDatabase `mapstructure:"tenants"`
}

type TenantDatabase struct {
	Type        string `mapstructure:"type"`
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`
	User        string `mapstructure:"user"`
	PasswordEnv string `mapstructure:"password_env"`
	Name        string `mapstructure:"name"`
}

// Backup 是 backup.* 配置。
type Backup struct {
	Dir       string `mapstructure:"dir"`        // 备份目录，默认 ./backup（权限 0700）
	KeepDays  int    `mapstructure:"keep_days"`  // 保留天数，0 = 不按天清理
	KeepCount int    `mapstructure:"keep_count"` // 保留份数，0 = 不按份数清理
	Schedule  string `mapstructure:"schedule"`   // 每天固定时刻 "HH:MM"，空 = 关闭定时
}

// ValidSchedule 判断定时是否合法：空串或 24 小时制 "HH:MM"。
func ValidSchedule(s string) bool {
	if s == "" {
		return true
	}
	t, err := time.Parse("15:04", s)
	return err == nil && t.Format("15:04") == s
}

type Cache struct {
	Driver   string `mapstructure:"driver"` // memory | redis
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
}

// Rule 是一条固定窗口限流规则：Window 内最多 Limit 次；Limit <= 0 表示这条规则不限流。
type Rule struct {
	Limit  int           `mapstructure:"limit"`
	Window time.Duration `mapstructure:"window"`
}

// RateLimit 是 ratelimit.* 配置。计数存放在 cache（redis 时多实例共享，memory 时仅本进程）。
type RateLimit struct {
	Enabled        bool `mapstructure:"enabled"`         // 总开关；false 时所有规则放行
	Login          Rule `mapstructure:"login"`           // 登录
	Refresh        Rule `mapstructure:"refresh"`         // 刷新令牌
	PasswordChange Rule `mapstructure:"password_change"` // 修改密码
	Captcha        Rule `mapstructure:"captcha"`         // 图形验证码
	SMSCode        Rule `mapstructure:"sms_code"`
	Token          Rule `mapstructure:"token"` // /oauth/token 失败次数
	Setup          Rule `mapstructure:"setup"` // 初始化向导
	Upload         Rule `mapstructure:"upload"`
	AI             Rule `mapstructure:"ai"`

	PATCreate   Rule `mapstructure:"pat_create"`   // 创建个人访问令牌（按用户）
	OpenAPISign Rule `mapstructure:"openapi_sign"` // 开放平台令牌/签名校验失败次数（按 IP）
	ClientError Rule `mapstructure:"client_error"` // 前端错误上报（按 IP）

	DashboardLayout Rule `mapstructure:"dashboard_layout"` // 工作台布局读写（按用户）
	LogExport       Rule `mapstructure:"log_export"`       // 操作日志 / 登录日志服务端导出（按用户）
	TenantAPI       Rule `mapstructure:"tenant_api"`       // 租户内全部后台接口的总量（按租户）；limit=0 表示不限流

	MCP        Rule `mapstructure:"mcp"`         // POST /api/v1/mcp（按用户）
	MCPConfirm Rule `mapstructure:"mcp_confirm"` // MCP 写确认：密码校验失败次数（按用户+确认 ID）
}

// Rule 按名字取规则；未知名字或总开关关闭时返回零值（不限流）。
func (r RateLimit) Rule(name string) Rule {
	if !r.Enabled {
		return Rule{}
	}
	switch name {
	case "login":
		return r.Login
	case "refresh":
		return r.Refresh
	case "password_change":
		return r.PasswordChange
	case "captcha":
		return r.Captcha
	case "sms_code":
		return r.SMSCode
	case "token":
		return r.Token
	case "setup":
		return r.Setup
	case "upload":
		return r.Upload
	case "ai":
		return r.AI
	case "pat_create":
		return r.PATCreate
	case "openapi_sign":
		return r.OpenAPISign
	case "client_error":
		return r.ClientError
	case "dashboard_layout":
		return r.DashboardLayout
	case "log_export":
		return r.LogExport
	case "tenant_api":
		return r.TenantAPI
	case "mcp":
		return r.MCP
	case "mcp_confirm":
		return r.MCPConfirm
	}
	return Rule{}
}

// Language 是一门语言的完整档案。config.yaml 里的 languages 只是首次启动的种子，
// 落库后以 sys_language 为准（见 core/i18n 的清单同步）；除前三项外都可缺省，由种子推导。
type Language struct {
	Code    string `mapstructure:"code" json:"code"`
	Name    string `mapstructure:"name" json:"name"`
	Dir     string `mapstructure:"dir" json:"dir"`
	Default bool   `mapstructure:"default" json:"default"`
	// Short 是紧凑展示名（列表徽标一类）。
	Short string `mapstructure:"short" json:"short,omitempty"`
	// HTMLLang 写进 <html lang> 与 hreflang；缺省等于 Code。
	HTMLLang string `mapstructure:"htmlLang" json:"htmlLang,omitempty"`
	// OGLocale 写进 og:locale；缺省由 Code 推导（en-US → en_US）。
	OGLocale string `mapstructure:"ogLocale" json:"ogLocale,omitempty"`
	// Prefix 是公开站的 URL 路径前缀；默认语言必须为空，其余语言必须非空。
	Prefix string `mapstructure:"prefix" json:"prefix,omitempty"`
	// DateFormat 是 Go 布局串；缺省为 "2006-01-02"。
	DateFormat string `mapstructure:"dateFormat" json:"dateFormat,omitempty"`
}

type Auth struct {
	Methods       []string       `mapstructure:"methods"`
	Providers     map[string]any `mapstructure:"providers"`
	Proxy         string         `mapstructure:"proxy"`
	SingleSession bool           `mapstructure:"single_session"` // true = 同一用户只保留最后一次登录
	JWTSecret     string         `mapstructure:"jwt_secret"`     // 空 = 自动生成并保存到 data/jwt.secret
	AccessTTL     time.Duration  `mapstructure:"access_ttl"`
	RefreshTTL    time.Duration  `mapstructure:"refresh_ttl"`
	// 自助注册（POST /auth/register）：默认关闭。开启后登录页出现注册入口，
	// 注册强制图形验证码并受 IP 限流，建号走与手工创建相同的租户额度检查。
	AllowRegister bool     `mapstructure:"allow_register"`
	RegisterRoles []string `mapstructure:"register_roles"` // 注册用户默认挂的角色码（在当前租户内解析）；空 = 不挂角色，待管理员分配
}

// DefaultLanguage 返回默认语言码。
func (c *Config) DefaultLanguage() string {
	for _, l := range c.Languages {
		if l.Default {
			return l.Code
		}
	}
	return c.Languages[0].Code
}

// IsProduction 是否生产环境。
func (c *Config) IsProduction() bool { return c.App.Env == "production" }

// Load 读取框架默认值，再合并用户配置（文件不存在则忽略），环境变量 NEXADMIN_* 优先级最高。
func Load(defaultPath, userPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(defaultPath)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read %s: %w", defaultPath, err)
	}
	if _, err := os.Stat(userPath); err == nil {
		v.SetConfigFile(userPath)
		if err := v.MergeInConfig(); err != nil {
			return nil, fmt.Errorf("config: merge %s: %w", userPath, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: stat %s: %w", userPath, err)
	}
	v.SetEnvPrefix("NEXADMIN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: decode: %w", err)
	}
	cfg.DefaultConfigPath = defaultPath
	cfg.UserConfigPath = userPath
	cfg.DataDir = "data"
	cfg.LocalesDir = "config/locales"
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ValidateTrustedProxies 要求 server.trusted_proxies 的每一项都是合法 IP 或 CIDR；空项一律报错，避免 "" 被悄悄忽略。
func ValidateTrustedProxies(list []string) error {
	for _, s := range list {
		s = strings.TrimSpace(s)
		if net.ParseIP(s) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(s); err != nil {
			return fmt.Errorf("config: server.trusted_proxies has invalid IP/CIDR %q", s)
		}
	}
	return nil
}

func (c *Config) validate() error {
	switch c.App.Env {
	case "dev", "production":
	default:
		return fmt.Errorf("config: app.env must be dev or production, got %q", c.App.Env)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("config: server.port %d out of range", c.Server.Port)
	}
	if err := ValidateTrustedProxies(c.Server.TrustedProxies); err != nil {
		return err
	}
	switch c.Database.Type {
	case "mysql", "mariadb", "postgres":
	default:
		return fmt.Errorf("config: database.type must be mysql, mariadb or postgres, got %q", c.Database.Type)
	}
	if c.Database.NodeID < 0 || c.Database.NodeID > 1023 {
		return fmt.Errorf("config: database.node_id %d out of range 0-1023", c.Database.NodeID)
	}
	switch c.Cache.Driver {
	case "memory":
	case "redis":
		if c.Cache.Addr == "" {
			return fmt.Errorf("config: cache.addr is required when cache.driver is redis")
		}
	default:
		return fmt.Errorf("config: cache.driver must be memory or redis, got %q", c.Cache.Driver)
	}
	if len(c.Languages) == 0 {
		return fmt.Errorf("config: languages must not be empty")
	}
	if c.Auth.AccessTTL <= 0 || c.Auth.RefreshTTL <= 0 {
		return fmt.Errorf("config: auth.access_ttl and auth.refresh_ttl must be positive")
	}
	if err := c.Storage.Validate(); err != nil {
		return err
	}
	if !ValidSchedule(c.Backup.Schedule) {
		return fmt.Errorf("config: backup.schedule must be empty or HH:MM, got %q", c.Backup.Schedule)
	}
	if c.Backup.KeepDays < 0 || c.Backup.KeepCount < 0 {
		return fmt.Errorf("config: backup.keep_days and backup.keep_count must not be negative")
	}
	switch c.Secret.Algorithm {
	case "", "aes-gcm", "sm4-gcm":
	default:
		return fmt.Errorf("config: secret.algorithm must be aes-gcm or sm4-gcm, got %q", c.Secret.Algorithm)
	}
	return nil
}
