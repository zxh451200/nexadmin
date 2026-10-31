package region

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/region"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

const (
	snapshotID = 1
	// 数据源上限：完整三级树约 300KB，留足余量。
	maxSourceSize = 16 << 20
)

// busKind 是同步完成后通知其它实例重新装载快照的事件名。
const busKind = "region.snapshot"

// Status 是同步状态：当前生效的数据版本 + 默认数据源（前端用来预填输入框）。
type Status struct {
	region.Info
	DefaultSource string `json:"defaultSource"`
}

// Result 是一次同步的统计。
type Result struct {
	Info    region.Info `json:"info"`
	Added   int         `json:"added"`
	Removed int         `json:"removed"`
	Renamed int         `json:"renamed"`
}

// Service 负责同步快照的拉取、校验、落库与装配。
type Service struct {
	db     *gorm.DB
	log    *zap.Logger
	client *http.Client
	// sourceURL / sourceLicense 来自 config.yaml 的 region.*，不硬编码在代码里。
	sourceURL     string
	sourceLicense string
}

// NewService 构造服务；日志由模块装配时补上。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db, client: newSourceClient()}
}

// Status 返回当前生效的数据版本。
func (s *Service) Status() Status {
	return Status{Info: region.Current(), DefaultSource: s.sourceURL}
}

// Load 把库里保存的快照装进内存；没有记录或数据不可用时保持内置数据。
func (s *Service) Load(ctx context.Context) {
	if s.db == nil {
		return
	}
	var row Snapshot
	err := s.db.WithContext(ctx).Where("id = ?", idgen.ID(snapshotID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		region.Reset()
		return
	}
	if err != nil {
		s.warn("region: 读取省市区快照失败，继续使用内置数据", err)
		return
	}
	if _, err := region.Activate([]byte(row.Payload), row.info()); err != nil {
		s.warn("region: 已保存的省市区快照不可用，改用内置数据", err)
		region.Reset()
	}
}

// Sync 拉取 source（空则用上次的来源或默认来源），校验通过后落库并立即生效。
func (s *Service) Sync(ctx context.Context, source, actor string) (Result, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = s.source(ctx)
	}
	if source == "" {
		return Result{}, response.BadRequest("region.source_missing")
	}
	raw, lastModified, err := s.fetch(ctx, source)
	if err != nil {
		return Result{}, err
	}
	tree, err := parseSource(raw)
	if err != nil {
		return Result{}, err
	}
	meta := region.Info{
		SourceURL: source,
		DataDate:  dateFrom(lastModified),
		License:   s.licenseFor(source),
		SyncedBy:  actor,
		SyncedAt:  time.Now().Format(time.RFC3339),
	}
	info, err := region.Validate(tree, meta)
	if err != nil {
		return Result{}, response.BadRequest("region.data_invalid", err.Error())
	}
	before, err := region.Tree(3)
	if err != nil {
		return Result{}, err
	}
	added, removed, renamed := diffNames(before, tree)
	row := Snapshot{
		SourceURL:   source,
		DataDate:    meta.DataDate,
		License:     meta.License,
		Payload:     string(tree),
		SHA256:      info.SHA256,
		Provinces:   info.Provinces,
		Prefectures: info.Prefectures,
		Counties:    info.Counties,
		SyncedBy:    actor,
		SyncedAt:    time.Now(),
	}
	if err := s.save(ctx, row); err != nil {
		return Result{}, err
	}
	if _, err := region.Activate(tree, meta); err != nil {
		return Result{}, response.Internal().Wrap(err)
	}
	if s.log != nil {
		// 每次同步都记一条，包含「检查了多少、变了几条」，便于排障时区分空跑与已跳过
		s.log.Info("region: 省市区数据已同步",
			zap.String("source", source), zap.String("actor", actor), zap.String("sha256", info.SHA256),
			zap.Int("provinces", info.Provinces), zap.Int("prefectures", info.Prefectures), zap.Int("counties", info.Counties),
			zap.Int("added", added), zap.Int("removed", removed), zap.Int("renamed", renamed))
	}
	return Result{Info: region.Current(), Added: added, Removed: removed, Renamed: renamed}, nil
}

// Reset 删除快照并退回内置数据。
func (s *Service) Reset(ctx context.Context) (region.Info, error) {
	if s.db != nil {
		if err := s.db.WithContext(ctx).Where("id = ?", idgen.ID(snapshotID)).Delete(&Snapshot{}).Error; err != nil {
			return region.Info{}, response.Internal().Wrap(err)
		}
	}
	info := region.Reset()
	if s.log != nil {
		s.log.Info("region: 已恢复内置省市区数据")
	}
	return info, nil
}

func (s *Service) save(ctx context.Context, row Snapshot) error {
	db := s.db.WithContext(ctx)
	res := db.Model(&Snapshot{}).Where("id = ?", idgen.ID(snapshotID)).Updates(map[string]any{
		"source_url":  row.SourceURL,
		"data_date":   row.DataDate,
		"license":     row.License,
		"payload":     row.Payload,
		"sha256":      row.SHA256,
		"provinces":   row.Provinces,
		"prefectures": row.Prefectures,
		"counties":    row.Counties,
		"synced_by":   row.SyncedBy,
		"synced_at":   row.SyncedAt,
	})
	if res.Error != nil {
		return response.Internal().Wrap(res.Error)
	}
	if res.RowsAffected == 0 {
		row.ID = idgen.ID(snapshotID)
		if err := db.Create(&row).Error; err != nil {
			return response.Internal().Wrap(err)
		}
	}
	return nil
}

