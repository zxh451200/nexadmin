package realname

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/datascope"
	"nexadmin/core/region"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idcard"
	"nexadmin/pkg/mask"
)

const (
	// 同一「姓名+号码」的在线核验结论缓存一天，避免重复提交时反复扣费。
	verifyCacheTTL = 24 * time.Hour
	// 每日额度窗口：按租户累计外部调用次数。
	quotaWindow = 24 * time.Hour

	minTimeoutMs = 1000
	maxTimeoutMs = 15000
	defTimeoutMs = 8000
	defDailyLen  = 200

	maxNameLen   = 50
	maxRemarkLen = 200

	// secretMask 是前端回显的占位值：留空或原样回传都表示沿用已存密钥。
	secretMask = "********"
)

type Service struct {
	db    *gorm.DB
	cache cache.Cache
	log   *zap.Logger
	// httpClient 是出网核验用的客户端覆盖位：生产留 nil（按配置超时新建），
	// 测试注入自己的 client 以指向假上游。
	httpClient *http.Client
}

func NewService(db *gorm.DB, c cache.Cache, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, cache: c, log: log}
}

// ParseView 是号码解析结果。号码只以脱敏形式回传，明文不出服务层。
type ParseView struct {
	Status           string   `json:"status"`
	Reason           string   `json:"reason,omitempty"`
	NumberMasked     string   `json:"numberMasked"`
	Legacy           bool     `json:"legacy"`
	RegionCode       string   `json:"regionCode,omitempty"`
	RegionNames      []string `json:"regionNames,omitempty"`
	RegionHistorical bool     `json:"regionHistorical"`
	Birthday         string   `json:"birthday,omitempty"`
	Sex              string   `json:"sex,omitempty"`
	Age              int      `json:"age"`
}

// Parse 校验号码并解析自带信息：不落库、不记审计、不出网。
func (s *Service) Parse(idType, number string) (*ParseView, error) {
	if err := checkIdType(idType); err != nil {
		return nil, err
	}
	info, err := idcard.Parse(number, lookupRegion)
	if err != nil {
		return &ParseView{
			Status:       StatusInvalid,
			Reason:       string(idcard.ReasonOf(err)),
			NumberMasked: mask.IDCard(idcard.Normalize(number)),
		}, nil
	}
	names, historical := regionTrail(info.RegionCode)
	return &ParseView{
		Status:           StatusValid,
		NumberMasked:     mask.IDCard(info.Number),
		Legacy:           info.Legacy,
		RegionCode:       info.RegionCode,
		RegionNames:      names,
		RegionHistorical: historical,
		Birthday:         info.Birthday.Format("2006-01-02"),
		Sex:              info.Sex,
		Age:              info.AgeAt(time.Now().UTC()),
	}, nil
}

// VerifyInput 是一次核验的输入。Number/Name 是明文，只在出网调用那一瞬存在。
type VerifyInput struct {
	IdType string
	Number string
	Name   string
	Remark string
}

// VerifyResult 是核验响应：台账行 + 号码解析 + 是否真的走了在线核验。
type VerifyResult struct {
	Verification *Verification `json:"verification"`
	Parse        *ParseView    `json:"parse"`
	Online       bool          `json:"online"`
}

// Verify 校验号码，按配置尝试在线核验，并把结论写进台账。
// 号码不合规时不调用外部服务（省一次钱，也避免把明显错误的输入送给厂商）。
func (s *Service) Verify(ctx context.Context, in VerifyInput) (*VerifyResult, error) {
	return s.verify(ctx, in, nil)
}

