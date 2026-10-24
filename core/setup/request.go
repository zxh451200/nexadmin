package setup

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/storage"
)

// flexInt 同时接受 3306 和 "3306"（前端表单里端口是字符串）。
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(string(b)), `"`))
	if err != nil {
		return fmt.Errorf("invalid number %s", b)
	}
	*f = flexInt(n)
	return nil
}

type dbReq struct {
	Type       string  `json:"type"` // mysql | mariadb | postgresql
	Host       string  `json:"host"`
	Port       flexInt `json:"port"`
	Username   string  `json:"username"`
	Password   string  `json:"password"`
	Database   string  `json:"database"`
	AutoCreate bool    `json:"autoCreate"`
}

type cacheReq struct {
	Type     string `json:"type"` // memory | valkey
	Addr     string `json:"addr"`
	Password string `json:"password"`
}

type adminReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SiteName string `json:"siteName"`
	Language string `json:"language"`
	Timezone string `json:"timezone"`
}

// optionalReq 是向导“可选项”：文件存储（S3）与 AI。字段名与前端 schema.ts 的 optional 一致。
type optionalReq struct {
	Storage     string `json:"storage"` // local（默认）| s3
	S3Endpoint  string `json:"s3Endpoint"`
	S3Bucket    string `json:"s3Bucket"`
	S3AccessKey string `json:"s3AccessKey"`
	S3SecretKey string `json:"s3SecretKey"`
	// 对应 storage.s3.region / prefix / path_style，向导与非交互 init --config 共用。
	S3Region    string `json:"s3Region"`
	S3Prefix    string `json:"s3Prefix"`
	S3PathStyle bool   `json:"s3PathStyle"`

	AIEnabled bool   `json:"aiEnabled"`
	AIBaseURL string `json:"aiBaseUrl"`
	AIAPIKey  string `json:"aiApiKey"`
	AIModel   string `json:"aiModel"`
}

type executeReq struct {
	DB       dbReq       `json:"db"`
	Cache    cacheReq    `json:"cache"`
	Admin    adminReq    `json:"admin"`
	Optional optionalReq `json:"optional"`
}

