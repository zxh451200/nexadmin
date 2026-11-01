package ticket

// 自动化规则：新建工单时按条件执行动作（改优先级、转组、加内部备注、追加标签）。
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
	// 标签条件/动作在库里跟 plg_ticket.tags 同一种存储——「,a,b,」包裹串（见 canonicalTags），
	// 边界逗号让 match_tags=vip 不会命中 viptag，也让筛选口径与工单侧一致；
	// 对外一律用下面的数组字段（AfterFind 拆回，与 Ticket 同一套路）。
	MatchTagsRaw string   `gorm:"column:match_tags;size:255;not null;default:''" json:"-"`
	MatchTags    []string `gorm:"-" json:"matchTags"`
	AddTagsRaw   string   `gorm:"column:add_tags;size:255;not null;default:''" json:"-"`
	AddTags      []string `gorm:"-" json:"addTags"`
}

func (Rule) TableName() string { return "plg_ticket_rule" }

// AfterFind 把标签存储串拆回数组：规则列表与引擎读到的都是同一份口径。
func (r *Rule) AfterFind(*gorm.DB) error {
	r.MatchTags = splitTags(r.MatchTagsRaw)
	r.AddTags = splitTags(r.AddTagsRaw)
	return nil
}

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
	MatchTags     []string `json:"matchTags"`
	AddTags       []string `json:"addTags"`
}

func (in *RuleInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.MatchPriority = strings.TrimSpace(in.MatchPriority)
	in.MatchBizType = strings.TrimSpace(in.MatchBizType)
	in.MatchKeyword = strings.TrimSpace(in.MatchKeyword)
	in.SetPriority = strings.TrimSpace(in.SetPriority)
	in.Note = strings.TrimSpace(in.Note)
	// 标签条件/动作直接复用工单标签的规范器（trim + 丢空 + 去重 + 不含逗号 + 每个 ≤20 rune +
	// 总量 ≤10），规则侧与人工 SetTags 侧才不会长成两套限制。
	matchTags, err := normalizeTags(in.MatchTags)
	if err != nil {
		return err
	}
	addTags, err := normalizeTags(in.AddTags)
	if err != nil {
		return err
	}
	in.MatchTags, in.AddTags = matchTags, addTags
	switch {
	case in.Name == "" || len(in.Name) > 64:
		return response.BadRequest("error.bad_request")
	case len(in.MatchBizType) > 32 || len(in.MatchKeyword) > 64 || len(in.Note) > 500:
		return response.BadRequest("error.bad_request")
	case in.MatchPriority != "" && !ValidPriority(in.MatchPriority):
		return response.BadRequest("error.bad_request")
	case in.SetPriority != "" && !ValidPriority(in.SetPriority):
		return response.BadRequest("error.bad_request")
	// add_tags 也是动作：只加标签的规则同样合法，否则「命中即打标」这种 trigger 做不出来。
	case in.SetPriority == "" && in.SetGroupID == 0 && in.Note == "" && len(in.AddTags) == 0:
		return response.BadRequest("error.bad_request")
	}
	if in.Sort < 0 {
		in.Sort = 0
	}
	return nil
}

// ticketTagSet 取工单当前标签集合：raw 是库里的「,a,b,」串，TagList 是内存态数组，两份都读。
// 建单链路里的 Ticket 是代码手拼的，AfterFind 不一定跑过，只读一份会让规则静默不命中。
func ticketTagSet(t Ticket) map[string]bool {
	have := make(map[string]bool, len(t.TagList)+4)
	for _, tag := range append(splitTags(t.Tags), t.TagList...) {
		have[tag] = true
	}
	return have
}

// tagsDisplay 把「,a,b,」存储串转成动态里用的显示串（逗号分隔，无边界逗号）——
// 与人工 SetTags 落动态时的写法保持一致，前端不必分两种格式解析。
func tagsDisplay(raw string) string {
	return strings.Join(splitTags(raw), ",")
}

