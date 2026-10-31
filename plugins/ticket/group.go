package ticket

// 工单分组（队列）与自动分派：组内成员按未完结工单数最少优先，
// 新单进入启用了 auto_assign 的第一个组并直接分给该成员；手动转组不覆盖已有负责人。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Group 是工单分组（平台级）。
type Group struct {
	database.PlatformModel
	Name        string `gorm:"size:64;not null" json:"name"`
	Description string `gorm:"size:200;not null;default:''" json:"description"`
	AutoAssign  bool   `gorm:"not null;default:0" json:"autoAssign"`
	Enabled     bool   `gorm:"not null;default:1" json:"enabled"`
	Sort        int    `gorm:"not null;default:0" json:"sort"`
}

func (Group) TableName() string { return "plg_ticket_group" }

// GroupMember 是组内坐席；user_name 冗余，避免列表每行回查用户表。
type GroupMember struct {
	database.PlatformModel
	GroupID  idgen.ID `gorm:"not null;uniqueIndex:uk_plg_ticket_group_member,priority:1" json:"groupId"`
	UserID   idgen.ID `gorm:"not null;uniqueIndex:uk_plg_ticket_group_member,priority:2" json:"userId"`
	UserName string   `gorm:"size:64;not null;default:''" json:"userName"`
}

func (GroupMember) TableName() string { return "plg_ticket_group_member" }

// GroupInput 是新增/修改分组的入参。
type GroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	AutoAssign  bool   `json:"autoAssign"`
	Enabled     bool   `json:"enabled"`
	Sort        int    `json:"sort"`
}

// GroupView 是分组 + 成员，管理弹窗一次取全。
type GroupView struct {
	Group
	Members []GroupMember `json:"members"`
}

func (in *GroupInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || len(in.Name) > 64 || len(in.Description) > 200 {
		return response.BadRequest("error.bad_request")
	}
	if in.Sort < 0 {
		in.Sort = 0
	}
	return nil
}

// ListGroups 按排序返回分组及成员。
func (s *Service) ListGroups(ctx context.Context) ([]GroupView, error) {
	groups := make([]Group, 0, 8)
	if err := s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	out := make([]GroupView, 0, len(groups))
	for _, g := range groups {
		members := make([]GroupMember, 0, 4)
		if err := s.db.WithContext(ctx).Where("group_id = ?", int64(g.ID)).Order("id ASC").Find(&members).Error; err != nil {
			return nil, err
		}
		out = append(out, GroupView{Group: g, Members: members})
	}
	return out, nil
}

// CreateGroup 新增分组。
func (s *Service) CreateGroup(ctx context.Context, in GroupInput) (*Group, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	g := &Group{Name: in.Name, Description: in.Description, AutoAssign: in.AutoAssign, Enabled: in.Enabled, Sort: in.Sort}
	g.ID = id
	if err := s.db.WithContext(ctx).Create(g).Error; err != nil {
		return nil, err
	}
	return g, nil
}

// UpdateGroup 修改分组（先确认存在，避免 RowsAffected 在无变化时误判）；
// 改名后同步工单上的冗余 group_name，避免详情页继续显示旧名。
func (s *Service) UpdateGroup(ctx context.Context, id idgen.ID, in GroupInput) (*Group, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	var g Group
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", int64(id)).Take(&g).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("error.not_found")
			}
			return err
		}
		if err := tx.Model(&Group{}).Where("id = ?", int64(id)).Updates(map[string]any{
			"name": in.Name, "description": in.Description,
			"auto_assign": in.AutoAssign, "enabled": in.Enabled, "sort": in.Sort,
		}).Error; err != nil {
			return err
		}
		if g.Name != in.Name {
			// UpdateColumn 不碰 updated_at：改名是分组的事，不算工单被编辑。
			if err := tx.Model(&Ticket{}).Where("group_id = ?", int64(id)).
				UpdateColumn("group_name", in.Name).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	g.Name, g.Description, g.AutoAssign, g.Enabled, g.Sort = in.Name, in.Description, in.AutoAssign, in.Enabled, in.Sort
	return &g, nil
}

