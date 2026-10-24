package job

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

// Filter 是列表筛选。Owner 为 0 表示不限所有者（管理员看全部）。
type Filter struct {
	Owner    idgen.ID
	Type     string
	Status   string
	Page     int
	PageSize int
}

// Store 是任务持久化接口。所有状态迁移都是“带旧状态条件的单条 UPDATE”，以影响行数判断是否成功，
// 因此多实例并发时不会重复执行、也不会把已取消的任务覆盖回成功。
type Store interface {
	Insert(ctx context.Context, j *Job) error
	Get(ctx context.Context, id idgen.ID) (*Job, error)
	List(ctx context.Context, f Filter) ([]*Job, int64, error)
	CountActive(ctx context.Context, owner idgen.ID) (int64, error)
	// Claim 乐观抢占一个 queued 任务（类型限定在 types 内）；没有可抢的返回 nil, nil。
	Claim(ctx context.Context, types []string, node string, now time.Time) (*Job, error)
	// SetProgress 仅在任务仍是 running 时更新进度。
	SetProgress(ctx context.Context, id idgen.ID, progress int, msg string) error
	// Finish 仅在任务仍是 running 时落终态；返回 false 表示已被取消或其它实例处理。
	Finish(ctx context.Context, id idgen.ID, status string, res ResultFile, errMsg string, now time.Time) (bool, error)
	// Cancel 把 queued / running 任务置为 canceled；返回是否发生了迁移。
	Cancel(ctx context.Context, id idgen.ID, now time.Time) (bool, error)
	// Requeue 把 failed 任务重新排队；返回是否发生了迁移。
	Requeue(ctx context.Context, id idgen.ID) (bool, error)
	Status(ctx context.Context, id idgen.ID) (string, error)
	// ResetRunning 把本节点遗留的 running 置为 failed，返回条数。
	ResetRunning(ctx context.Context, node, msg string, now time.Time) (int64, error)
	// Delete 删除已结束的任务；返回是否删除。
	Delete(ctx context.Context, id idgen.ID) (bool, error)
	// Expired 返回结束时间早于 before 的任务（跨租户，调用方需带 tenant.Skip）。
	Expired(ctx context.Context, before time.Time, limit int) ([]*Job, error)
}

var finals = []string{StatusSuccess, StatusFailed, StatusCanceled}

// GormStore 是基于 GORM 的 Store。
type GormStore struct{ db *gorm.DB }

// NewGormStore 创建 Store。
func NewGormStore(db *gorm.DB) *GormStore { return &GormStore{db: db} }

func (s *GormStore) tx(ctx context.Context) *gorm.DB { return s.db.WithContext(ctx) }

func (s *GormStore) Insert(ctx context.Context, j *Job) error { return s.tx(ctx).Create(j).Error }

func (s *GormStore) Get(ctx context.Context, id idgen.ID) (*Job, error) {
	var j Job
	if err := s.tx(ctx).Where("id = ?", int64(id)).Take(&j).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &j, nil
}

