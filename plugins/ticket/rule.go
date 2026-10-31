package ticket

// 自动化规则：新建工单时按条件执行动作（改优先级、转组、加内部备注）。
// 规则按 sort、id 顺序全部执行；后执行的规则看到前一条的结果（后写覆盖）。

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// maxBizTypeOptions 是候选业务类型的返回上限（按字母序截断）。
const maxBizTypeOptions = 50

// Rule 是一条自动化规则。
type Rule struct {
	database.PlatformModel
	Name          string   `gorm:"size:64;not null" json:"name"`
	Enabled       bool     `gorm:"not null;default:1" json:"enabled"`
	Sort          int      `gorm:"not null;default:0" json:"sort"`
	MatchPriority string   `gorm:"size:16;not null;default:''" json:"matchPriority"`
	MatchBizType  string   `gorm:"size:32;not null;default:''" json:"matchBizType"`
	MatchKeyword  string   `gorm:"size:64;not null;default:''" json:"matchKeyword"`
	SetPriority   string   `gorm:"size:16;not null;default:''" json:"setPriority"`
	SetGroupID    idgen.ID `gorm:"not null;default:0" json:"setGroupId"`
	Note          string   `gorm:"size:500;not null;default:''" json:"note"`
}

func (Rule) TableName() string { return "plg_ticket_rule" }

// RuleInput 是新增/修改规则的入参。
type RuleInput struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Sort          int      `json:"sort"`
	MatchPriority string   `json:"matchPriority"`
	MatchBizType  string   `json:"matchBizType"`
	MatchKeyword  string   `json:"matchKeyword"`
	SetPriority   string   `json:"setPriority"`
	SetGroupID    idgen.ID `json:"setGroupId"`
	Note          string   `json:"note"`
}

func (in *RuleInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.MatchPriority = strings.TrimSpace(in.MatchPriority)
	in.MatchBizType = strings.TrimSpace(in.MatchBizType)
	in.MatchKeyword = strings.TrimSpace(in.MatchKeyword)
	in.SetPriority = strings.TrimSpace(in.SetPriority)
	in.Note = strings.TrimSpace(in.Note)
	switch {
	case in.Name == "" || len(in.Name) > 64:
		return response.BadRequest("error.bad_request")
	case len(in.MatchBizType) > 32 || len(in.MatchKeyword) > 64 || len(in.Note) > 500:
		return response.BadRequest("error.bad_request")
	case in.MatchPriority != "" && !ValidPriority(in.MatchPriority):
		return response.BadRequest("error.bad_request")
	case in.SetPriority != "" && !ValidPriority(in.SetPriority):
		return response.BadRequest("error.bad_request")
	case in.SetPriority == "" && in.SetGroupID == 0 && in.Note == "":
		return response.BadRequest("error.bad_request")
	}
	if in.Sort < 0 {
		in.Sort = 0
	}
	return nil
}

// ruleMatches 判断规则条件是否命中新建工单（空条件 = 不限）。
func ruleMatches(r Rule, t Ticket) bool {
	if r.MatchPriority != "" && r.MatchPriority != t.Priority {
		return false
	}
	if r.MatchBizType != "" && r.MatchBizType != t.BizType {
		return false
	}
	if r.MatchKeyword != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(r.MatchKeyword)) {
		return false
	}
	return true
}

// ListRules 按 sort、id 升序返回全部规则（管理页与引擎共用）。
func (s *Service) ListRules(ctx context.Context) ([]Rule, error) {
	rows := make([]Rule, 0, 8)
	err := s.db.WithContext(ctx).Order("sort ASC, id ASC").Find(&rows).Error
	return rows, err
}

// BizTypeOptions 返回可直接用于规则条件的业务类型：已有工单用过的 + 既有规则写过的，去重排序。
// 自由文本填错只会静默不命中，所以候选值由数据派生，不另建字典表。
func (s *Service) BizTypeOptions(ctx context.Context) ([]string, error) {
	var used, configured []string
	if err := s.db.WithContext(ctx).Model(&Ticket{}).
		Where("biz_type <> ?", "").Distinct().Limit(maxBizTypeOptions).Pluck("biz_type", &used).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&Rule{}).
		Where("match_biz_type <> ?", "").Distinct().Limit(maxBizTypeOptions).Pluck("match_biz_type", &configured).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(used)+len(configured))
	out := make([]string, 0, len(used)+len(configured))
	for _, v := range append(used, configured...) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

// CreateRule 新增规则。
func (s *Service) CreateRule(ctx context.Context, in RuleInput) (*Rule, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if err := s.checkRuleGroup(ctx, in.SetGroupID); err != nil {
		return nil, err
	}
	id, err := idgen.Next()
	if err != nil {
		return nil, err
	}
	r := &Rule{
		Name: in.Name, Enabled: in.Enabled, Sort: in.Sort,
		MatchPriority: in.MatchPriority, MatchBizType: in.MatchBizType, MatchKeyword: in.MatchKeyword,
		SetPriority: in.SetPriority, SetGroupID: in.SetGroupID, Note: in.Note,
	}
	r.ID = id
	if err := s.db.WithContext(ctx).Create(r).Error; err != nil {
		return nil, err
	}
	return r, nil
}