// DeleteGroup 删除分组：成员一起删，组内工单退回未分组，规则里对该组的引用一并清掉。
func (s *Service) DeleteGroup(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var g Group
		if err := tx.Where("id = ?", int64(id)).Take(&g).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("error.not_found")
			}
			return err
		}
		if err := tx.Where("group_id = ?", int64(id)).Delete(&GroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Ticket{}).Where("group_id = ?", int64(id)).
			Updates(map[string]any{"group_id": 0, "group_name": ""}).Error; err != nil {
			return err
		}
		if err := clearRuleGroupRefs(tx, int64(id)); err != nil {
			return err
		}
		return tx.Where("id = ?", int64(id)).Delete(&Group{}).Error
	})
}

// clearRuleGroupRefs 清掉规则里对该组的引用；只剩「转组」这一个动作的规则顺带停用，
// 否则它会带着一个空动作长期显示为启用。
func clearRuleGroupRefs(tx *gorm.DB, groupID int64) error {
	var rules []Rule
	if err := tx.Where("set_group_id = ?", groupID).Find(&rules).Error; err != nil {
		return err
	}
	for _, r := range rules {
		cleared := map[string]any{"set_group_id": 0}
		if r.SetPriority == "" && r.Note == "" {
			cleared["enabled"] = false
		}
		if err := tx.Model(&Rule{}).Where("id = ?", int64(r.ID)).UpdateColumns(cleared).Error; err != nil {
			return err
		}
	}
	return nil
}

const maxGroupMembers = 200

// SetGroupMembers 整份替换组内成员；用户姓名按当前租户用户表补齐（查不到留空）。
func (s *Service) SetGroupMembers(ctx context.Context, groupID idgen.ID, userIDs []idgen.ID) (*GroupView, error) {
	var g Group
	if err := s.db.WithContext(ctx).Where("id = ?", int64(groupID)).Take(&g).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	seen := make(map[idgen.ID]bool, len(userIDs))
	ids := make([]idgen.ID, 0, len(userIDs))
	for _, id := range userIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) > maxGroupMembers {
		return nil, response.BadRequest("error.bad_request")
	}
	names := s.userNames(ctx, ids)
	members := make([]GroupMember, 0, len(ids))
	for _, uid := range ids {
		id, err := idgen.Next()
		if err != nil {
			return nil, err
		}
		members = append(members, GroupMember{GroupID: groupID, UserID: uid, UserName: names[uid]})
		members[len(members)-1].ID = id
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", int64(groupID)).Delete(&GroupMember{}).Error; err != nil {
			return err
		}
		if len(members) == 0 {
			return nil
		}
		return tx.Create(&members).Error
	})
	if err != nil {
		return nil, err
	}
	return &GroupView{Group: g, Members: members}, nil
}

// userNames 批量取用户显示名；用户表不可用（如单测夹具）时留空，不影响分组本身。
func (s *Service) userNames(ctx context.Context, ids []idgen.ID) map[idgen.ID]string {
	out := make(map[idgen.ID]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	var rows []struct {
		ID       idgen.ID `gorm:"column:id"`
		Nickname string   `gorm:"column:nickname"`
		Username string   `gorm:"column:username"`
	}
	if err := s.db.WithContext(ctx).Table("sys_user").
		Select("id, nickname, username").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		s.logWarn("ticket: resolve group member names failed", "err", err.Error())
		return out
	}
	for _, r := range rows {
		if r.Nickname != "" {
			out[r.ID] = truncate(r.Nickname, 64)
		} else {
			out[r.ID] = truncate(r.Username, 64)
		}
	}
	return out
}

