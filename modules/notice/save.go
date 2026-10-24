package notice

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Save 新增（id=0）或修改公告，并整体替换发布对象。新增一律是草稿。
func (s *Service) Save(ctx context.Context, id idgen.ID, f Form, by idgen.ID, allowPlatform bool) (*View, error) {
	if f.Platform && !allowPlatform {
		return nil, response.Forbidden("error.forbidden")
	}
	if f.Scope != ScopeAll && len(f.TargetIDs) == 0 {
		return nil, badReq("请选择发布对象")
	}
	publishAt, ok1 := parseTime(f.PublishAt)
	expireAt, ok2 := parseTime(f.ExpireAt)
	if !ok1 || !ok2 {
		return nil, badReq("时间格式不正确")
	}
	if publishAt != nil && expireAt != nil && !expireAt.After(*publishAt) {
		return nil, badReq("到期时间必须晚于发布时间")
	}

	n := Notice{Status: StatusDraft, CreatedBy: by}
	if id != 0 {
		cur, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		n = *cur
	}
	att := strings.TrimSpace(string(f.Attachments))
	if att == "" || att == "null" {
		att = "[]"
	}
	n.Title, n.Type, n.Level, n.Scope = f.Title, f.Type, f.Level, f.Scope
	n.Content, n.Pinned, n.Popup, n.Platform = sanitize(f.Content), f.Pinned, f.Popup, f.Platform
	n.PublishAt, n.ExpireAt, n.Attachments = publishAt, expireAt, string(attachmentsJSON(att))

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&n).Error; err != nil {
			return err
		}
		if err := tx.Where("notice_id = ?", int64(n.ID)).Delete(&Target{}).Error; err != nil {
			return err
		}
		if f.Scope == ScopeAll {
			return nil
		}
		rows := make([]Target, 0, len(f.TargetIDs))
		for _, t := range f.TargetIDs {
			rows = append(rows, Target{NoticeID: n.ID, TargetID: t})
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, n.ID)
}

// Publish 发布（草稿/已撤回 → 已发布）：记录发布时间和应读人数快照，并清空旧的已读记录。
// 设置了 publishAt 的公告到点前对用户不可见，这就是“定时发布”。
func (s *Service) Publish(ctx context.Context, id idgen.ID) (*View, error) {
	n, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if n.Status == StatusPublished {
		return nil, badReq("公告已发布")
	}
	targets := s.targetIDs(ctx, id)
	if n.Scope != ScopeAll && len(targets) == 0 {
		return nil, badReq("请选择发布对象")
	}
	var total int64
	if err := s.recipients(ctx, n, targets).Count(&total).Error; err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Model(&Notice{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"status": StatusPublished, "published_at": time.Now().UTC(), "total_count": int(total),
	}).Error
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(tenant.Skip(ctx)).Where("notice_id = ?", int64(id)).Delete(&Read{}).Error; err != nil {
		return nil, err
	}
	publishedAt := time.Now().UTC()
	event.Publish(ctx, event.NoticePublished, event.NoticeData{ID: n.ID, TenantID: n.TenantID, Title: n.Title, Type: n.Type,
		Level: n.Level, Scope: n.Scope, PublishedAt: &publishedAt})
	return s.Get(ctx, id)
}

// Revoke 撤回已发布的公告。
func (s *Service) Revoke(ctx context.Context, id idgen.ID) (*View, error) {
	n, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if n.Status != StatusPublished {
		return nil, badReq("只有已发布的公告可以撤回")
	}
	if err := s.db.WithContext(ctx).Model(&Notice{}).Where("id = ?", int64(id)).Update("status", StatusRevoked).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// recipients 返回“应读用户”的查询（别名 u）。平台公告面向所有租户，其余限定公告所在租户。
func (s *Service) recipients(ctx context.Context, n *Notice, targets []idgen.ID) *gorm.DB {
	q := s.db.WithContext(tenant.Skip(ctx)).Table("sys_user AS u").Where("u.deleted_at IS NULL")
	if !n.Platform {
		q = q.Where("u.tenant_id = ?", int64(n.TenantID))
	}
	switch n.Scope {
	case ScopeDept:
		q = q.Where("u.dept_id IN ?", ids64(targets))
	case ScopeRole:
		q = q.Where("u.id IN (?)", s.db.Table("sys_user_role").Select("user_id").Where("role_id IN ?", ids64(targets)))
	case ScopeUser:
		q = q.Where("u.id IN ?", ids64(targets))
	}
	return q
}

type unreadRow struct {
	ID       idgen.ID
	Username string
	Nickname string
	DeptID   idgen.ID
}

// ReadStats 已读/未读人数与未读名单（最多 100 人）。
func (s *Service) ReadStats(ctx context.Context, id idgen.ID) (*ReadStats, error) {
	n, err := s.get(ctx, id)
	if err != nil {
		return nil, err
	}
	var read int64
	if err := s.db.WithContext(tenant.Skip(ctx)).Model(&Read{}).Where("notice_id = ?", int64(id)).Count(&read).Error; err != nil {
		return nil, err
	}
	readSub := s.db.Table("sys_notice_read").Select("user_id").Where("notice_id = ?", int64(id))
	q := s.recipients(ctx, n, s.targetIDs(ctx, id)).Where("u.id NOT IN (?)", readSub).Session(&gorm.Session{})
	st := &ReadStats{ReadCount: read, UnreadUsers: []UnreadUser{}}
	if err := q.Count(&st.UnreadCount).Error; err != nil {
		return nil, err
	}
	var rows []unreadRow
	if err := q.Select("u.id, u.username, u.nickname, u.dept_id").Order("u.id").Limit(100).Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := s.deptNames(rows)
	for _, r := range rows {
		st.UnreadUsers = append(st.UnreadUsers, UnreadUser{ID: r.ID, Username: r.Username, Nickname: r.Nickname, DeptName: names[r.DeptID]})
	}
	return st, nil
}

// deptNames 尽力查部门名（失败就不显示部门，不影响主流程）。
func (s *Service) deptNames(rows []unreadRow) map[idgen.ID]string {
	out := map[idgen.ID]string{}
	var ids []int64
	for _, r := range rows {
		ids = append(ids, int64(r.DeptID))
	}
	if len(ids) == 0 {
		return out
	}
	var ds []struct {
		ID   idgen.ID
		Name string
	}
	if s.db.Table("sys_dept").Select("id, name").Where("id IN ?", ids).Scan(&ds).Error == nil {
		for _, d := range ds {
			out[d.ID] = d.Name
		}
	}
	return out
}