// verify 的 cfgOverride 仅供测试注入假上游配置；nil 表示按当前租户的配置读取。
func (s *Service) verify(ctx context.Context, in VerifyInput, cfgOverride *Config) (*VerifyResult, error) {
	if err := checkIdType(in.IdType); err != nil {
		return nil, err
	}
	name := truncateRunes(strings.TrimSpace(in.Name), maxNameLen)
	if name == "" {
		return nil, response.BadRequest("realname.name_required")
	}
	number := idcard.Normalize(in.Number)

	row := &Verification{
		IdType:       IdTypeIDCard,
		NameMasked:   mask.Name(name),
		NumberMasked: mask.IDCard(number),
		NumberHash:   numberHash(number),
		Remark:       truncateRunes(strings.TrimSpace(in.Remark), maxRemarkLen),
	}
	if cl := auth.ClaimsFromContext(ctx); cl != nil {
		row.OperatorID = cl.UserID
		row.OperatorName = truncateRunes(cl.Username, 64)
	}

	parse, err := s.Parse(in.IdType, in.Number)
	if err != nil {
		return nil, err
	}
	row.Status = parse.Status
	row.Reason = parse.Reason
	if row.Status == StatusInvalid {
		return &VerifyResult{Verification: row, Parse: parse}, s.record(ctx, row)
	}

	cfg := cfgOverride
	if cfg == nil {
		var err error
		if cfg, err = s.config(ctx); err != nil {
			return nil, err
		}
	}
	prov, err := newProvider(cfg, s.httpClient)
	if err != nil {
		row.Status = StatusError
		row.Reason = ReasonProviderError
		row.ProviderMsg = truncateRunes(err.Error(), 200)
		if perr := s.record(ctx, row); perr != nil {
			return nil, perr
		}
		return &VerifyResult{Verification: row, Parse: parse}, nil
	}
	if prov == nil {
		// 在线核验未启用：结论停在「格式通过」，入口和留痕都在，配置好密钥即可升级成 match/mismatch。
		if perr := s.record(ctx, row); perr != nil {
			return nil, perr
		}
		return &VerifyResult{Verification: row, Parse: parse}, nil
	}
	row.Provider = cfg.Provider

	if over, err := s.quotaExceeded(ctx, cfg.DailyLimit); err != nil {
		return nil, err
	} else if over {
		row.Status = StatusError
		row.Reason = ReasonDailyLimit
		if perr := s.record(ctx, row); perr != nil {
			return nil, perr
		}
		return &VerifyResult{Verification: row, Parse: parse}, nil
	}

	key := verifyCacheKey(ctx, name, number)
	if cached, ok := s.cacheLoad(ctx, key); ok {
		row.Status = cached.Status
		row.Reason = cached.Reason
		row.ProviderMsg = cached.Message
		if perr := s.record(ctx, row); perr != nil {
			return nil, perr
		}
		return &VerifyResult{Verification: row, Parse: parse, Online: true}, nil
	}

	start := time.Now()
	outcome, err := prov.Verify(ctx, VerifyRequest{Name: name, Number: number, IdType: IdTypeIDCard})
	row.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		row.Status = StatusError
		row.Reason = ReasonProviderError
		row.ProviderMsg = truncateRunes(err.Error(), 200)
		s.log.Warn("realname: 在线核验调用失败", zap.String("provider", cfg.Provider), zap.Error(err))
	} else {
		row.Status = StatusMismatch
		if outcome.Matched {
			row.Status = StatusMatch
		}
		row.ProviderMsg = truncateRunes(outcome.Message, 200)
		s.cacheStore(ctx, key, row.Status, row.ProviderMsg)
	}

	if perr := s.record(ctx, row); perr != nil {
		return nil, perr
	}
	return &VerifyResult{Verification: row, Parse: parse, Online: true}, nil
}

func (s *Service) record(ctx context.Context, row *Verification) error {
	return s.db.WithContext(ctx).Create(row).Error
}

// ListQuery 是台账筛选条件。
type ListQuery struct {
	Keyword  string
	Status   string
	Scope    *datascope.Scope
	Page     int
	PageSize int
}

// ListVerifications 按数据范围裁剪台账：受限账号只看自己发起的核验。
func (s *Service) ListVerifications(ctx context.Context, q ListQuery) ([]Verification, int64, error) {
	if q.Status != "" && !StatusSet[q.Status] {
		return nil, 0, response.BadRequest("realname.status_invalid")
	}
	tx := s.db.WithContext(ctx).Model(&Verification{})
	if q.Scope != nil {
		if cond, args, limited := q.Scope.UserIDCond("operator_id"); limited {
			tx = tx.Where(cond, args...)
		}
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("name_masked LIKE ? OR number_masked LIKE ? OR remark LIKE ?", like, like, like)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	offset := (q.Page - 1) * size
	if offset < 0 {
		offset = 0
	}
	var rows []Verification
	if err := tx.Order("id DESC").Offset(offset).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ConfigView 是配置回显：密钥只回「有没有」，真值不下发浏览器。
type ConfigView struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	AppKey     string `json:"appKey"`
	HasSecret  bool   `json:"hasSecret"`
	TimeoutMs  int    `json:"timeoutMs"`
	DailyLimit int    `json:"dailyLimit"`
}

// ConfigInput 是配置写入。AppSecret 留空或等于占位值时沿用已存密钥。
type ConfigInput struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	AppKey     string `json:"appKey"`
	AppSecret  string `json:"appSecret"`
	TimeoutMs  int    `json:"timeoutMs"`
	DailyLimit int    `json:"dailyLimit"`
}