func (s *Service) source(ctx context.Context) string {
	if s.db != nil {
		var row Snapshot
		if err := s.db.WithContext(ctx).Select("source_url").Where("id = ?", idgen.ID(snapshotID)).First(&row).Error; err == nil && row.SourceURL != "" {
			return row.SourceURL
		}
	}
	return s.sourceURL
}

func (s *Service) warn(msg string, err error) {
	if s.log != nil {
		s.log.Warn(msg, zap.Error(err))
	}
}

func (row Snapshot) info() region.Info {
	syncedAt := ""
	if !row.SyncedAt.IsZero() {
		syncedAt = row.SyncedAt.Format(time.RFC3339)
	}
	return region.Info{
		SourceURL: row.SourceURL,
		DataDate:  row.DataDate,
		License:   row.License,
		SyncedBy:  row.SyncedBy,
		SyncedAt:  syncedAt,
	}
}

// fetch 拉取数据源；只放行 HTTPS，且连接阶段拒绝内网/本机地址（含重定向后的每一跳）。
func (s *Service) fetch(ctx context.Context, source string) ([]byte, string, error) {
	if err := checkSourceURL(source); err != nil {
		// 文案里只放地址本身，具体原因进日志，避免出现「必须是 HTTPS：必须是 HTTPS」这种重复
		return nil, "", response.BadRequest("region.source_insecure", source).Wrap(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, "", response.BadRequest("region.source_insecure", source).Wrap(err)
	}
	req.Header.Set("User-Agent", "nexadmin-region-sync")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", response.BadRequest("region.source_unreachable", err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", response.BadRequest("region.source_unreachable", fmt.Sprintf("%s %d", source, resp.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceSize+1))
	if err != nil {
		return nil, "", response.BadRequest("region.source_unreachable", err.Error())
	}
	if len(raw) > maxSourceSize {
		return nil, "", response.BadRequest("region.source_too_large", maxSourceSize>>20)
	}
	return raw, resp.Header.Get("Last-Modified"), nil
}

func checkSourceURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return errors.New("数据源必须是 HTTPS 地址")
	}
	if u.Hostname() == "" || u.User != nil {
		return errors.New("数据源地址不合法")
	}
	return nil
}

// newSourceClient 的拨号阶段做地址校验：DNS 解析到内网/本机一律拒绝，防止数据源被指向内网。
// 变量形式便于测试注入带自签证书的 httptest 客户端。
var newSourceClient = func() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !publicIP(ip.IP) {
					return nil, fmt.Errorf("%s 解析到内网或本机地址 %s", host, ip.IP)
				}
			}
			var lastErr error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("%s 没有可用地址", host)
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("重定向次数过多")
			}
			return checkSourceURL(req.URL.String())
		},
	}
}
func publicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

// parseSource 接受两种公开格式：code→name 的扁平对象，或已有的三级树数组。
func parseSource(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, response.BadRequest("region.data_invalid", "数据源为空")
	}
	switch trimmed[0] {
	case '{':
		var flat map[string]string
		if err := json.Unmarshal(trimmed, &flat); err != nil || len(flat) == 0 {
			return nil, response.BadRequest("region.data_invalid", "扁平格式应为 code→name 的对象")
		}
		tree, err := region.FromFlat(flat)
		if err != nil {
			return nil, response.BadRequest("region.data_invalid", err.Error())
		}
		return tree, nil
	case '[':
		var nodes []*region.Node
		if err := json.Unmarshal(trimmed, &nodes); err != nil || len(nodes) == 0 {
			return nil, response.BadRequest("region.data_invalid", "树格式应为节点数组")
		}
		tree, err := region.Encode(nodes)
		if err != nil {
			return nil, response.BadRequest("region.data_invalid", err.Error())
		}
		return tree, nil
	}
	return nil, response.BadRequest("region.data_invalid", "数据源应是 JSON 对象或数组")
}

func diffNames(before []*region.Node, raw []byte) (added, removed, renamed int) {
	var after []*region.Node
	if err := json.Unmarshal(raw, &after); err != nil {
		return 0, 0, 0
	}
	oldNames := map[string]string{}
	var walkOld func(list []*region.Node)
	walkOld = func(list []*region.Node) {
		for _, n := range list {
			oldNames[n.Code] = n.Name
			walkOld(n.Children)
		}
	}
	walkOld(before)
	newNames := map[string]string{}
	var walkNew func(list []*region.Node)
	walkNew = func(list []*region.Node) {
		for _, n := range list {
			newNames[n.Code] = n.Name
			walkNew(n.Children)
		}
	}
	walkNew(after)
	for code, name := range newNames {
		old, ok := oldNames[code]
		if !ok {
			added++
			continue
		}
		if old != name {
			renamed++
		}
	}
	for code := range oldNames {
		if _, ok := newNames[code]; !ok {
			removed++
		}
	}
	return added, removed, renamed
}

// licenseFor 只给配置里的默认数据源标注许可证；管理员临时换成别的地址时不猜，留空由页面显示「—」。
func (s *Service) licenseFor(source string) string {
	if s.sourceURL != "" && strings.TrimSpace(source) == s.sourceURL {
		return s.sourceLicense
	}
	return ""
}

func dateFrom(lastModified string) string {
	if t, err := http.ParseTime(lastModified); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	return ""
}