func (s *GormStore) List(ctx context.Context, f Filter) ([]*Job, int64, error) {
	q := s.tx(ctx).Model(&Job{})
	if f.Owner != 0 {
		q = q.Where("owner_id = ?", int64(f.Owner))
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	switch f.Status {
	case "":
	case StatusActive: // 进行中 = 排队 + 执行（顶栏徽标用）
		q = q.Where("status IN ?", []string{StatusQueued, StatusRunning})
	default:
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*Job
	page, size := max(f.Page, 1), f.PageSize
	if size < 1 {
		size = 10
	}
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}

func (s *GormStore) CountActive(ctx context.Context, owner idgen.ID) (int64, error) {
	var n int64
	err := s.tx(ctx).Model(&Job{}).Where("owner_id = ? AND status IN ?", int64(owner), []string{StatusQueued, StatusRunning}).Count(&n).Error
	return n, err
}

// QueueCounter 是可选能力：按状态统计 queued / running 任务数（跨租户）。Store 不实现时队列指标不输出。
type QueueCounter interface {
	CountByStatus(ctx context.Context) (map[string]int64, error)
}

// CountByStatus 一条 GROUP BY 统计排队 / 执行中的任务数；调用方需带 tenant.Skip（指标是全局口径）。
func (s *GormStore) CountByStatus(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		N      int64
	}
	err := s.tx(ctx).Model(&Job{}).Select("status, COUNT(*) AS n").
		Where("status IN ?", []string{StatusQueued, StatusRunning}).Group("status").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{StatusQueued: 0, StatusRunning: 0}
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out, nil
}

func (s *GormStore) Claim(ctx context.Context, types []string, node string, now time.Time) (*Job, error) {
	if len(types) == 0 {
		return nil, nil
	}
	for range 5 { // 候选被别的实例抢走时换下一个
		var c Job
		err := s.tx(ctx).Where("status = ? AND type IN ?", StatusQueued, types).Order("created_at ASC, id ASC").Limit(1).Take(&c).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		res := s.tx(ctx).Model(&Job{}).Where("id = ? AND status = ?", int64(c.ID), StatusQueued).
			Updates(map[string]any{"status": StatusRunning, "node": node, "started_at": now, "progress": 0, "message": ""})
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 1 {
			c.Status, c.Node, c.StartedAt, c.Progress, c.Message = StatusRunning, node, &now, 0, ""
			return &c, nil
		}
	}
	return nil, nil
}

func (s *GormStore) SetProgress(ctx context.Context, id idgen.ID, progress int, msg string) error {
	return s.tx(ctx).Model(&Job{}).Where("id = ? AND status = ?", int64(id), StatusRunning).
		Updates(map[string]any{"progress": progress, "message": msg}).Error
}

func (s *GormStore) Finish(ctx context.Context, id idgen.ID, status string, res ResultFile, errMsg string, now time.Time) (bool, error) {
	set := map[string]any{"status": status, "finished_at": now, "error": errMsg}
	if status == StatusSuccess {
		set["progress"] = 100
		set["result_file"], set["result_name"], set["result_size"], set["result_mime"] = res.Key, res.Name, res.Size, res.ContentType
	}
	r := s.tx(ctx).Model(&Job{}).Where("id = ? AND status = ?", int64(id), StatusRunning).Updates(set)
	return r.RowsAffected == 1, r.Error
}

func (s *GormStore) Cancel(ctx context.Context, id idgen.ID, now time.Time) (bool, error) {
	r := s.tx(ctx).Model(&Job{}).Where("id = ? AND status IN ?", int64(id), []string{StatusQueued, StatusRunning}).
		Updates(map[string]any{"status": StatusCanceled, "finished_at": now, "message": ""})
	return r.RowsAffected == 1, r.Error
}

func (s *GormStore) Requeue(ctx context.Context, id idgen.ID) (bool, error) {
	r := s.tx(ctx).Model(&Job{}).Where("id = ? AND status = ?", int64(id), StatusFailed).Updates(map[string]any{
		"status": StatusQueued, "progress": 0, "message": "", "error": "", "node": "",
		"result_file": "", "result_name": "", "result_size": 0, "result_mime": "",
		"started_at": nil, "finished_at": nil,
	})
	return r.RowsAffected == 1, r.Error
}

func (s *GormStore) Status(ctx context.Context, id idgen.ID) (string, error) {
	var st struct{ Status string }
	err := s.tx(ctx).Model(&Job{}).Select("status").Where("id = ?", int64(id)).Take(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotFound
	}
	return st.Status, err
}

func (s *GormStore) ResetRunning(ctx context.Context, node, msg string, now time.Time) (int64, error) {
	r := s.tx(ctx).Model(&Job{}).Where("status = ? AND node = ?", StatusRunning, node).
		Updates(map[string]any{"status": StatusFailed, "error": msg, "finished_at": now})
	return r.RowsAffected, r.Error
}

func (s *GormStore) Delete(ctx context.Context, id idgen.ID) (bool, error) {
	r := s.tx(ctx).Where("id = ? AND status IN ?", int64(id), finals).Delete(&Job{})
	return r.RowsAffected == 1, r.Error
}

func (s *GormStore) Expired(ctx context.Context, before time.Time, limit int) ([]*Job, error) {
	var rows []*Job
	err := s.tx(ctx).Where("status IN ? AND finished_at < ?", finals, before).Order("finished_at ASC").Limit(limit).Find(&rows).Error
	return rows, err
}