func (s *Service) GetConfig(ctx context.Context) (*ConfigView, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return nil, err
	}
	return cfg.view(), nil
}

// SaveConfig 落库配置。开启在线核验前必须把 provider 与 https 地址配齐，
// 否则开关打开就是运行期 502，不如在保存时就拒绝。
func (s *Service) SaveConfig(ctx context.Context, in ConfigInput) (*ConfigView, error) {
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider != "" && !providerNames[provider] {
		return nil, response.BadRequest("realname.provider_unknown")
	}
	cur, exists, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}

	next := *cur
	next.Enabled = in.Enabled
	next.Provider = provider
	next.Endpoint = strings.TrimSpace(in.Endpoint)
	next.AppKey = strings.TrimSpace(in.AppKey)
	next.TimeoutMs = in.TimeoutMs
	next.DailyLimit = in.DailyLimit
	next.normalize()

	secretChanged := false
	switch newSecret := strings.TrimSpace(in.AppSecret); newSecret {
	case "", secretMask:
	default:
		enc, err := secretEncrypt(newSecret)
		if err != nil {
			return nil, err
		}
		next.AppSecretEnc = enc
		secretChanged = true
	}

	if next.Enabled {
		if next.Provider == "" {
			return nil, response.BadRequest("realname.provider_required")
		}
		if err := validateEndpoint(next.Endpoint); err != nil {
			return nil, err
		}
		if next.AppKey == "" && next.AppSecretEnc == "" {
			return nil, response.BadRequest("realname.credentials_required")
		}
	}

	updates := map[string]any{
		"enabled":     next.Enabled,
		"provider":    next.Provider,
		"endpoint":    next.Endpoint,
		"app_key":     next.AppKey,
		"timeout_ms":  next.TimeoutMs,
		"daily_limit": next.DailyLimit,
	}
	if secretChanged {
		updates["app_secret_enc"] = next.AppSecretEnc
	}

	if exists {
		if err := s.db.WithContext(ctx).Model(&Config{}).Where("id = ?", cur.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
	} else {
		row := &Config{
			Enabled: next.Enabled, Provider: next.Provider, Endpoint: next.Endpoint,
			AppKey: next.AppKey, AppSecretEnc: next.AppSecretEnc,
			TimeoutMs: next.TimeoutMs, DailyLimit: next.DailyLimit,
		}
		if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
			return nil, err
		}
	}
	return next.view(), nil
}

func (c *Config) view() *ConfigView {
	return &ConfigView{
		Enabled: c.Enabled, Provider: c.Provider, Endpoint: c.Endpoint, AppKey: c.AppKey,
		HasSecret: c.AppSecretEnc != "", TimeoutMs: c.TimeoutMs, DailyLimit: c.DailyLimit,
	}
}

// config 读取当前租户配置；不存在时返回默认值（在线核验关闭）。
// 返回值的 AppSecretEnc 已被替换成明文，只用于出网调用，禁止序列化或再次落库。
func (s *Service) config(ctx context.Context) (*Config, error) {
	cfg, _, err := s.loadConfig(ctx)
	return cfg, err
}

func (s *Service) loadConfig(ctx context.Context) (*Config, bool, error) {
	cfg := defaultConfig()
	var row Config
	err := s.db.WithContext(ctx).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cfg, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	plain, err := secretDecrypt(row.AppSecretEnc)
	if err != nil {
		// 解不开密钥不该让配置页 500：按未配置处理，开关自然回落为不可用。
		s.log.Warn("realname: 解密核验密钥失败", zap.Error(err))
		row.AppSecretEnc = ""
	} else {
		row.AppSecretEnc = plain
	}
	row.normalize()
	return &row, true, nil
}