// UpdateRule 修改规则。
func (s *Service) UpdateRule(ctx context.Context, id idgen.ID, in RuleInput) (*Rule, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	if err := s.checkRuleGroup(ctx, in.SetGroupID); err != nil {
		return nil, err
	}
	var r Rule
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).Take(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("error.not_found")
		}
		return nil, err
	}
	updates := map[string]any{
		"name": in.Name, "enabled": in.Enabled, "sort": in.Sort,
		"match_priority": in.MatchPriority, "match_biz_type": in.MatchBizType, "match_keyword": in.MatchKeyword,
		"set_priority": in.SetPriority, "set_group_id": in.SetGroupID, "note": in.Note,
	}
	if err := s.db.WithContext(ctx).Model(&Rule{}).Where("id = ?", int64(id)).Updates(updates).Error; err != nil {
		return nil, err
	}
	r.Name, r.Enabled, r.Sort = in.Name, in.Enabled, in.Sort
	r.MatchPriority, r.MatchBizType, r.MatchKeyword = in.MatchPriority, in.MatchBizType, in.MatchKeyword
	r.SetPriority, r.SetGroupID, r.Note = in.SetPriority, in.SetGroupID, in.Note
	return &r, nil
}

// DeleteRule 删除规则。
func (s *Service) DeleteRule(ctx context.Context, id idgen.ID) error {
	res := s.db.WithContext(ctx).Where("id = ?", int64(id)).Delete(&Rule{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return response.NotFound("error.not_found")
	}
	return nil
}

func (s *Service) checkRuleGroup(ctx context.Context, groupID idgen.ID) error {
	if groupID == 0 {
		return nil
	}
	var g Group
	if err := s.db.WithContext(ctx).Where("id = ?", int64(groupID)).Take(&g).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("error.not_found")
		}
		return err
	}
	// 停用组不会被命中（applyRules 只认启用的组），存进去只会变成静默空转的规则，写入时就拒。
	if !g.Enabled {
		return response.Conflict("ticket.group_disabled")
	}
	return nil
}

// applyRules 对新建工单执行所有命中规则；失败只记日志，不影响建单。
func (s *Service) applyRules(ctx context.Context, t *Ticket) {
	rules, err := s.ListRules(ctx)
	if err != nil {
		s.logWarn("ticket: load rules failed", "err", err.Error())
		return
	}
	if len(rules) == 0 {
		return
	}
	assigned := false
	changed := false
	for _, r := range rules {
		if !r.Enabled || !ruleMatches(r, *t) {
			continue
		}
		updates := map[string]any{}
		if r.SetPriority != "" && r.SetPriority != t.Priority {
			updates["priority"] = r.SetPriority
			target := s.CurrentSLA().TargetFor(r.SetPriority)
			loc := s.location()
			now := time.Now().UTC()
			updates["due_at"] = businessDue(now, target.FirstMinutes, s.CurrentSLA(), loc)
			updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, s.CurrentSLA(), loc)
			t.Priority = r.SetPriority
		}
		if r.SetGroupID != 0 {
			var g Group
			if err := s.db.WithContext(ctx).Where("id = ?", int64(r.SetGroupID)).Take(&g).Error; err == nil && g.Enabled {
				updates["group_id"] = g.ID
				updates["group_name"] = g.Name
				t.GroupID, t.GroupName = g.ID, g.Name
				if g.AutoAssign && t.AssigneeID == 0 {
					if uid, name, err := s.leastLoadedMember(s.db.WithContext(ctx), g.ID); err == nil && uid != 0 {
						updates["assignee_id"] = uid
						updates["assignee_name"] = name
						t.AssigneeID, t.AssigneeName = uid, name
						assigned = true
					}
				}
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				s.logWarn("ticket: rule group lookup failed", "rule", r.Name, "err", err.Error())
			}
		}
		if len(updates) > 0 {
			if err := s.db.WithContext(ctx).Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
				s.logWarn("ticket: rule apply failed", "rule", r.Name, "err", err.Error())
				continue
			}
			changed = true
		}
		if r.Note != "" {
			if _, _, err := s.Reply(ctx, t.TicketNo, ReplyInput{
				ReplierType: ReplierSystem, ReplierName: "rule", Content: r.Note, Internal: true,
			}); err != nil {
				s.logWarn("ticket: rule note failed", "rule", r.Name, "err", err.Error())
			}
			changed = true
		}
	}
	if changed {
		var fresh Ticket
		if err := s.db.WithContext(ctx).Where("id = ?", int64(t.ID)).Take(&fresh).Error; err == nil {
			*t = fresh
		}
	}
	if assigned {
		s.notifyAssign(ctx, t)
	}
}
