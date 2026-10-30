package notice

import (
	"context"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// audience 查当前用户的部门与角色，用来判断哪些“指定对象”的公告对他可见。
func (s *Service) audience(ctx context.Context, uid idgen.ID) (dept int64, roles []int64) {
	_ = s.db.WithContext(ctx).Table("sys_user").Select("dept_id").Where("id = ?", int64(uid)).Limit(1).Scan(&dept).Error
	_ = s.db.WithContext(ctx).Table("sys_user_role").Where("user_id = ?", int64(uid)).Pluck("role_id", &roles).Error
	return dept, roles
}

func (s *Service) readSub(uid idgen.ID) *gorm.DB {
	return s.db.Table("sys_notice_read").Select("notice_id").Where("user_id = ?", int64(uid))
}

// visible 是“对 uid 可见的公告”：已发布、已到发布时间、未过期、属于本租户或是平台公告，
// 并且发布范围覆盖该用户（全员 / 指定用户 / 指定部门 / 指定角色），
// 且在该用户最近一次“清空我的公告”之后发布。
func (s *Service) visible(ctx context.Context, uid idgen.ID) *gorm.DB {
	tid, _ := tenant.From(ctx)
	now := time.Now().UTC()
	dept, roles := s.audience(ctx, uid)
	byTarget := func(cond string, arg any) *gorm.DB {
		return s.db.Table("sys_notice_target").Select("notice_id").Where(cond, arg)
	}
	q := s.db.WithContext(tenant.Skip(ctx)).Model(&Notice{}).
		Where("status = ?", StatusPublished).
		Where("(publish_at IS NULL OR publish_at <= ?)", now).
		Where("(expire_at IS NULL OR expire_at > ?)", now).
		Where("(tenant_id = ? OR platform = ?)", int64(tid), true).
		Where("(scope = ? OR (scope = ? AND id IN (?)) OR (scope = ? AND id IN (?)) OR (scope = ? AND id IN (?)))",
			ScopeAll,
			ScopeUser, byTarget("target_id = ?", int64(uid)),
			ScopeDept, byTarget("target_id = ?", dept),
			ScopeRole, byTarget("target_id IN ?", roles))
	if c := s.clearedRow(ctx, uid); c != nil {
		q = q.Where("COALESCE(published_at, created_at) > ?", c.ClearedAt)
	}
	return q
}

// Clear 清空“我的公告”：公告本身不动，只记住当前时间点，之前发布的公告不再出现在
// 该用户的列表、未读数与登录弹窗里；之后发布的新公告照常可见。
func (s *Service) Clear(ctx context.Context, uid idgen.ID) error {
	now := time.Now().UTC()
	if c := s.clearedRow(ctx, uid); c != nil {
		return s.db.WithContext(ctx).Model(&Clear{}).Where("id = ?", int64(c.ID)).Update("cleared_at", now).Error
	}
	err := s.db.WithContext(ctx).Create(&Clear{UserID: uid, ClearedAt: now}).Error
	if err != nil { // 并发首次清空：唯一索引兜底，退化为更新
		if c := s.clearedRow(ctx, uid); c != nil {
			return s.db.WithContext(ctx).Model(&Clear{}).Where("id = ?", int64(c.ID)).Update("cleared_at", now).Error
		}
	}
	return err
}

// clearedRow 是用户的清空记录；从未清空返回 nil。
func (s *Service) clearedRow(ctx context.Context, uid idgen.ID) *Clear {
	var row Clear
	if err := s.db.WithContext(ctx).Where("user_id = ?", int64(uid)).Limit(1).Find(&row).Error; err != nil || row.ID == 0 {
		return nil
	}
	return &row
}

// Popup 登录弹窗：重要且未读的公告，置顶优先，最多 2 条。
func (s *Service) Popup(ctx context.Context, uid idgen.ID) ([]PopupItem, error) {
	var rows []Notice
	err := s.visible(ctx, uid).Where("level = ?", "important").Where("id NOT IN (?)", s.readSub(uid)).
		Order("pinned DESC").Order("published_at DESC").Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PopupItem, 0, len(rows))
	for _, n := range rows {
		out = append(out, PopupItem{ID: n.ID, Title: n.Title, Content: n.Content, PublishedAt: n.PublishedAt, Type: n.Type, Level: n.Level})
	}
	return out, nil
}

// UnreadCount 未读公告数（铃铛红点用）。
func (s *Service) UnreadCount(ctx context.Context, uid idgen.ID) (int64, error) {
	var n int64
	err := s.visible(ctx, uid).Where("id NOT IN (?)", s.readSub(uid)).Count(&n).Error
	return n, err
}

// Mine 我的公告分页：置顶在前，其后按发布时间倒序，并带上已读标记。
func (s *Service) Mine(ctx context.Context, uid idgen.ID, keyword, typ string, unread bool, page, size int) ([]MyNotice, int64, error) {
	q := s.visible(ctx, uid)
	if unread {
		q = q.Where("id NOT IN (?)", s.readSub(uid))
	}
	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	if keyword != "" {
		q = q.Where("title LIKE ?", likeArg(keyword))
	}
	q = q.Session(&gorm.Session{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Notice
	if err := q.Order("pinned DESC").Order("published_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(rows))
	for i, n := range rows {
		ids[i] = int64(n.ID)
	}
	var readIDs []int64
	if len(ids) > 0 {
		s.db.Table("sys_notice_read").Where("user_id = ? AND notice_id IN ?", int64(uid), ids).Pluck("notice_id", &readIDs)
	}
	isRead := map[int64]bool{}
	for _, v := range readIDs {
		isRead[v] = true
	}
	out := make([]MyNotice, 0, len(rows))
	for _, n := range rows {
		out = append(out, MyNotice{ID: n.ID, Title: n.Title, Type: n.Type, Level: n.Level, Content: n.Content, Pinned: n.Pinned,
			Read: isRead[int64(n.ID)], PublishedAt: n.PublishedAt, Attachments: attachmentsJSON(n.Attachments)})
	}
	return out, total, nil
}

// MarkRead 标记已读（幂等）。只能标记自己可见的公告。
func (s *Service) MarkRead(ctx context.Context, uid, id idgen.ID) error {
	var visible int64
	if err := s.visible(ctx, uid).Where("id = ?", int64(id)).Count(&visible).Error; err != nil {
		return err
	}
	if visible == 0 {
		return response.NotFound("error.not_found")
	}
	exists := func() bool {
		var c int64
		s.db.WithContext(ctx).Model(&Read{}).Where("notice_id = ? AND user_id = ?", int64(id), int64(uid)).Count(&c)
		return c > 0
	}
	if exists() {
		return nil
	}
	err := s.db.WithContext(ctx).Create(&Read{NoticeID: id, UserID: uid, ReadAt: time.Now().UTC()}).Error
	if err != nil && exists() { // 并发重复提交，唯一索引兜底
		return nil
	}
	return err
}
