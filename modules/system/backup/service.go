package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/config"
	"nexadmin/core/ctl"
	"nexadmin/pkg/idgen"
)

const (
	dumpTimeout = 2 * time.Hour
	staleAfter  = dumpTimeout + 10*time.Minute // 超过这个时间还是 running 的记录视为进程中断
	maxKeepDays = 3650
	maxKeepCnt  = 1000
)

// 业务错误；handler 转成对应的 HTTP 响应。
var (
	ErrBusy     = errors.New("已有备份正在进行")
	ErrNotFound = errors.New("备份不存在")
	ErrNotReady = errors.New("备份未完成或已失败，没有可用的文件")
)

// UnavailableError 表示本机缺少导出工具（或连接参数不安全）。
// Err 保留原始错误，handler 据此区分“缺工具”（可本地化）与其他原因。
type UnavailableError struct {
	Reason string
	Err    error
}

// ErrIsolationUnsupported 表示独立租户库模式不支持页面上发起的单库备份（handler 按 key 返回文案）。
var ErrIsolationUnsupported = errors.New("独立租户数据库模式请使用命令行多库备份；此页面的单库备份不能覆盖租户业务数据")

func (e *UnavailableError) Error() string { return e.Reason }

func (e *UnavailableError) Unwrap() error { return e.Err }

// Dumper 隔离“真正调用外部工具”的部分；生产用 toolDumper，单测用假实现。
type Dumper interface {
	Check() (tool string, err error) // 工具不可用时 err.Error() 就是页面上显示的原因
	Dump(ctx context.Context, path string) (ctl.DumpResult, error)
	Engine() string
}

type toolDumper struct {
	cfg  config.Database
	look ctl.LookPath
}

func (d toolDumper) Check() (string, error) {
	if d.cfg.Isolation.Enabled {
		return "", ErrIsolationUnsupported
	}
	t, err := ctl.DumpTool(d.cfg, d.look)
	if err != nil {
		return "", err
	}
	return t.Name, nil
}

func (d toolDumper) Dump(ctx context.Context, path string) (ctl.DumpResult, error) {
	if _, err := d.Check(); err != nil {
		return ctl.DumpResult{}, err
	}
	t, err := ctl.DumpTool(d.cfg, d.look)
	if err != nil {
		return ctl.DumpResult{}, err
	}
	return ctl.DumpGzip(ctx, t, path)
}

func (d toolDumper) Engine() string { return ctl.Engine(d.cfg) }

// Service 是备份的业务层。
type Service struct {
	db      *gorm.DB
	log     *zap.Logger
	dir     string
	init    config.Backup // yaml 里的初始设置（库里没有设置行时用）
	dumper  Dumper
	version string
	now     func() time.Time
	loc     *time.Location
	free    func(dir string) (int64, bool)

	mu      sync.Mutex
	running bool
}