func defaultConfig() *Config {
	return &Config{Provider: "", Enabled: false, TimeoutMs: defTimeoutMs, DailyLimit: defDailyLen}
}

func (c *Config) normalize() {
	if c.TimeoutMs < minTimeoutMs {
		c.TimeoutMs = defTimeoutMs
	}
	if c.TimeoutMs > maxTimeoutMs {
		c.TimeoutMs = maxTimeoutMs
	}
	if c.DailyLimit < 0 {
		c.DailyLimit = 0
	}
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
}

// quotaExceeded 统计当天本租户的外部调用次数；limit<=0 表示不限额。
func (s *Service) quotaExceeded(ctx context.Context, limit int) (bool, error) {
	if limit <= 0 || s.cache == nil {
		return false, nil
	}
	key := cache.GlobalKey("realname", "quota", quotaTenant(ctx), time.Now().UTC().Format("20060102"))
	n, err := cache.Incr(s.cache, ctx, key, quotaWindow)
	if err != nil {
		return false, err
	}
	return n > int64(limit), nil
}

func verifyCacheKey(ctx context.Context, name, number string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(name) + "|" + number))
	return cache.GlobalKey("realname", "verify", quotaTenant(ctx), hex.EncodeToString(sum[:16]))
}

// quotaTenant 给配额与缓存 key 取租户标识；没有租户上下文时归到 "0"。
func quotaTenant(ctx context.Context) string {
	if id, ok := tenant.From(ctx); ok {
		return id.String()
	}
	return "0"
}

type cachedOutcome struct {
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *Service) cacheLoad(ctx context.Context, key string) (cachedOutcome, bool) {
	if s.cache == nil {
		return cachedOutcome{}, false
	}
	raw, err := s.cache.Get(ctx, key)
	if err != nil || raw == "" {
		return cachedOutcome{}, false
	}
	var out cachedOutcome
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Status == "" {
		return cachedOutcome{}, false
	}
	return out, true
}

func (s *Service) cacheStore(ctx context.Context, key, status, message string) {
	if s.cache == nil || (status != StatusMatch && status != StatusMismatch) {
		return // 失败结论不缓存：下一次请求应当真的重试。
	}
	raw, err := json.Marshal(cachedOutcome{Status: status, Message: message})
	if err != nil {
		return
	}
	_ = s.cache.Set(ctx, key, string(raw), verifyCacheTTL)
}

// lookupRegion 现行码优先，其次已撤销的历史码：老身份证的区划大多改过名或撤并，
// 只认现行表会把合法号码判成非法。
func lookupRegion(code string) (string, bool) {
	if name, ok := region.Name(code); ok {
		return name, true
	}
	if h, ok := region.HistoricalInfo(code); ok {
		return h.Name, true
	}
	return "", false
}

// regionTrail 把 6 位区划码拆成省/市/县名称；同名层级合并（直辖市、不设区的市）。
func regionTrail(code string) (names []string, historical bool) {
	if len(code) != 6 {
		return nil, false
	}
	_, historical = region.HistoricalInfo(code)
	seen := map[string]bool{}
	for _, c := range []string{code[:2] + "0000", code[:4] + "0000", code} {
		name, ok := region.Name(c)
		if !ok {
			if h, isHistorical := region.HistoricalInfo(c); isHistorical {
				name, ok = h.Name, true
			}
		}
		if ok && name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, historical
}

func checkIdType(idType string) error {
	switch strings.ToLower(strings.TrimSpace(idType)) {
	case "", IdTypeIDCard:
		return nil
	default:
		return response.BadRequest("realname.id_type_unsupported")
	}
}

// numberHash 是号码的 sha256 摘要：用于回溯同一号码的核验记录，且永远不可逆推出号码。
func numberHash(number string) string {
	sum := sha256.Sum256([]byte(number))
	return hex.EncodeToString(sum[:])
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func secretEncrypt(plain string) (string, error) {
	if secret.Default() == nil {
		return plain, nil
	}
	return secret.Encrypt(plain)
}

// secretDecrypt 兼容历史明文值：无前缀时原样返回。
func secretDecrypt(enc string) (string, error) {
	if enc == "" || secret.Default() == nil || !secret.IsEncrypted(enc) {
		return enc, nil
	}
	return secret.Decrypt(enc)
}
