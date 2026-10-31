package workflow

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/nexadmin/flowlite/store"
	"nexadmin/pkg/idgen"
)

// 本文件是 flowlite 存储端口的 GORM 适配层。
// 租户隔离沿用框架的 tenant 插件：所有查询/更新都通过 Model/TenantModel 触发，
// 保证不会因为手写 SQL 绕过 tenant_id 条件。

func idText(id idgen.ID) string { return strconv.FormatInt(int64(id), 10) }

func parseID(s string) (idgen.ID, error) { return idgen.Parse(strings.TrimSpace(s)) }

func notFoundOr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.ErrNotFound
	}
	return err
}

// ---- 定义 ----

type gormDefs struct{ db *gorm.DB }

func defRecord(m *Def) *store.Definition {
	return &store.Definition{
		ID: idText(m.ID), Key: m.DefKey, Name: m.Name, Status: m.Status, Version: m.Version,
		Definition: []byte(m.Definition), XML: []byte(m.BpmnXML),
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func (g gormDefs) Get(ctx context.Context, id string) (*store.Definition, error) {
	key, err := parseID(id)
	if err != nil {
		return nil, store.ErrNotFound
	}
	var m Def
	if err := g.db.WithContext(ctx).First(&m, "id = ?", int64(key)).Error; err != nil {
		return nil, notFoundOr(err)
	}
	return defRecord(&m), nil
}

func (g gormDefs) GetByKey(ctx context.Context, key string) (*store.Definition, error) {
	var m Def
	err := g.db.WithContext(ctx).Where("def_key = ?", key).Order("version DESC, id DESC").Take(&m).Error
	if err != nil {
		return nil, notFoundOr(err)
	}
	return defRecord(&m), nil
}

func (g gormDefs) Save(ctx context.Context, d *store.Definition) (*store.Definition, error) {
	db := g.db.WithContext(ctx)
	if strings.TrimSpace(d.ID) == "" {
		m := &Def{
			DefKey: d.Key, Name: d.Name, Status: d.Status, Version: 1,
			Definition: string(d.Definition), BpmnXML: string(d.XML),
		}
		if err := db.Create(m).Error; err != nil {
			return nil, err
		}
		return defRecord(m), nil
	}
	id, err := parseID(d.ID)
	if err != nil {
		return nil, store.ErrNotFound
	}
	var m Def
	if err := db.First(&m, "id = ?", int64(id)).Error; err != nil {
		return nil, notFoundOr(err)
	}
	m.DefKey, m.Name, m.Status = d.Key, d.Name, d.Status
	m.Definition, m.BpmnXML = string(d.Definition), string(d.XML)
	m.Version++
	if err := db.Save(&m).Error; err != nil {
		return nil, err
	}
	return defRecord(&m), nil
}

func (g gormDefs) Delete(ctx context.Context, id string) error {
	key, err := parseID(id)
	if err != nil {
		return store.ErrNotFound
	}
	res := g.db.WithContext(ctx).Delete(&Def{}, "id = ?", int64(key))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (g gormDefs) List(ctx context.Context, q store.DefQuery) ([]*store.Definition, int64, error) {
	base := g.db.WithContext(ctx).Model(&Def{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		base = base.Where("def_key LIKE ? OR name LIKE ?", like, like)
	}
	if q.Status != "" {
		base = base.Where("status = ?", q.Status)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Def
	err := base.Session(&gorm.Session{}).Order("id DESC").
		Offset((pageOf(q.Page) - 1) * sizeOf(q.PageSize)).Limit(sizeOf(q.PageSize)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]*store.Definition, 0, len(rows))
	for i := range rows {
		out = append(out, defRecord(&rows[i]))
	}
	return out, total, nil
}

// ---- 实例 ----

type gormInsts struct{ db *gorm.DB }

func instRecord(m *Inst) *store.Instance {
	return &store.Instance{
		ID: idText(m.ID), DefID: idText(m.DefID), DefKey: m.DefKey, DefVersion: m.DefVersion,
		Title: m.Title, BizType: m.BizType, BusinessKey: m.BusinessKey, Submitter: m.Submitter,
		Status: m.Status, Revision: m.Revision, TenantID: idText(m.TenantID),
		State: []byte(m.StateJSON), Snapshot: []byte(m.DefSnapshot),
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func (g gormInsts) Create(ctx context.Context, rec *store.Instance) error {
	defID, err := parseID(rec.DefID)
	if err != nil {
		return err
	}
	m := &Inst{
		DefID: defID, DefKey: rec.DefKey, DefVersion: rec.DefVersion,
		Title: rec.Title, BizType: rec.BizType, BusinessKey: rec.BusinessKey,
		Submitter: rec.Submitter, Status: rec.Status, Revision: rec.Revision,
		DefSnapshot: string(rec.Snapshot), StateJSON: string(rec.State),
	}
	if tenantID, err := parseID(rec.TenantID); err == nil && tenantID != 0 {
		m.TenantID = tenantID
	}
	if err := g.db.WithContext(ctx).Create(m).Error; err != nil {
		return err
	}
	rec.ID = idText(m.ID)
	rec.TenantID = idText(m.TenantID)
	rec.CreatedAt, rec.UpdatedAt = m.CreatedAt, m.UpdatedAt
	return nil
}

func (g gormInsts) Get(ctx context.Context, id string) (*store.Instance, error) {
	key, err := parseID(id)
	if err != nil {
		return nil, store.ErrNotFound
	}
	var m Inst
	if err := g.db.WithContext(ctx).First(&m, "id = ?", int64(key)).Error; err != nil {
		return nil, notFoundOr(err)
	}
	return instRecord(&m), nil
}

func (g gormInsts) GetMany(ctx context.Context, ids []string) ([]*store.Instance, error) {
	keys := make([]int64, 0, len(ids))
	for _, s := range ids {
		if k, err := parseID(s); err == nil {
			keys = append(keys, int64(k))
		}
	}
	if len(keys) == 0 {
		return []*store.Instance{}, nil
	}
	var rows []Inst
	if err := g.db.WithContext(ctx).Where("id IN ?", keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*store.Instance, 0, len(rows))
	for i := range rows {
		out = append(out, instRecord(&rows[i]))
	}
	return out, nil
}

// Update 用 revision 做乐观锁：只有库里仍是读到的版本才写。
func (g gormInsts) Update(ctx context.Context, rec *store.Instance, expectedRevision int64) error {
	key, err := parseID(rec.ID)
	if err != nil {
		return store.ErrNotFound
	}
	db := g.db.WithContext(ctx)
	res := db.Model(&Inst{}).Where("id = ? AND revision = ?", int64(key), expectedRevision).
		Updates(map[string]any{
			"status":       rec.Status,
			"revision":     rec.Revision,
			"state_json":   string(rec.State),
			"def_snapshot": string(rec.Snapshot),
			"updated_at":   time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var n int64
		if err := db.Model(&Inst{}).Where("id = ?", int64(key)).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return store.ErrConflict
	}
	rec.UpdatedAt = time.Now().UTC()
	return nil
}

func (g gormInsts) List(ctx context.Context, q store.InstQuery) ([]*store.Instance, int64, error) {
	base := g.db.WithContext(ctx).Model(&Inst{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		base = base.Where("title LIKE ? OR def_key LIKE ? OR business_key LIKE ?", like, like, like)
	}
	if q.Status != "" {
		base = base.Where("status = ?", q.Status)
	}
	if q.BizType != "" {
		base = base.Where("biz_type = ?", q.BizType)
	}
	if q.Submitter != "" {
		base = base.Where("submitter = ?", q.Submitter)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Inst
	err := base.Session(&gorm.Session{}).Order("id DESC").
		Offset((pageOf(q.Page) - 1) * sizeOf(q.PageSize)).Limit(sizeOf(q.PageSize)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]*store.Instance, 0, len(rows))
	for i := range rows {
		out = append(out, instRecord(&rows[i]))
	}
	return out, total, nil
}

func (g gormInsts) FindRunningByBiz(ctx context.Context, bizType, businessKey string) (*store.Instance, error) {
	var m Inst
	err := g.db.WithContext(ctx).
		Where("biz_type = ? AND business_key = ? AND status = ?", bizType, businessKey, "running").
		Order("id DESC").Take(&m).Error
	if err != nil {
		return nil, notFoundOr(err)
	}
	return instRecord(&m), nil
}

func (g gormInsts) ListRunning(ctx context.Context, limit int) ([]*store.Instance, error) {
	var rows []Inst
	if err := g.db.WithContext(ctx).Where("status = ?", "running").
		Order("updated_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*store.Instance, 0, len(rows))
	for i := range rows {
		out = append(out, instRecord(&rows[i]))
	}
	return out, nil
}

// ---- 待办 ----

type gormTasks struct{ db *gorm.DB }

// SyncPending 用给定集合替换实例的全部待办：同 (node, assignee) 保留创建时间，
// 不再出现的置为 closed。整段在一个事务里完成，避免读者看到半套待办。
func (g gormTasks) SyncPending(ctx context.Context, instID, defKey string, pending []store.PendingTask) error {
	key, err := parseID(instID)
	if err != nil {
		return store.ErrNotFound
	}
	return g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []Task
		if err := tx.Where("inst_id = ? AND status = ?", int64(key), TaskPending).Find(&rows).Error; err != nil {
			return err
		}
		existing := make(map[string]*Task, len(rows))
		for i := range rows {
			existing[rows[i].NodeKey+"\x00"+rows[i].Assignee] = &rows[i]
		}
		now := time.Now().UTC()
		for _, p := range pending {
			sig := p.NodeKey + "\x00" + p.Assignee
			if cur, ok := existing[sig]; ok {
				if cur.NodeName != p.NodeName {
					if err := tx.Model(&Task{}).Where("id = ?", int64(cur.ID)).
						Updates(map[string]any{"node_name": p.NodeName, "updated_at": now}).Error; err != nil {
						return err
					}
				}
				delete(existing, sig)
				continue
			}
			m := &Task{
				InstID: key, DefKey: defKey, NodeKey: p.NodeKey, NodeName: p.NodeName,
				Assignee: p.Assignee, Status: TaskPending,
			}
			if err := tx.Create(m).Error; err != nil {
				return err
			}
		}
		for _, cur := range existing {
			if err := tx.Model(&Task{}).Where("id = ?", int64(cur.ID)).
				Updates(map[string]any{"status": TaskClosed, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (g gormTasks) ListPending(ctx context.Context, assignee string, q store.TaskQuery) ([]*store.Task, int64, error) {
	base := g.db.WithContext(ctx).Model(&Task{}).
		Where("status = ?", TaskPending).
		Where("assignee = ? OR assignee = ''", assignee)
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Task
	err := base.Session(&gorm.Session{}).Order("created_at DESC, id DESC").
		Offset((pageOf(q.Page) - 1) * sizeOf(q.PageSize)).Limit(sizeOf(q.PageSize)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]*store.Task, 0, len(rows))
	for i := range rows {
		m := &rows[i]
		out = append(out, &store.Task{
			ID: idText(m.ID), InstID: idText(m.InstID), DefKey: m.DefKey,
			NodeKey: m.NodeKey, NodeName: m.NodeName, Assignee: m.Assignee, Status: m.Status,
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		})
	}
	return out, total, nil
}

func (g gormTasks) ListByInstance(ctx context.Context, instID string) ([]*store.Task, error) {
	key, err := parseID(instID)
	if err != nil {
		return nil, store.ErrNotFound
	}
	var rows []Task
	if err := g.db.WithContext(ctx).Where("inst_id = ?", int64(key)).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*store.Task, 0, len(rows))
	for i := range rows {
		m := &rows[i]
		out = append(out, &store.Task{
			ID: idText(m.ID), InstID: idText(m.InstID), DefKey: m.DefKey,
			NodeKey: m.NodeKey, NodeName: m.NodeName, Assignee: m.Assignee, Status: m.Status,
			CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		})
	}
	return out, nil
}

// ---- 动作 ----

type gormActions struct{ db *gorm.DB }

func (g gormActions) Record(ctx context.Context, a *store.Action) error {
	instID, err := parseID(a.InstID)
	if err != nil {
		return err
	}
	m := &Action{
		InstID: instID, DefKey: a.DefKey, NodeKey: a.NodeKey, Actor: a.Actor,
		Action: a.Action, Comment: a.Comment, OccurredAt: a.At,
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = time.Now().UTC()
	}
	if err := g.db.WithContext(ctx).Create(m).Error; err != nil {
		return err
	}
	a.ID = idText(m.ID)
	return nil
}

func (g gormActions) List(ctx context.Context, q store.ActionQuery) ([]*store.Action, int64, error) {
	base := g.db.WithContext(ctx).Model(&Action{})
	if q.Actor != "" {
		base = base.Where("actor = ?", q.Actor)
	}
	if len(q.Actions) > 0 {
		base = base.Where("action IN ?", q.Actions)
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Action
	err := base.Session(&gorm.Session{}).Order("occurred_at DESC, id DESC").
		Offset((pageOf(q.Page) - 1) * sizeOf(q.PageSize)).Limit(sizeOf(q.PageSize)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]*store.Action, 0, len(rows))
	for i := range rows {
		m := &rows[i]
		out = append(out, &store.Action{
			ID: idText(m.ID), InstID: idText(m.InstID), DefKey: m.DefKey,
			NodeKey: m.NodeKey, Actor: m.Actor, Action: m.Action, Comment: m.Comment, At: m.OccurredAt,
		})
	}
	return out, total, nil
}

func (g gormActions) ListByInstance(ctx context.Context, instID string) ([]*store.Action, error) {
	key, err := parseID(instID)
	if err != nil {
		return nil, store.ErrNotFound
	}
	var rows []Action
	if err := g.db.WithContext(ctx).Where("inst_id = ?", int64(key)).Order("occurred_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*store.Action, 0, len(rows))
	for i := range rows {
		m := &rows[i]
		out = append(out, &store.Action{
			ID: idText(m.ID), InstID: idText(m.InstID), DefKey: m.DefKey,
			NodeKey: m.NodeKey, Actor: m.Actor, Action: m.Action, Comment: m.Comment, At: m.OccurredAt,
		})
	}
	return out, nil
}

func pageOf(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

func sizeOf(size int) int {
	if size < 1 {
		return 20
	}
	if size > 200 {
		return 200
	}
	return size
}
