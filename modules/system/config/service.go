package config

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core"
	coreai "nexadmin/core/ai"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 封装参数与上传限制的数据访问与业务规则。
type Service struct {
	db  *gorm.DB
	log *zap.Logger // 可为空；仅用于设置页密钥解密失败的告警
	// notifier 用于设置页「发送测试」；可为空（单测里构造的 Service 不需要）。
	notifier *notify.Service

	// aiAllowPrivate 对应 ai.allow_private_base_url；aiResolver 仅供测试替换（nil = 系统解析器）。
	aiAllowPrivate bool
	aiResolver     coreai.Resolver

	// 文件模块每次上传都要读上传限制：这里缓存 uploadCacheTTL，本进程保存后立即失效，其他实例最多晚一个 TTL 生效。
	upMu    sync.Mutex
	upVal   UploadSettings
	upExp   time.Time
	upValid bool
}

// uploadCacheTTL 是上传限制的缓存时长（多实例时其他实例看到设置页修改的最大延迟）。
const uploadCacheTTL = 5 * time.Second

// invalidateUpload 丢弃上传限制缓存。参数增删改与上传限制保存后都要调用（通用参数 CRUD 也能改 upload.* 键）。
func (s *Service) invalidateUpload() {
	s.upMu.Lock()
	s.upValid = false
	s.upMu.Unlock()
}

// uploadLimits 返回带短缓存的上传限制，供文件模块通过 core.SetUploadLimitsSource 使用。读库失败不缓存，返回错误（文件模块回落 config.yaml）。
func (s *Service) uploadLimits(ctx context.Context) (core.UploadLimits, error) {
	s.upMu.Lock()
	if s.upValid && time.Now().Before(s.upExp) {
		v := s.upVal
		s.upMu.Unlock()
		return toCoreLimits(v), nil
	}
	s.upMu.Unlock()
	v, err := s.Upload(ctx)
	if err != nil {
		return core.UploadLimits{}, err
	}
	s.upMu.Lock()
	s.upVal, s.upExp, s.upValid = *v, time.Now().Add(uploadCacheTTL), true
	s.upMu.Unlock()
	return toCoreLimits(*v), nil
}

func toCoreLimits(u UploadSettings) core.UploadLimits {
	return core.UploadLimits{MaxImageMB: u.MaxImageMb, MaxVideoMB: u.MaxVideoMb, MaxFileMB: u.MaxFileMb, AllowedTypes: append([]string(nil), u.AllowedTypes...)}
}

// NewService 创建服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func bad(key string, args ...any) error { return response.BadRequest(key, args...) }

// badErr 是「用户能看懂的固定文案 + 只进日志的内部错误」：Wrap 的 err 不返回前端。
func badErr(key string, err error, args ...any) error {
	return response.BadRequest(key, args...).Wrap(err)
}

// validateValue 按参数类型校验值：number 必须是数字，boolean 必须是 true/false，json 必须是合法 JSON。
func validateValue(typ, v string) error {
	switch typ {
	case TypeNumber:
		if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
			return bad("config.value_not_number")
		}
	case TypeBoolean:
		if v != "true" && v != "false" {
			return bad("config.value_not_boolean")
		}
	case TypeJSON:
		if !json.Valid([]byte(v)) {
			return bad("config.value_not_json")
		}
	}
	return nil
}

// List 分页查询参数，按创建时间升序。
func (s *Service) List(ctx context.Context, q ListQuery, page, size int) ([]Config, int64, error) {
	db := s.db.WithContext(ctx).Model(&Config{})
	if kw := strings.ToLower(strings.TrimSpace(q.Keyword)); kw != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw) + "%"
		db = db.Where("LOWER(name) LIKE ? OR LOWER(config_key) LIKE ? OR LOWER(config_value) LIKE ?", like, like, like)
	}
	if q.Type != "" {
		db = db.Where("config_type IN ?", strings.Split(q.Type, ","))
	}
	switch q.IsPublic {
	case "true", "1":
		db = db.Where("is_public = ?", true)
	case "false", "0":
		db = db.Where("is_public = ?", false)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Config{}
	err := db.Order("created_at, id").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

// Get 读取单个参数。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*Config, error) {
	var c Config
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	return &c, nil
}

func (s *Service) keyTaken(ctx context.Context, key string, exclude idgen.ID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Config{}).Where("config_key = ? AND id <> ?", key, int64(exclude)).Count(&n).Error
	return n > 0, err
}

// Create 新增参数。
func (s *Service) Create(ctx context.Context, in Input) (*Config, error) {
	if err := validateValue(in.Type, in.Value); err != nil {
		return nil, err
	}
	if taken, err := s.keyTaken(ctx, in.Key, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, response.Conflict("config.key_exists")
	}
	c := &Config{Name: in.Name, Key: in.Key, Value: in.Value, Type: in.Type, IsPublic: in.IsPublic, Remark: in.Remark}
	err := s.db.WithContext(ctx).Create(c).Error
	s.invalidateUpload()
	return c, err
}