// NewService 创建 Service；version 是框架版本（写进文件名与元数据）。
func NewService(db *gorm.DB, log *zap.Logger, cfg *config.Config, version string) *Service {
	loc, err := time.LoadLocation(cfg.App.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return &Service{
		db: db, log: log, dir: ctl.BackupDir(cfg), init: cfg.Backup, version: version,
		dumper: toolDumper{cfg: cfg.Database}, now: time.Now, loc: loc, free: freeSpace,
	}
}

// freeSpace 目录还没创建时，向上找最近的已存在目录。
func freeSpace(dir string) (int64, bool) {
	p, err := filepath.Abs(dir)
	if err != nil {
		return 0, false
	}
	for {
		if _, err := os.Stat(p); err == nil {
			return diskFree(p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0, false
		}
		p = parent
	}
}

func clipErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1000 {
		s = s[:1000]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	return s
}

// ---- 设置 ----

func (s *Service) defaults() Setting {
	return Setting{ID: settingID, Schedule: s.init.Schedule, KeepDays: s.init.KeepDays, KeepCount: s.init.KeepCount}
}

// EnsureSetting 保证设置行存在（yaml 的值作为初始值）；多实例同时执行也安全。
func (s *Service) EnsureSetting(ctx context.Context) error {
	d := s.defaults()
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&d).Error
}

// Setting 读取当前设置；库里还没有时返回 yaml 的初始值。
func (s *Service) Setting(ctx context.Context) (Setting, error) {
	var st Setting
	err := s.db.WithContext(ctx).Where("id = ?", settingID).Take(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.defaults(), nil
	}
	return st, err
}

// ConfigInput 是 PUT /backups/config 的请求体。
type ConfigInput struct {
	Schedule  string `json:"schedule"`
	KeepDays  int    `json:"keepDays"`
	KeepCount int    `json:"keepCount"`
}

// Validate 检查设置是否合法。
func (c ConfigInput) Validate() error {
	switch {
	case !config.ValidSchedule(c.Schedule):
		return errors.New("计划时间格式必须是 HH:MM（24 小时制），或留空关闭")
	case c.KeepDays < 0 || c.KeepDays > maxKeepDays:
		return errors.New("保留天数超出范围")
	case c.KeepCount < 0 || c.KeepCount > maxKeepCnt:
		return errors.New("保留份数超出范围")
	}
	return nil
}

// SaveSetting 保存设置；改动计划时间会清掉“今天已抢占”标记，让新时间当天仍可生效。
func (s *Service) SaveSetting(ctx context.Context, in ConfigInput) (Setting, error) {
	if err := in.Validate(); err != nil {
		return Setting{}, err
	}
	if err := s.EnsureSetting(ctx); err != nil {
		return Setting{}, err
	}
	old, err := s.Setting(ctx)
	if err != nil {
		return Setting{}, err
	}
	upd := map[string]any{"schedule": in.Schedule, "keep_days": in.KeepDays, "keep_count": in.KeepCount, "updated_at": s.now().UTC()}
	if in.Schedule != old.Schedule {
		upd["last_run_date"] = ""
	}
	if err := s.db.WithContext(ctx).Model(&Setting{}).Where("id = ?", settingID).Updates(upd).Error; err != nil {
		return Setting{}, err
	}
	return s.Setting(ctx)
}

// ---- 状态 ----

// ToolStatus 是导出工具的可用性；找不到工具时 Available=false 并带原因。
// Missing 只给缺的工具名，页面按当前语言拼整句，Reason 留给日志与命令行。
type ToolStatus struct {
	Available bool     `json:"available"`
	Tool      string   `json:"tool,omitempty"`
	Missing   []string `json:"missing,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

// ToolStatus 探测导出工具。
func (s *Service) ToolStatus() ToolStatus {
	tool, err := s.dumper.Check()
	if err != nil {
		st := ToolStatus{Available: false, Reason: err.Error()}
		var tm *ctl.ToolMissingError
		if errors.As(err, &tm) {
			st.Missing = tm.Names
		}
		return st
	}
	return ToolStatus{Available: true, Tool: tool}
}

// Status 是 GET /backups/status 的内容。
type Status struct {
	ToolStatus
	Engine        string     `json:"engine"`
	Version       string     `json:"version"`
	Dir           string     `json:"dir"`
	DiskFreeBytes *int64     `json:"diskFreeBytes"` // 读不到为 null
	Schedule      string     `json:"schedule"`
	NextRunAt     *time.Time `json:"nextRunAt"`
	Running       bool       `json:"running"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
}

// Status 汇总页面状态卡需要的信息。
func (s *Service) Status(ctx context.Context) (Status, error) {
	set, err := s.Setting(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{ToolStatus: s.ToolStatus(), Engine: s.dumper.Engine(), Version: s.version, Dir: s.dir, Schedule: set.Schedule}
	if n, ok := s.free(s.dir); ok {
		st.DiskFreeBytes = &n
	}
	st.NextRunAt = nextRun(set.Schedule, s.now().In(s.loc), set.LastRunDate)
	var n int64
	if err := s.db.WithContext(ctx).Model(&Record{}).Where("status = ? AND created_at > ?", StatusRunning, s.now().UTC().Add(-staleAfter)).Count(&n).Error; err != nil {
		return Status{}, err
	}
	st.Running = n > 0
	var last Record
	if err := s.db.WithContext(ctx).Where("status = ?", StatusSuccess).Order("created_at DESC").Limit(1).Find(&last).Error; err != nil {
		return Status{}, err
	}
	if last.ID != 0 {
		t := last.CreatedAt
		st.LastSuccessAt = &t
	}
	return st, nil
}

// ---- 列表 / 删除 ----

// List 分页列出备份记录（新的在前）。
func (s *Service) List(ctx context.Context, page, size int) ([]Record, int64, error) {
	var total int64
	q := s.db.WithContext(ctx).Model(&Record{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []Record
	err := q.Order("created_at DESC").Order("id DESC").Limit(size).Offset((page - 1) * size).Find(&list).Error
	return list, total, err
}

// Get 取一条记录。
func (s *Service) Get(ctx context.Context, id idgen.ID) (*Record, error) {
	var r Record
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Path 返回备份文件的完整路径；文件名不合法（路径穿越等）返回错误。
func (s *Service) Path(r *Record) (string, error) { return ctl.BackupPath(s.dir, r.FileName) }

// removeFiles 删除备份文件和 .sha256 旁路文件；文件已经不在了不算错。
func (s *Service) removeFiles(r *Record) error {
	p, err := s.Path(r)
	if err != nil {
		return err
	}
	for _, f := range []string{p, ctl.SidecarPath(p)} {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Delete 删除备份文件与记录；正在进行的备份不能删。
func (s *Service) Delete(ctx context.Context, id idgen.ID) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.Status == StatusRunning && s.now().UTC().Sub(r.CreatedAt) < staleAfter {
		return ErrBusy
	}
	if err := s.removeFiles(r); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Where("id = ?", id).Delete(&Record{}).Error
}

// ApplyRetention 按当前设置清理过期备份，返回清理条数。
func (s *Service) ApplyRetention(ctx context.Context) (int, error) {
	set, err := s.Setting(ctx)
	if err != nil {
		return 0, err
	}
	if set.KeepDays <= 0 && set.KeepCount <= 0 {
		return 0, nil
	}
	var all []Record
	if err := s.db.WithContext(ctx).Find(&all).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, r := range selectExpired(all, s.now().UTC(), set.KeepDays, set.KeepCount) {
		if err := s.removeFiles(&r); err != nil {
			s.log.Warn("backup: retention remove file failed", zap.String("file", r.FileName), zap.Error(err))
			continue
		}
		if err := s.db.WithContext(ctx).Where("id = ?", r.ID).Delete(&Record{}).Error; err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
