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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	// Preview = true 表示这是 dry-run 结果：数据未生效，样本字段才有值。
	Preview        bool         `json:"preview,omitempty"`
	AddedSamples   []DiffSample `json:"addedSamples,omitempty"`
	RemovedSamples []DiffSample `json:"removedSamples,omitempty"`
	RenamedSamples []DiffSample `json:"renamedSamples,omitempty"`
}

// DiffSample 是预览时的一条变更样本（每类最多 maxDiffSamples 条，防大 diff 撑爆响应）。
type DiffSample struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Old 只有改名样本有：原名称。
	Old string `json:"old,omitempty"`
}

// maxDiffSamples 是每类变更样本的上限：预览只给「长什么样」，全量清单没意义。
const maxDiffSamples = 20

// maxVersions 是版本列表一次返回的条数上限：历史用于回看与回滚，最早的版本留在库里。
const maxVersions = 50

// VersionView 是一条历史版本的展示行：不带整棵树 payload（列表里塞 300KB 的 JSON 没有意义），
// 正文另走 GetVersion。
type VersionView struct {
	Version     int       `json:"version"`
	SHA256      string    `json:"sha256"`
	DataDate    string    `json:"dataDate"`
	SourceURL   string    `json:"sourceUrl"`
	Provinces   int       `json:"provinces"`
	Prefectures int       `json:"prefectures"`
	Counties    int       `json:"counties"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

// VersionDetail 是单个版本的完整记录：展示行之上再加整棵树，前端只用它算省级摘要。
type VersionDetail struct {
	VersionView
	Payload string `json:"payload"`
}

func (v SnapshotVersion) view() VersionView {
	return VersionView{
		Version: v.Version, SHA256: v.SHA256, DataDate: v.DataDate, SourceURL: v.SourceURL,
		Provinces: v.Provinces, Prefectures: v.Prefectures, Counties: v.Counties,
		CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt,
	}
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
// 后台执行：立即返回，进度与结果走 SyncStatus 轮询（源最大 16MB，慢源不再卡住请求）。
func (s *Service) Sync(ctx context.Context, source, actor string) error {
	return s.StartSync(ctx, source, actor, nil)
}

// syncState 是后台同步任务的状态：前端轮询 GET /regions/sync/status 消费。
type syncState struct {
	Running bool    `json:"running"`
	Stage   string  `json:"stage,omitempty"` // fetching | validating
	Result  *Result `json:"result,omitempty"`
	Error   string  `json:"error,omitempty"`
}

// 后台同步全局串行（内存快照的 Activate 不支持多写者并发生效）；状态指针整体替换，读侧无锁。
var (
	syncMu  sync.Mutex
	syncJob atomic.Pointer[syncState]
)

// syncRunning 是「后台同步在跑」的当前判定：回滚改的是同一份内存快照，
// 有同步在跑时回滚一律当场 409，否则回滚的结果会被在跑的同步覆盖掉。
func syncRunning() bool {
	st := syncJob.Load()
	return st != nil && st.Running
}

// StartSync 把一次同步放到后台协程执行并立即返回；已有任务在跑时返回 409。
// 廉价校验（来源缺失、明文地址）在提交前同步完成：明显的输入错误当场报 400，
// 不让用户等轮询；慢的部分（拉取 16MB、校验、落库）才是后台化的意义。
// onSynced 在成功落库并生效后回调（广播其它实例重装快照），由 handler 注入。
func (s *Service) StartSync(ctx context.Context, source, actor string, onSynced func()) error {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncRunning() {
		return response.Conflict("region.sync_running")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = s.source(ctx)
	}
	if source == "" {
		return response.BadRequest("region.source_missing")
	}
	if err := checkSourceURL(source); err != nil {
		return response.BadRequest("region.source_insecure", source).Wrap(err)
	}
	syncJob.Store(&syncState{Running: true, Stage: "fetching"})
	// 后台任务不复用请求 ctx（请求返回即取消）；进程退出随进程消亡，状态自然丢弃。
	go func() {
		res, err := s.sync(context.Background(), source, actor, true)
		if err != nil {
			// 错误文案：response.Error 的本地化文案需要请求 ctx；后台任务取不到就用原始文本。
			syncJob.Store(&syncState{Error: err.Error()})
			return
		}
		syncJob.Store(&syncState{Result: &res})
		if onSynced != nil {
			onSynced()
		}
	}()
	return nil
}

func (s *Service) storeSyncStage(stage string) {
	syncJob.Store(&syncState{Running: true, Stage: stage})
}

// SyncStatus 返回当前后台同步状态（从未跑过时是零值：not running、无结果）。
func (s *Service) SyncStatus() *syncState {
	if p := syncJob.Load(); p != nil {
		return p
	}
	return &syncState{}
}

// Preview 与 Sync 同一条校验链路，但不落库不生效：先看这次同步会改什么，再决定要不要应用。
func (s *Service) Preview(ctx context.Context, source string) (Result, error) {
	return s.sync(ctx, source, "", false)
}

func (s *Service) sync(ctx context.Context, source, actor string, apply bool) (Result, error) {
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
	return s.applyData(ctx, raw, source, lastModified, actor, apply)
}

// ImportFile 处理手动上传的数据文件（离线 / 内网环境没有可拉取的 HTTPS URL）：
// 与 URL 同步走同一条解析/校验/落库链路，来源记录为 "upload"。
func (s *Service) ImportFile(ctx context.Context, raw []byte, actor string, apply bool) (Result, error) {
	if len(raw) == 0 {
		return Result{}, response.BadRequest("region.source_missing")
	}
	return s.applyData(ctx, raw, "upload", "", actor, apply)
}

func (s *Service) applyData(ctx context.Context, raw []byte, source, lastModified, actor string, apply bool) (Result, error) {
	meta := region.Info{
		SourceURL: source,
		DataDate:  dateFrom(lastModified),
		License:   s.licenseFor(source),
		SyncedBy:  actor,
		SyncedAt:  time.Now().Format(time.RFC3339),
	}
	return s.apply(ctx, raw, meta, apply)
}

// apply 是「解析 → 校验 → 与当前数据比对 → 留档并落库 → 生效」这条唯一链路：
// 在线同步、文件导入和版本回滚都从这里过，回滚不会因为少校验一步而写进坏数据。
func (s *Service) apply(ctx context.Context, raw []byte, meta region.Info, apply bool) (Result, error) {
	tree, err := parseSource(raw)
	if err != nil {
		return Result{}, err
	}
	info, err := region.Validate(tree, meta)
	if err != nil {
		return Result{}, response.BadRequest("region.data_invalid", err.Error())
	}
	before, err := region.Tree(3)
	if err != nil {
		return Result{}, err
	}
	added, removed, renamed, addedS, removedS, renamedS := diffTree(before, tree)
	if !apply {
		// 预览只回报差异与样本，不写库、不切换内存数据。
		return Result{Preview: true, Added: added, Removed: removed, Renamed: renamed,
			AddedSamples: addedS, RemovedSamples: removedS, RenamedSamples: renamedS}, nil
	}
	row := Snapshot{
		SourceURL:   meta.SourceURL,
		DataDate:    meta.DataDate,
		License:     meta.License,
		Payload:     string(tree),
		SHA256:      info.SHA256,
		Provinces:   info.Provinces,
		Prefectures: info.Prefectures,
		Counties:    info.Counties,
		SyncedBy:    meta.SyncedBy,
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
			zap.String("source", meta.SourceURL), zap.String("actor", meta.SyncedBy), zap.String("sha256", info.SHA256),
			zap.Int("provinces", info.Provinces), zap.Int("prefectures", info.Prefectures), zap.Int("counties", info.Counties),
			zap.Int("added", added), zap.Int("removed", removed), zap.Int("renamed", renamed))
	}
	return Result{Info: region.Current(), Added: added, Removed: removed, Renamed: renamed}, nil
}

// Reset 删除快照并退回内置数据：历史版本表不动，且删除前先把当前快照存成一个版本，
// 所以「恢复内置数据」之后还能从版本历史一键回滚回来。
func (s *Service) Reset(ctx context.Context, actor string) (region.Info, error) {
	if s.db != nil {
		db := s.db.WithContext(ctx)
		if err := db.Transaction(func(tx *gorm.DB) error {
			// 没有「即将写入的内容」可比对：恢复内置一定让当前快照留一个版本。
			if err := archiveCurrent(ctx, tx, ""); err != nil {
				return err
			}
			if err := tx.Where("id = ?", idgen.ID(snapshotID)).Delete(&Snapshot{}).Error; err != nil {
				return response.Internal().Wrap(err)
			}
			return nil
		}); err != nil {
			return region.Info{}, err
		}
	}
	info := region.Reset()
	if s.log != nil {
		s.log.Info("region: 已恢复内置省市区数据", zap.String("actor", actor))
	}
	return info, nil
}

// archiveCurrent 在覆盖 sys_region_snapshot 之前把被覆盖的旧快照存成一个版本：
// 库里没有当前行（首次同步、或刚恢复过内置）就没有旧内容可存；
// nextSHA 与当前行摘要相同说明这次写入没改内容（例如重复同步同一个源），不重复留档。
// 版本号取库里现有最大值 +1：行数与列表下标都不能当版本号，删过行就会撞号。
func archiveCurrent(ctx context.Context, tx *gorm.DB, nextSHA string) error {
	var cur Snapshot
	err := tx.WithContext(ctx).Where("id = ?", idgen.ID(snapshotID)).First(&cur).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return response.Internal().Wrap(err)
	}
	if cur.SHA256 == nextSHA {
		return nil
	}
	var maxVersion int
	if err := tx.WithContext(ctx).Model(&SnapshotVersion{}).
		Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return response.Internal().Wrap(err)
	}
	version := SnapshotVersion{
		Version: maxVersion + 1, Payload: cur.Payload, SHA256: cur.SHA256, DataDate: cur.DataDate,
		SourceURL: cur.SourceURL, Provinces: cur.Provinces, Prefectures: cur.Prefectures,
		Counties: cur.Counties, CreatedBy: cur.SyncedBy,
	}
	if err := tx.WithContext(ctx).Create(&version).Error; err != nil {
		return response.Internal().Wrap(err)
	}
	return nil
}

func (s *Service) save(ctx context.Context, row Snapshot) error {
	db := s.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		// 顺序不能反：先留档再覆盖，覆盖失败回滚时历史也不会留下一条「根本没生效过」的版本。
		if err := archiveCurrent(ctx, tx, row.SHA256); err != nil {
			return err
		}
		res := tx.Model(&Snapshot{}).Where("id = ?", idgen.ID(snapshotID)).Updates(map[string]any{
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
			if err := tx.Create(&row).Error; err != nil {
				return response.Internal().Wrap(err)
			}
		}
		return nil
	})
}

// ListVersions 列出历史版本（新→旧，不含整棵树正文）：版本历史覆盖的是
// 「上一次生效的快照」，所以当前生效的那一份不在列表里，看它用状态接口。
func (s *Service) ListVersions(ctx context.Context) ([]VersionView, error) {
	if s.db == nil {
		return []VersionView{}, nil
	}
	var rows []SnapshotVersion
	if err := s.db.WithContext(ctx).Order("version DESC").Limit(maxVersions).Find(&rows).Error; err != nil {
		return nil, response.Internal().Wrap(err)
	}
	out := make([]VersionView, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.view())
	}
	return out, nil
}

// GetVersion 取单个版本的完整记录（含整棵树正文）。
func (s *Service) GetVersion(ctx context.Context, version int) (VersionDetail, error) {
	row, err := s.versionRow(ctx, version)
	if err != nil {
		return VersionDetail{}, err
	}
	return VersionDetail{VersionView: row.view(), Payload: row.Payload}, nil
}

func (s *Service) versionRow(ctx context.Context, version int) (SnapshotVersion, error) {
	var row SnapshotVersion
	if s.db == nil {
		return row, response.NotFound("region.version_not_found", version)
	}
	err := s.db.WithContext(ctx).Where("version = ?", version).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, response.NotFound("region.version_not_found", version)
	}
	if err != nil {
		return row, response.Internal().Wrap(err)
	}
	return row, nil
}

// RollbackToVersion 回滚到某个历史版本：把那份 payload 当作一次正常同步走完整校验与落库，
// 回滚前的当前快照同样留档，所以版本号继续递增、历史不丢，回滚本身也能再被回滚。
// 来源与数据日期沿用该版本记录的值，页面看到的「这份数据从哪来」不会因为回滚而变成空。
func (s *Service) RollbackToVersion(ctx context.Context, version int, actor string) (Result, error) {
	if syncRunning() {
		return Result{}, response.Conflict("region.sync_running")
	}
	row, err := s.versionRow(ctx, version)
	if err != nil {
		return Result{}, err
	}
	meta := region.Info{
		SourceURL: row.SourceURL,
		DataDate:  row.DataDate,
		License:   s.licenseFor(row.SourceURL),
		SyncedBy:  actor,
		SyncedAt:  time.Now().Format(time.RFC3339),
	}
	res, err := s.apply(ctx, []byte(row.Payload), meta, true)
	if err != nil {
		return Result{}, err
	}
	if s.log != nil {
		s.log.Info("region: 已回滚到历史版本", zap.Int("version", version), zap.String("actor", actor), zap.String("sha256", row.SHA256))
	}
	return res, nil
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
// 两种格式都会先补全港澳台：公开数据源大多不含台湾省，只含港澳，
// 不补就会让「同步一次港澳台又没了」——补全后的字节就是落库与生效的字节，摘要与差异对比都按它算。
func parseSource(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, response.BadRequest("region.data_invalid", "数据源为空")
	}
	var tree []byte
	var err error
	switch trimmed[0] {
	case '{':
		var flat map[string]string
		if err = json.Unmarshal(trimmed, &flat); err != nil || len(flat) == 0 {
			return nil, response.BadRequest("region.data_invalid", "扁平格式应为 code→name 的对象")
		}
		if tree, err = region.FromFlat(flat); err != nil {
			return nil, response.BadRequest("region.data_invalid", err.Error())
		}
	case '[':
		var nodes []*region.Node
		if err = json.Unmarshal(trimmed, &nodes); err != nil || len(nodes) == 0 {
			return nil, response.BadRequest("region.data_invalid", "树格式应为节点数组")
		}
		if tree, err = region.Encode(nodes); err != nil {
			return nil, response.BadRequest("region.data_invalid", err.Error())
		}
	default:
		return nil, response.BadRequest("region.data_invalid", "数据源应是 JSON 对象或数组")
	}
	completed, err := region.EnsureSpecialRegions(tree)
	if err != nil {
		return nil, response.BadRequest("region.data_invalid", err.Error())
	}
	return completed, nil
}

// diffTree 对比新旧两棵树：返回三类变更的计数，并各收集最多 maxDiffSamples 条样本
// 供同步预览展示；遍历顺序确定，样本是确定性的（按 code 排序后取前 N 条）。
func diffTree(before []*region.Node, raw []byte) (added, removed, renamed int, addedS, removedS, renamedS []DiffSample) {
	var after []*region.Node
	if err := json.Unmarshal(raw, &after); err != nil {
		return 0, 0, 0, nil, nil, nil
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
	// 排序保证样本确定性，不然每次预览的样本都不一样，没法对照。
	codes := make([]string, 0, len(newNames)+len(oldNames))
	for code := range newNames {
		codes = append(codes, code)
	}
	for code := range oldNames {
		if _, ok := newNames[code]; !ok {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	for _, code := range codes {
		name, ok := newNames[code]
		if !ok {
			removed++
			if len(removedS) < maxDiffSamples {
				removedS = append(removedS, DiffSample{Code: code, Name: oldNames[code]})
			}
			continue
		}
		old, ok := oldNames[code]
		switch {
		case !ok:
			added++
			if len(addedS) < maxDiffSamples {
				addedS = append(addedS, DiffSample{Code: code, Name: name})
			}
		case old != name:
			renamed++
			if len(renamedS) < maxDiffSamples {
				renamedS = append(renamedS, DiffSample{Code: code, Name: name, Old: old})
			}
		}
	}
	return added, removed, renamed, addedS, removedS, renamedS
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