// Update 修改参数。
func (s *Service) Update(ctx context.Context, id idgen.ID, in Input) (*Config, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	if err := validateValue(in.Type, in.Value); err != nil {
		return nil, err
	}
	if taken, err := s.keyTaken(ctx, in.Key, id); err != nil {
		return nil, err
	} else if taken {
		return nil, response.Conflict("config.key_exists")
	}
	err := s.db.WithContext(ctx).Model(&Config{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"name": in.Name, "config_key": in.Key, "config_value": in.Value, "config_type": in.Type,
		"is_public": in.IsPublic, "remark": in.Remark,
	}).Error
	s.invalidateUpload()
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete 批量删除参数。
func (s *Service) Delete(ctx context.Context, ids []idgen.ID) error {
	raw := make([]int64, len(ids))
	for i, id := range ids {
		raw[i] = int64(id)
	}
	err := s.db.WithContext(ctx).Where("id IN ?", raw).Delete(&Config{}).Error
	s.invalidateUpload()
	return err
}

// ---- 上传限制 ----

func (s *Service) values(ctx context.Context, keys ...string) (map[string]string, error) {
	var rows []Config
	if err := s.db.WithContext(ctx).Where("config_key IN ?", keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return m, nil
}

// boolConfigValue 解析布尔类参数（user.register_enabled / login.captcha_enabled）的取值：
// 行不存在 / 值为空 / 读库失败时回退各自的兜底值。
func boolConfigValue(v string, present bool, fallback bool) bool {
	if !present || strings.TrimSpace(v) == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// registerEnabledSourceFn 返回自助注册的运行时开关：每次调用现读 sys_config，
// 后台参数页改完即刻生效，无需重启。
func (s *Service) registerEnabledSourceFn(fallback bool) func(context.Context) bool {
	return func(ctx context.Context) bool {
		m, err := s.values(tenant.Skip(ctx), "user.register_enabled")
		if err != nil {
			return fallback
		}
		v, ok := m["user.register_enabled"]
		return boolConfigValue(v, ok, fallback)
	}
}

// loginCaptchaSourceFn 返回「登录验证码」的运行时开关（出厂 true：密码登录必须先过图形验证码）。
// 行缺失 / 读库失败时回退 fallback（一般传 true，保持“默认开启”语义）。
func (s *Service) loginCaptchaSourceFn(fallback bool) func(context.Context) bool {
	return func(ctx context.Context) bool {
		m, err := s.values(tenant.Skip(ctx), "login.captcha_enabled")
		if err != nil {
			return fallback
		}
		v, ok := m["login.captcha_enabled"]
		return boolConfigValue(v, ok, fallback)
	}
}

// clamp 把非法（负数、NaN、Inf）限制值归零，0 表示不限制。
func clamp(f float64) float64 {
	if f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func num(v string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return clamp(f)
}

// Upload 读取上传限制。
func (s *Service) Upload(ctx context.Context) (*UploadSettings, error) {
	m, err := s.values(ctx, KeyUploadImage, KeyUploadVideo, KeyUploadFile, KeyUploadTypes, KeyUploadWebp)
	if err != nil {
		return nil, err
	}
	types := []string{}
	for _, t := range strings.Split(m[KeyUploadTypes], ",") {
		if t = strings.TrimSpace(t); t != "" {
			types = append(types, t)
		}
	}
	webp, hasWebp := m[KeyUploadWebp]
	return &UploadSettings{MaxImageMb: num(m[KeyUploadImage]), MaxVideoMb: num(m[KeyUploadVideo]),
		MaxFileMb: num(m[KeyUploadFile]), AllowedTypes: types, ImageWebp: boolConfigValue(webp, hasWebp, false)}, nil
}

// SaveUpload 保存上传限制，写回对应的四个参数行（缺失则补建）。
func (s *Service) SaveUpload(ctx context.Context, in UploadSettings) (*UploadSettings, error) {
	in.MaxImageMb, in.MaxVideoMb, in.MaxFileMb = clamp(in.MaxImageMb), clamp(in.MaxVideoMb), clamp(in.MaxFileMb)
	if in.AllowedTypes == nil {
		in.AllowedTypes = []string{}
	}
	items := []struct{ key, name, value, typ string }{
		{KeyUploadImage, "图片上传大小上限(MB)", strconv.FormatFloat(in.MaxImageMb, 'f', -1, 64), TypeNumber},
		{KeyUploadVideo, "视频上传大小上限(MB)", strconv.FormatFloat(in.MaxVideoMb, 'f', -1, 64), TypeNumber},
		{KeyUploadFile, "普通文件上传大小上限(MB)", strconv.FormatFloat(in.MaxFileMb, 'f', -1, 64), TypeNumber},
		{KeyUploadTypes, "允许上传的文件类型", strings.Join(in.AllowedTypes, ","), TypeString},
		{KeyUploadWebp, "为图片生成 WebP 副本", strconv.FormatBool(in.ImageWebp), TypeBoolean},
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, it := range items {
			res := tx.Model(&Config{}).Where("config_key = ?", it.key).Update("config_value", it.value)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 { // 没有这一行：补建（可能是值未变化，先确认是否存在）
				var n int64
				if err := tx.Model(&Config{}).Where("config_key = ?", it.key).Count(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					c := &Config{Name: it.name, Key: it.key, Value: it.value, Type: it.typ, IsPublic: true}
					if err := tx.Create(c).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	s.invalidateUpload()
	if err != nil {
		return nil, err
	}
	return &in, nil
}

// seedConfig 供 Seed 使用：按 key 幂等插入。
func seedConfig(db *gorm.DB, c *Config, now time.Time) error {
	var n int64
	if err := db.Model(&Config{}).Where("config_key = ?", c.Key).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	c.CreatedAt, c.UpdatedAt = now, now
	return db.Create(c).Error
}