// AssignGroup 把工单转到某个分组；groupID=0 表示移出分组。
// 组启用自动分派且工单还没有负责人时，顺手按最少负载分给组内成员。
func (s *Service) AssignGroup(ctx context.Context, ticketNo string, groupID idgen.ID) (*Ticket, error) {
	var g Group
	if groupID != 0 {
		if err := s.db.WithContext(ctx).Where("id = ?", int64(groupID)).Take(&g).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, response.NotFound("error.not_found")
			}
			return nil, err
		}
		if !g.Enabled {
			return nil, response.Conflict("ticket.group_disabled")
		}
	}
	var t Ticket
	assigned := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("ticket_no = ?", strings.TrimSpace(ticketNo)).Take(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("ticket.not_found")
			}
			return err
		}
		updates := map[string]any{"group_id": groupID, "group_name": g.Name}
		if g.AutoAssign && t.AssigneeID == 0 {
			uid, name, err := s.leastLoadedMember(tx, g.ID)
			if err == nil && uid != 0 {
				updates["assignee_id"] = uid
				updates["assignee_name"] = name
				assigned = true
			}
		}
		fromGroup, fromAssignee := t.GroupName, t.AssigneeName
		toAssignee, _ := updates["assignee_name"].(string)
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
			return err
		}
		act := actorFrom(ctx)
		if t.GroupID != groupID || fromGroup != g.Name {
			if err := s.recordActivity(ctx, tx, &t, act, ActGroupChanged, fromGroup, g.Name, ""); err != nil {
				return err
			}
		}
		if assigned && toAssignee != fromAssignee {
			if err := s.recordActivity(ctx, tx, &t, act, ActAssigneeChanged, fromAssignee, toAssignee, ""); err != nil {
				return err
			}
		}
		return tx.Where("id = ?", int64(t.ID)).Take(&t).Error
	})
	if err != nil {
		return nil, err
	}
	if assigned {
		s.notifyAssign(ctx, &t)
	}
	return &t, nil
}

// autoAssignGroup 新单进入第一个启用的自动分派组，并按最少未完结单分给组内成员。
func (s *Service) autoAssignGroup(ctx context.Context, t *Ticket) {
	if t.GroupID != 0 {
		return
	}
	var g Group
	err := s.db.WithContext(ctx).Where("enabled = ? AND auto_assign = ?", true, true).
		Order("sort ASC, id ASC").Take(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	if err != nil {
		s.logWarn("ticket: resolve auto-assign group failed", "err", err.Error())
		return
	}
	// 分派是「发生的事」，不是人做的动作：发起人记系统，否则事后看不出这单怎么落到某人手里。
	oldGroupID, oldGroupName := t.GroupID, t.GroupName
	oldAssigneeID, oldAssigneeName := t.AssigneeID, t.AssigneeName
	updates := map[string]any{"group_id": g.ID, "group_name": g.Name}
	t.GroupID, t.GroupName = g.ID, g.Name
	if t.AssigneeID == 0 {
		uid, name, err := s.leastLoadedMember(s.db.WithContext(ctx), g.ID)
		if err == nil && uid != 0 {
			updates["assignee_id"] = uid
			updates["assignee_name"] = name
			t.AssigneeID, t.AssigneeName = uid, name
		}
	}
	if err := s.db.WithContext(ctx).Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
		s.logWarn("ticket: auto assign update failed", "err", err.Error())
		return
	}
	act := systemActor("auto assign")
	if oldGroupID != t.GroupID {
		if err := s.recordActivity(ctx, nil, t, act, ActGroupChanged, oldGroupName, t.GroupName, ""); err != nil {
			s.logWarn("ticket: record auto group failed", "err", err.Error())
		}
	}
	if oldAssigneeID != t.AssigneeID {
		if err := s.recordActivity(ctx, nil, t, act, ActAssigneeChanged, oldAssigneeName, t.AssigneeName, ""); err != nil {
			s.logWarn("ticket: record auto assign failed", "err", err.Error())
		}
	}
	if t.AssigneeID != 0 {
		s.notifyAssign(ctx, t)
	}
}

// leastLoadedMember 取组内未完结工单最少的成员；并列时取 user_id 较小者，保证可复现。
func (s *Service) leastLoadedMember(db *gorm.DB, groupID idgen.ID) (idgen.ID, string, error) {
	var row struct {
		UserID   idgen.ID `gorm:"column:user_id"`
		UserName string   `gorm:"column:user_name"`
		Load     int64    `gorm:"column:load_count"`
	}
	err := db.Table("plg_ticket_group_member AS m").
		Select("m.user_id, m.user_name, COUNT(t.id) AS load_count").
		Joins("LEFT JOIN plg_ticket t ON t.assignee_id = m.user_id AND t.status IN ?",
			[]string{StatusOpen, StatusProcessing, StatusPending}).
		Where("m.group_id = ?", int64(groupID)).
		Group("m.user_id, m.user_name").
		Order("load_count ASC, m.user_id ASC").
		Limit(1).Scan(&row).Error
	return row.UserID, row.UserName, err
}