var (
	identRe    = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	usernameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,31}$`)
)

// configType 保留 mariadb 与 mysql 的区别，postgresql 写成 postgres。
func configType(raw string) string {
	if raw == "postgresql" {
		return "postgres"
	}
	return raw
}

func (d dbReq) toConfig(nodeID int) (config.Database, error) {
	if _, err := database.Normalize(d.Type); err != nil {
		return config.Database{}, err
	}
	if d.Host == "" || d.Username == "" || d.Port < 1 || d.Port > 65535 {
		return config.Database{}, fmt.Errorf("host, port and username are required")
	}
	if !identRe.MatchString(d.Database) {
		return config.Database{}, fmt.Errorf("invalid database name (letters, digits, underscore only)")
	}
	return config.Database{Type: configType(d.Type), Host: d.Host, Port: int(d.Port), User: d.Username, Password: d.Password, Name: d.Database, NodeID: nodeID}, nil
}

func (c cacheReq) toConfig() config.Cache {
	if c.Type == "valkey" || c.Type == "redis" {
		return config.Cache{Driver: "redis", Addr: c.Addr, Password: c.Password}
	}
	return config.Cache{Driver: "memory"}
}

// s3Config 返回要写入 storage 段的配置；未选 S3 返回 nil（不写任何 storage 项，沿用 default.yaml 的 local）。
// 字段名对齐 core/storage.Config（storage.driver / storage.s3.*）；值可以是 ${ENV}，由 storage.New 在启动时展开。
func (o optionalReq) s3Config() (map[string]any, error) {
	switch o.Storage {
	case "", "local":
		return nil, nil
	case "s3":
	default:
		return nil, fmt.Errorf("unknown storage %q", o.Storage)
	}
	cfg := storage.Config{Driver: "s3", S3: storage.S3Config{
		Endpoint: strings.TrimSpace(o.S3Endpoint), Bucket: strings.TrimSpace(o.S3Bucket),
		AccessKey: strings.TrimSpace(o.S3AccessKey), SecretKey: o.S3SecretKey,
		Region: strings.TrimSpace(o.S3Region), Prefix: strings.TrimSpace(o.S3Prefix), PathStyle: o.S3PathStyle,
	}}
	if err := cfg.Validate(); err != nil { // 缺项报错，信息里不含任何值
		return nil, fmt.Errorf("S3 storage needs endpoint, bucket, access key and secret key")
	}
	s3 := map[string]any{"endpoint": cfg.S3.Endpoint, "bucket": cfg.S3.Bucket, "access_key": cfg.S3.AccessKey, "secret_key": cfg.S3.SecretKey}
	if cfg.S3.Region != "" {
		s3["region"] = cfg.S3.Region
	}
	if cfg.S3.Prefix != "" {
		s3["prefix"] = cfg.S3.Prefix
	}
	if cfg.S3.PathStyle {
		s3["path_style"] = true
	}
	return map[string]any{"driver": "s3", "s3": s3}, nil
}

// redact 从错误文本里抹掉密码，密码和令牌不进日志也不回显。
func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}

// maxPasswordBytes：bcrypt 只取前 72 字节，超出部分不参与校验，所以直接拒绝。
const maxPasswordBytes = 72

// passwordAcceptable 不设任何强度规则：非空且不超过 72 字节即可。
func passwordAcceptable(pw string) bool {
	return pw != "" && len(pw) <= maxPasswordBytes
}

// validate 校验整个请求；返回的错误会原样展示在向导里（不含密码）。
func (r *Runner) validate(req *executeReq) (config.Database, error) {
	dbCfg, err := req.DB.toConfig(r.Cfg.Database.NodeID)
	if err != nil {
		return dbCfg, err
	}
	if req.Cache.toConfig().Driver == "redis" && req.Cache.Addr == "" {
		return dbCfg, fmt.Errorf("cache address is required")
	}
	if _, err := req.Optional.s3Config(); err != nil {
		return dbCfg, err
	}
	a := req.Admin
	if !usernameRe.MatchString(a.Username) {
		return dbCfg, fmt.Errorf("invalid admin username")
	}
	if !passwordAcceptable(a.Password) {
		return dbCfg, fmt.Errorf("admin password must be 1-72 bytes")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(a.SiteName)); n == 0 || n > 60 { // 60：与设置页「站点名称」上限一致
		return dbCfg, fmt.Errorf("site name is required (at most 60 characters)")
	}
	if _, err := time.LoadLocation(a.Timezone); err != nil {
		return dbCfg, fmt.Errorf("invalid timezone %q", a.Timezone)
	}
	for _, l := range r.Cfg.Languages {
		if l.Code == a.Language {
			return dbCfg, nil
		}
	}
	return dbCfg, fmt.Errorf("unknown language %q", a.Language)
}

// overlay 生成要写入 config/config.yaml 的覆盖项。
func (r *Runner) overlay(req *executeReq, dbCfg config.Database) map[string]any {
	langs := make([]map[string]any, 0, len(r.Cfg.Languages))
	for _, l := range r.Cfg.Languages {
		m := map[string]any{"code": l.Code, "name": l.Name, "dir": l.Dir}
		if l.Code == req.Admin.Language {
			m["default"] = true
		}
		langs = append(langs, m)
	}
	cc := req.Cache.toConfig()
	out := map[string]any{
		"app":       map[string]any{"name": strings.TrimSpace(req.Admin.SiteName), "timezone": req.Admin.Timezone},
		"database":  map[string]any{"type": dbCfg.Type, "host": dbCfg.Host, "port": dbCfg.Port, "user": dbCfg.User, "password": dbCfg.Password, "name": dbCfg.Name},
		"cache":     map[string]any{"driver": cc.Driver, "addr": cc.Addr, "password": cc.Password},
		"languages": langs,
	}
	if st, _ := req.Optional.s3Config(); st != nil { // validate 已校验过；未选 S3 时不写 storage 段
		out["storage"] = st
	}
	if req.Optional.AIEnabled {
		out["ai"] = map[string]any{"base_url": req.Optional.AIBaseURL, "api_key": req.Optional.AIAPIKey, "model": req.Optional.AIModel}
	}
	return out
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		sm, sok := v.(map[string]any)
		dm, dok := dst[k].(map[string]any)
		if sok && dok {
			deepMerge(dm, sm)
			continue
		}
		dst[k] = v
	}
}

// writeConfig 把覆盖项合并进 config/config.yaml（权限 0600），返回回滚函数。
func writeConfig(path string, overlay map[string]any) (rollback func(), err error) {
	prev, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	cur := map[string]any{}
	if existed {
		if err := yaml.Unmarshal(prev, &cur); err != nil {
			return nil, fmt.Errorf("existing %s is not valid yaml: %w", path, err)
		}
	}
	deepMerge(cur, overlay)
	var buf bytes.Buffer
	buf.WriteString("# 由初始化向导生成；只写需要覆盖 config/default.yaml 的项。\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cur); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return func() {
		if existed {
			_ = os.WriteFile(path, prev, 0o600)
		} else {
			_ = os.Remove(path)
		}
	}, nil
}