// mergeRuleTags 把规则的 add_tags 追加到工单已有标签尾部：原有顺序不动、已存在的跳过、
// 总量截到 maxTags（超出部分进 dropped，由调用方记日志）。
// 为什么原有标签优先、且截断不报错：规则作者看不到工单当前挂了几个标签，
// 若因为「已经有 10 个」而拒绝，命中动作会整条失败——而规则引擎的口径是失败只影响这条规则，
// 绝不拖垮建单；已有标签是人工或前序规则打上的，被规则挤掉还会连带让标签筛选失准。
func mergeRuleTags(raw string, add []string) (merged []string, dropped []string) {
	existing := splitTags(raw)
	merged = make([]string, 0, len(existing)+len(add))
	seen := make(map[string]bool, len(existing)+len(add))
	for _, tag := range append(existing, add...) {
		if tag == "" || seen[tag] {
			continue
		}
		if len(merged) >= maxTags {
			dropped = append(dropped, tag)
			continue
		}
		seen[tag] = true
		merged = append(merged, tag)
	}
	return merged, dropped
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
	// match_tags 多个标签之间取「与」（全部命中才算命中）：
	// 1) 其余条件（优先级/业务类型/关键词）本来就是全 AND，单给标签开 OR 会让一条规则
	//    有两种结合方向，读规则的人猜不到语义，命中率排查成本直接翻倍；
	// 2) 规则是「全部执行、命中即动作」，所以要 OR 写两条同动作规则就行——AND 能用多条拼出 OR，
	//    反过来（在单条规则里用 AND 拼出 OR）拼不出来，AND 才是可组合的那一侧；
	// 3) 触发器是批量自动化，宁可少命中：OR 会让「vip 或 refund」把无关单也打上标，
	//    而这些标签正是列表筛选与后续规则的条件，污染面比漏命中大得多。
	if len(r.MatchTags) > 0 {
		have := ticketTagSet(t)
		for _, tag := range r.MatchTags {
			if !have[tag] {
				return false
			}
		}
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
		MatchTagsRaw: canonicalTags(in.MatchTags), MatchTags: in.MatchTags,
		AddTagsRaw: canonicalTags(in.AddTags), AddTags: in.AddTags,
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
		"match_tags": canonicalTags(in.MatchTags), "add_tags": canonicalTags(in.AddTags),
	}
	if err := s.db.WithContext(ctx).Model(&Rule{}).Where("id = ?", int64(id)).Updates(updates).Error; err != nil {
		return nil, err
	}
	r.Name, r.Enabled, r.Sort = in.Name, in.Enabled, in.Sort
	r.MatchPriority, r.MatchBizType, r.MatchKeyword = in.MatchPriority, in.MatchBizType, in.MatchKeyword
	r.SetPriority, r.SetGroupID, r.Note = in.SetPriority, in.SetGroupID, in.Note
	r.MatchTagsRaw, r.AddTagsRaw = canonicalTags(in.MatchTags), canonicalTags(in.AddTags)
	r.MatchTags, r.AddTags = in.MatchTags, in.AddTags
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
// 注意 match_tags 的命中前提是「此刻工单已有标签」：建单入参不带标签，所以今天只有
// 前序规则的 add_tags（按 sort、id 顺序）能让后一条规则的 match_tags 命中；
// 人工 SetTags 后是否要重跑规则属于服务层口径，不在这里扩展。
// applyRules 按工单当前内容跑一遍规则：每条规则只跑一次、每次调用只跑一轮。
// withNotes=false 用在「人工改标签后重跑」：内部备注是建单那一下的沟通动作，
// 每次改标签都补一条会把时间轴刷成噪音。
// 重跑不会递归——规则改优先级/转组/加标签都直接写库，不经过 SetTags。
func (s *Service) applyRules(ctx context.Context, t *Ticket, withNotes bool) {
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
		before := *t
		updates := map[string]any{}
		// 先定组、再算时钟：规则可能把这张单转到另一个队列，而队列绑定的那套策略才是它真正适用的 SLA。
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
		if r.SetPriority != "" && r.SetPriority != t.Priority {
			updates["priority"] = r.SetPriority
			sla := s.slaFor(ctx, int64(t.GroupID))
			target := sla.TargetFor(r.SetPriority)
			loc := s.location()
			now := time.Now().UTC()
			updates["due_at"] = businessDue(now, target.FirstMinutes, sla, loc)
			updates["resolution_due_at"] = businessDue(now, target.ResolutionMinutes, sla, loc)
			t.Priority = r.SetPriority
		}
		// 追加标签：与人工 SetTags 走同一列、同一存储串，规则写完再读回来也能被后序规则的 match_tags 命中。
		if len(r.AddTags) > 0 {
			merged, dropped := mergeRuleTags(t.Tags, r.AddTags)
			if dropped != nil {
				s.logWarn("ticket: rule tags quota exceeded", "rule", r.Name, "dropped", strings.Join(dropped, ","))
			}
			if canonical := canonicalTags(merged); canonical != t.Tags {
				updates["tags"] = canonical
				t.Tags, t.TagList = canonical, merged
			}
		}
		if len(updates) > 0 {
			if err := s.db.WithContext(ctx).Model(&Ticket{}).Where("id = ?", int64(t.ID)).Updates(updates).Error; err != nil {
				s.logWarn("ticket: rule apply failed", "rule", r.Name, "err", err.Error())
				continue
			}
			changed = true
			s.recordRuleEffects(ctx, t, before, *t, r.Name)
		}
		if withNotes && r.Note != "" {
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

// recordRuleEffects 把规则改写的字段逐条落进动态，再补一条「命中规则 X」：
// 只记规则名看不出后果，只记后果看不出是谁干的。记失败不影响建单，与规则本身同一口径。
func (s *Service) recordRuleEffects(ctx context.Context, t *Ticket, before, after Ticket, ruleName string) {
	act := systemActor("rule")
	rules := []struct {
		action       string
		from, to     string
		changedField bool
	}{
		{ActPriorityChanged, before.Priority, after.Priority, before.Priority != after.Priority},
		{ActGroupChanged, before.GroupName, after.GroupName, before.GroupID != after.GroupID},
		{ActAssigneeChanged, before.AssigneeName, after.AssigneeName, before.AssigneeID != after.AssigneeID},
		// 标签与人工 SetTags 的动态同一形态（前后都是逗号拼接的显示串），前端字典不用分场景。
		{ActTagsChanged, tagsDisplay(before.Tags), tagsDisplay(after.Tags), before.Tags != after.Tags},
	}
	for _, item := range rules {
		if !item.changedField {
			continue
		}
		if err := s.recordActivity(ctx, nil, t, act, item.action, item.from, item.to, ruleName); err != nil {
			s.logWarn("ticket: record rule activity failed", "rule", ruleName, "action", item.action, "err", err.Error())
		}
	}
	if err := s.recordActivity(ctx, nil, t, act, ActRuleApplied, "", ruleName, ""); err != nil {
		s.logWarn("ticket: record rule hit failed", "rule", ruleName, "err", err.Error())
	}
}
