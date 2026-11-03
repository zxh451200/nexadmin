package ticket

// 合并重复工单 + 关联单（Zendesk / Freshdesk / Intercom 的标配能力）。
//
// 表结构为什么是「一张新表 + 一个可空列」：
//   - 被合并方的「合并进了哪张」是一次性事实 → plg_ticket.merged_into_id 可空列。
//     一次写入、永不改写，反查「这张主单吞了哪些单」就是一条 WHERE，不值得再开一张表。
//   - 关联单是双向、可解除的多对多 → 新表 plg_ticket_link，按（较小 id，较大 id）规范化只存一行：
//     唯一键天然去重，两个方向命中同一个索引形状，解除就是删那一行。
//   - 已有表的既有列一个不动；主单的「合并了 N 张」不建计数列，按 merged_into_id 即时反查后
//     落进动态轨迹，免得再多一个会漂移的冗余计数（回复、改状态、批量操作那些路径都不会同步它）。
//
// 被合并方的回复与附件一行都不搬：原地保留才可追溯。「不再活跃」由终态 + 指针表达，
// 不由数据搬迁表达——搬走会丢掉「这条回复原本挂在哪张单上」，还会让附件的下载定位
// （工单号 + 不可枚举 token 双要素，见 attachment.go）当场失效。
//
// 边界（每条都有用例，见 merge_test.go）：
//   - 自合并、自关联：400，且不碰数据库。
//   - 被合并方已经合并过（指针非空）：409 拒绝，不改写历史。选「拒绝」而不是「改指向最终主单」，
//     因为那张单的回复与附件正是靠这一个指针说明「对话接着去哪儿聊」，重写它等于偷偷搬历史，
//     还会让旧主单的「合并了 N 张」计数与动态对不上；要换目标请先在最终主单上操作。
//   - 主单必须仍在活跃态（open/processing/pending）：已解决、已关闭的单不接受合并，409。
//     合并的语义是「把重复的并到还活着的那条对话上」，指向一条已终结的单只会让客户看到
//     一个死胡同；关闭态需要复用时应当由坐席显式重开，而不是借合并顺带复活。
//   - 主单自己也被合并过（重开过的旧副本，见下面那条已知交互）：顺着 merged_into_id 找到最终主单，
//     不新建第二个头。链路解析只读不改，所以不算改写历史。
//   - 环：沿链路走过的节点若回到被合并方自身（或链路中途成环 / 超过 hop 上限），409 拒绝。
//     环在正常规则下走不出来，但「回复会把 closed 工单重开为 processing」这条既有路径能让一张
//     已合并的单重新变活跃，于是 B→A 与 A→B 可以凑成环——这个守卫是必需的，不是摆设。
//   - 并发反向合并（A→B 与 B→A 同时到）：两张单按工单号定序加锁（与 im 的会话合并同一手法），
//     每个事务只会去拿「比已持有更大的行」，等待图不成环，所以不死锁；后到的那个看到已提交的
//     终态/指针后按上面的规则被拒。真库并发用例见 TestITMySQLTicketMerge。
//
// 已知交互（不在本次写域，需后续在服务层收口）：报告人回复会把已关闭工单重开为 processing，
// 但没人清 merged_into_id，所以重开后的旧副本仍是「被合并过的单」。上面的链路解析与环检测
// 就是为这个状态准备的兜底；若要根治，请让 Reply/SetStatus 在重开时清掉该列（改 service.go/model.go）。
//
// 动态轨迹复用 activity.go 的 recordActivity：合并/解绑与动态行同事务，审计不会比变更先失败或后失败。
// 新动作码在本文件里定义（不改 activity.go），公开视图 publicActivityActions 不含它们，
// 因此客户自助页看不到内部协作细节。

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 新增动作码（稳定契约，待登记进 activity.go 的动作码常量与前端字典）。
const (
	// ActMergedInto 记在被合并方上：它被并进了哪张单（toValue 落终态，detail 是主单号）。
	ActMergedInto = "merged_into"
	// ActMergedTickets 记在主单上：合并后它累计吞并了几张（toValue 是计数，detail 是本次被合并的单号）。
	ActMergedTickets = "merged_tickets"
	// ActLinked 记在关联双方上：与哪张单建立关联。
	ActLinked = "linked"
	// ActUnlinked 记在解除关联的双方上。
	ActUnlinked = "unlinked"
)

// 关联单的读与写边界。
const (
	// maxMergeHops 是主单链最多走几跳：正常数据一两跳足够，超限说明链路被写坏，直接判环而不是死循环。
	maxMergeHops = 32
	// maxLinksPerTicket 是单张工单的关联上限，防「一键关联全部」把面板刷爆。
	maxLinksPerTicket = 50
	// linkListLimit 是一次读取的关联条数上限（与 activityLimit 同一取向：长尾取最近这些）。
	linkListLimit = 100
)

// 关联列表里条目的来源，前端按它分组展示。
const (
	KindLink       = "link"        // 双向关联单
	KindMergedInto = "merged_into" // 本单被并进了那张
	KindMergedFrom = "merged_from" // 并到本单上来的那些重复单
)

// TicketLink 是一条双向关联，规范化存一行：TicketAID < TicketBID。
type TicketLink struct {
	database.TenantModel
	TicketAID idgen.ID `gorm:"column:ticket_a_id;not null;uniqueIndex:uk_plg_ticket_link_pair,priority:1" json:"ticketAId"`
	TicketBID idgen.ID `gorm:"column:ticket_b_id;not null;uniqueIndex:uk_plg_ticket_link_pair,priority:2" json:"ticketBId"`
	// LinkBy 是建关联的坐席显示名，冗余一份省得回查用户表。
	LinkBy string `gorm:"column:link_by;size:64;not null;default:''" json:"linkBy"`
}

func (TicketLink) TableName() string { return "plg_ticket_link" }

// mergeTicket 是合并/关联路径对 plg_ticket 的投影：只取这几条路径真正要读的列，
// 其中 merged_into_id 是本次新增的可空列，model.go 的 Ticket 里没有它（本次写域不改核心模型），
// 所以自带 TableName 让 GORM 落在 plg_ticket 上，而不是为这一个字段去动 Ticket。
type mergeTicket struct {
	ID idgen.ID `gorm:"column:id;primaryKey"`
	// TenantID 不读不用，但必须声明：租户插件是按 schema 里有没有 tenant_id 字段决定要不要
	// 追加条件的（core/database/model.go 的 tenantScope）。这个窄投影漏掉它，合并与关联
	// 就会静默地跨租户读写 plg_ticket —— 五张表里唯独这条没有模型兜底。
	TenantID        idgen.ID   `gorm:"column:tenant_id"`
	TicketNo        string     `gorm:"column:ticket_no"`
	BizType         string     `gorm:"column:biz_type"`
	BizNo           string     `gorm:"column:biz_no"`
	Title           string     `gorm:"column:title"`
	Status          string     `gorm:"column:status"`
	Priority        string     `gorm:"column:priority"`
	MergedInto      *int64     `gorm:"column:merged_into_id"`
	ClosedAt        *time.Time `gorm:"column:closed_at"`
	ResolutionDueAt *time.Time `gorm:"column:resolution_due_at"`
	SlaPausedAt     *time.Time `gorm:"column:sla_paused_at"`
}

func (mergeTicket) TableName() string { return "plg_ticket" }

// mergedIntoID 取被合并指针，没有则 0。
func (t *mergeTicket) mergedIntoID() idgen.ID {
	if t == nil || t.MergedInto == nil {
		return 0
	}
	return idgen.ID(*t.MergedInto)
}

// LinkedTicket 是关联列表里的一张单。
type LinkedTicket struct {
	TicketNo string    `json:"ticketNo"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	Priority string    `json:"priority"`
	Kind     string    `json:"kind"`
	LinkedAt time.Time `json:"linkedAt"`
}

// MergeResult 是合并结果：被合并的单号、实际生效的主单号（链解析后可能与传入不同）
// 与该主单累计吞并的张数。
type MergeResult struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	MergedCount int64  `json:"mergedCount"`
}

// MergeSummary 是一张单的合并痕迹：它被并进了哪张，以及哪些单并到了它身上。
type MergeSummary struct {
	MergedInto  *LinkedTicket  `json:"mergedInto,omitempty"`
	MergedFrom  []LinkedTicket `json:"mergedFrom"`
	MergedCount int64          `json:"mergedCount"`
}

// ---- 读工单的小工具 ----

// lockMergeTicket 在事务里按工单号加行锁读一行；查不到就是工单不存在。
func lockMergeTicket(tx *gorm.DB, ticketNo string) (*mergeTicket, error) {
	return takeMergeTicket(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("ticket_no = ?", ticketNo))
}

// takeMergeTicket 收口「取一行 + 404」，让合并与关联的读路径共用同一句判断。
func takeMergeTicket(tx *gorm.DB) (*mergeTicket, error) {
	var row mergeTicket
	if err := tx.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("ticket.not_found")
		}
		return nil, err
	}
	return &row, nil
}

// getMergeTicketByID 按 id 读一行（不加锁）：链路解析只读 hops，不给异常数据扩大锁面。
func getMergeTicketByID(tx *gorm.DB, id idgen.ID) (*mergeTicket, error) {
	return takeMergeTicket(tx.Where("id = ?", int64(id)))
}

// ticketRef 用投影行拼一个 recordActivity 需要的 *Ticket（它只读 ID 与工单号）。
func ticketRef(t *mergeTicket) *Ticket {
	row := &Ticket{TicketNo: t.TicketNo, Title: t.Title, Status: t.Status, Priority: t.Priority}
	row.ID = t.ID
	return row
}

// ---- 合并 ----

// Merge 把 fromNo 这张重复工单合并进 toNo：被合并方进入 closed 终态并记下指向主单的指针，
// 它的回复与附件原地保留（可追溯、不再活跃）；主单落一条「合并了 N 张」的动态。
// 主单若自己也被合并过（重开过的旧副本），顺指针解析到最终主单，不新建第二个头。
func (s *Service) Merge(ctx context.Context, fromNo, toNo, operator string) (*MergeResult, error) {
	fromNo, toNo = strings.TrimSpace(fromNo), strings.TrimSpace(toNo)
	switch {
	case fromNo == "" || toNo == "":
		return nil, response.BadRequest("error.bad_request")
	case fromNo == toNo:
		return nil, response.BadRequest("ticket.merge_self")
	}
	// 对侧单号在请求体里，中间件只挡得住 :no。合并会把本单的回复与附件并进目标单，
	// 目标单不在请求者范围内就等于往看不见的单里塞内容，所以这里补一次校验。
	if err := s.guardNo(ctx, toNo); err != nil {
		return nil, err
	}

	var out MergeResult
	var src *mergeTicket
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 定序加锁：并发反向合并（A→B 与 B→A）因此串行而不是死锁，后到的按规则被拒。
		first, second := fromNo, toNo
		if first > second {
			first, second = second, first
		}
		a, err := lockMergeTicket(tx, first)
		if err != nil {
			return err
		}
		b, err := lockMergeTicket(tx, second)
		if err != nil {
			return err
		}
		dst := b
		src = a
		if first != fromNo {
			src, dst = b, a
		}
		// 被合并方必须没被合并过：指针是一次性事实，改写它等于搬历史。
		if src.mergedIntoID() != 0 {
			return response.Conflict("ticket.already_merged")
		}
		root, err := resolveMergeRoot(tx, dst)
		if err != nil {
			return err
		}
		// 主单必须还活着：已解决/已关闭的单不接受合并。
		if !Settleable(root.Status) {
			return response.Conflict("ticket.merge_target_terminal")
		}
		// 兜底：链解析后落到自己头上就是环（正常数据走不到，异常/并发态必须拦住）。
		if root.ID == src.ID {
			return response.Conflict("ticket.merge_cycle")
		}

		now := time.Now().UTC()
		updates := map[string]any{
			"merged_into_id": int64(root.ID),
			"status":         StatusClosed,
			"closed_at":      now,
			"sla_paused_at":  nil,
		}
		// 与 SetStatus(closed) 同一口径：合并掉的那张也要留下解决时限的违约事实，
		// 否则清扫器只扫 open/processing，从此再没人补这个标记。
		if resolutionBreached(src.ResolutionDueAt, src.SlaPausedAt, now) {
			updates["resolution_overdue"] = true
		}
		if err := tx.Model(&Ticket{}).Where("id = ?", int64(src.ID)).Updates(updates).Error; err != nil {
			return err
		}

		act := agentActor(ctx, truncate(strings.TrimSpace(operator), 64))
		if err := s.recordActivity(ctx, tx, ticketRef(src), act, ActMergedInto, src.Status, StatusClosed, root.TicketNo); err != nil {
			return err
		}
		// 「合并了 N 张」即时反查，不建计数列：本次更新已提交在事务里，计数含当前这张。
		count, err := countMergedInto(tx, root.ID)
		if err != nil {
			return err
		}
		if err := s.recordActivity(ctx, tx, ticketRef(root), act, ActMergedTickets, "", strconv.FormatInt(count, 10), src.TicketNo); err != nil {
			return err
		}
		out = MergeResult{Source: src.TicketNo, Target: root.TicketNo, MergedCount: count}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 被合并方确实关闭了：复用既有 ticket.closed 事件目录，宿主 webhook 不必新增订阅。
	closed := ticketRef(src)
	closed.Status = StatusClosed
	s.publish(ctx, EventClosed, closed, EventData{SenderType: ReplierAgent, SenderName: truncate(strings.TrimSpace(operator), 64)})
	return &out, nil
}

// resolveMergeRoot 顺着 merged_into_id 找到最终主单。途经节点重复出现或回到起点即判环；
// 超过 maxMergeHops 也按环处理——坏数据不该让这次读无限循环。
func resolveMergeRoot(tx *gorm.DB, start *mergeTicket) (*mergeTicket, error) {
	seen := map[idgen.ID]bool{start.ID: true}
	cur := start
	for hop := 0; hop < maxMergeHops; hop++ {
		next := cur.mergedIntoID()
		if next == 0 {
			return cur, nil
		}
		if seen[next] {
			return nil, response.Conflict("ticket.merge_cycle")
		}
		seen[next] = true
		row, err := getMergeTicketByID(tx, next)
		if err != nil {
			return nil, err
		}
		cur = row
	}
	return nil, response.Conflict("ticket.merge_cycle")
}

// countMergedInto 反查并到某张主单上的工单数（含刚合并进来的那张）。
func countMergedInto(tx *gorm.DB, rootID idgen.ID) (int64, error) {
	var n int64
	if err := tx.Model(&Ticket{}).Where("merged_into_id = ?", int64(rootID)).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// MergeSummary 返回一张单的合并痕迹：被并进了哪张 + 哪些单并到了它身上。
// 对侧被删（悬挂引用）时该条留空/跳过，不让整张详情读失败。
func (s *Service) MergeSummary(ctx context.Context, ticketNo string) (*MergeSummary, error) {
	ticketNo = strings.TrimSpace(ticketNo)
	if ticketNo == "" {
		return nil, response.BadRequest("error.bad_request")
	}
	db := s.db.WithContext(ctx)
	self, err := takeMergeTicket(db.Where("ticket_no = ?", ticketNo))
	if err != nil {
		return nil, err
	}
	out := MergeSummary{MergedFrom: make([]LinkedTicket, 0, 4)}
	if into := self.mergedIntoID(); into != 0 {
		if target, err := getMergeTicketByID(db, into); err == nil {
			item := toLinkedTicket(target, KindMergedInto, mergedAt(target))
			out.MergedInto = &item
		} else if !isNotFound(err) {
			return nil, err
		}
	}
	var rows []mergeTicket
	if err := db.Where("merged_into_id = ?", int64(self.ID)).Order("id ASC").Limit(linkListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out.MergedFrom = append(out.MergedFrom, toLinkedTicket(&rows[i], KindMergedFrom, mergedAt(&rows[i])))
	}
	out.MergedCount = int64(len(out.MergedFrom))
	return &out, nil
}

// ---- 关联单 ----

// linkPairIDs 把两个 id 规范化成（较小，较大），一行存下双向关系。
func linkPairIDs(a, b idgen.ID) (idgen.ID, idgen.ID) {
	if a > b {
		return b, a
	}
	return a, b
}

// Link 建立双向关联（可重复调用会报已关联）；两侧各落一条动态。
// 允许关联终态单：合并与关闭恰恰是最需要「这几张是同一件事」留痕的场景。
func (s *Service) Link(ctx context.Context, ticketNo, otherNo, operator string) (*TicketLink, error) {
	ticketNo, otherNo = strings.TrimSpace(ticketNo), strings.TrimSpace(otherNo)
	switch {
	case ticketNo == "" || otherNo == "":
		return nil, response.BadRequest("error.bad_request")
	case ticketNo == otherNo:
		return nil, response.BadRequest("ticket.link_self")
	}
	// 关联是双向可见的：把范围外的单挂进来，它就会出现本单的关联列表里。
	if err := s.guardNo(ctx, otherNo); err != nil {
		return nil, err
	}
	name := truncate(strings.TrimSpace(operator), 64)

	var link TicketLink
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		first, second := ticketNo, otherNo
		if first > second {
			first, second = second, first
		}
		a, err := lockMergeTicket(tx, first)
		if err != nil {
			return err
		}
		b, err := lockMergeTicket(tx, second)
		if err != nil {
			return err
		}
		lo, hi := linkPairIDs(a.ID, b.ID)
		var dup int64
		if err := tx.Model(&TicketLink{}).Where("ticket_a_id = ? AND ticket_b_id = ?", int64(lo), int64(hi)).Count(&dup).Error; err != nil {
			return err
		}
		if dup > 0 {
			return response.Conflict("ticket.link_exists")
		}
		// 上限按两个端点各自数：关联是对称的，任何一侧爆量都不该继续加。
		for _, id := range [2]idgen.ID{lo, hi} {
			n, err := countLinks(tx, id)
			if err != nil {
				return err
			}
			if n >= maxLinksPerTicket {
				return response.BadRequest("ticket.link_limit")
			}
		}
		link = TicketLink{TicketAID: lo, TicketBID: hi, LinkBy: name}
		if err := tx.Create(&link).Error; err != nil {
			return err
		}
		act := agentActor(ctx, name)
		if err := s.recordActivity(ctx, tx, ticketRef(a), act, ActLinked, "", b.TicketNo, ""); err != nil {
			return err
		}
		if err := s.recordActivity(ctx, tx, ticketRef(b), act, ActLinked, "", a.TicketNo, ""); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// Unlink 解除双向关联（删那一行规范化记录），两侧各落一条动态。
func (s *Service) Unlink(ctx context.Context, ticketNo, otherNo, operator string) error {
	ticketNo, otherNo = strings.TrimSpace(ticketNo), strings.TrimSpace(otherNo)
	switch {
	case ticketNo == "" || otherNo == "":
		return response.BadRequest("error.bad_request")
	case ticketNo == otherNo:
		return response.BadRequest("ticket.link_self")
	}
	// 解除关联会给两侧各落一条动态，对侧在范围外同样不该被改。
	if err := s.guardNo(ctx, otherNo); err != nil {
		return err
	}
	name := truncate(strings.TrimSpace(operator), 64)

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := takeMergeTicket(tx.Where("ticket_no = ?", ticketNo))
		if err != nil {
			return err
		}
		b, err := takeMergeTicket(tx.Where("ticket_no = ?", otherNo))
		if err != nil {
			return err
		}
		lo, hi := linkPairIDs(a.ID, b.ID)
		res := tx.Where("ticket_a_id = ? AND ticket_b_id = ?", int64(lo), int64(hi)).Delete(&TicketLink{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return response.NotFound("ticket.link_not_found")
		}
		act := agentActor(ctx, name)
		if err := s.recordActivity(ctx, tx, ticketRef(a), act, ActUnlinked, b.TicketNo, "", ""); err != nil {
			return err
		}
		return s.recordActivity(ctx, tx, ticketRef(b), act, ActUnlinked, a.TicketNo, "", "")
	})
}

// countLinks 数一张单挂了几条关联（两个方向都算）。
func countLinks(tx *gorm.DB, id idgen.ID) (int64, error) {
	var n int64
	err := tx.Model(&TicketLink{}).Where("ticket_a_id = ? OR ticket_b_id = ?", int64(id), int64(id)).Count(&n).Error
	return n, err
}

// ListLinks 返回这张单的关联单列表（双向读，最近的在前）。
// 只有关联行与工单行两次查询：读接口不带锁，也不该因为一张单被删而整体失败。
func (s *Service) ListLinks(ctx context.Context, ticketNo string) ([]LinkedTicket, error) {
	ticketNo = strings.TrimSpace(ticketNo)
	if ticketNo == "" {
		return nil, response.BadRequest("error.bad_request")
	}
	var self mergeTicket
	if err := s.db.WithContext(ctx).Where("ticket_no = ?", ticketNo).Take(&self).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("ticket.not_found")
		}
		return nil, err
	}
	rows := make([]TicketLink, 0, 8)
	err := s.db.WithContext(ctx).
		Where("ticket_a_id = ? OR ticket_b_id = ?", int64(self.ID), int64(self.ID)).
		Order("id DESC").Limit(linkListLimit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []LinkedTicket{}, nil
	}
	peers := make([]idgen.ID, 0, len(rows))
	for _, l := range rows {
		peers = append(peers, l.other(self.ID))
	}
	index := s.loadLinkedTickets(ctx, peers)
	out := make([]LinkedTicket, 0, len(rows))
	for _, l := range rows {
		peer := l.other(self.ID)
		row, ok := index[peer]
		if !ok {
			// 引用已断（对侧被删）：跳过这一条而不是让整列表读失败。
			continue
		}
		item := toLinkedTicket(row, KindLink, l.CreatedAt)
		out = append(out, item)
	}
	return out, nil
}

// other 取关联行的另一端。
func (l TicketLink) other(id idgen.ID) idgen.ID {
	if l.TicketAID == id {
		return l.TicketBID
	}
	return l.TicketAID
}

// loadLinkedTickets 批量按 id 取工单行；一张都取不到时返回空表而不是报错（读路径要软失败）。
func (s *Service) loadLinkedTickets(ctx context.Context, ids []idgen.ID) map[idgen.ID]*mergeTicket {
	out := make(map[idgen.ID]*mergeTicket, len(ids))
	if len(ids) == 0 {
		return out
	}
	unique := make([]idgen.ID, 0, len(ids))
	seen := make(map[idgen.ID]bool, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return out
	}
	rows := make([]mergeTicket, 0, len(unique))
	// 范围外的对侧单取不到，调用方原有的「索引里没有就跳过」分支正好把它变成不显示，
	// 所以关联列表与合并摘要都不需要各自再裁一遍。
	if err := scopeCond(ctx, s.db.WithContext(ctx)).Where("id IN ?", unique).Find(&rows).Error; err != nil {
		s.logWarn("ticket: load linked tickets failed", "err", err.Error())
		return out
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out
}

func toLinkedTicket(t *mergeTicket, kind string, at time.Time) LinkedTicket {
	return LinkedTicket{
		TicketNo: t.TicketNo, Title: t.Title, Status: t.Status,
		Priority: t.Priority, Kind: kind, LinkedAt: at,
	}
}

// mergedAt 用关闭时间充当「这次合并发生的时刻」：合并就是在那一刻把被合并方关掉的，
// 不再为它单独存一个时间列（动态轨迹里也已经有 created_at）。
func mergedAt(t *mergeTicket) time.Time {
	if t == nil || t.ClosedAt == nil {
		return time.Time{}
	}
	return *t.ClosedAt
}

// isNotFound 判断是否已是本包统一的 404：读路径遇到悬挂引用要软失败，而不是当 500 抛出去。
func isNotFound(err error) bool {
	var e *response.Error
	return errors.As(err, &e) && e.Status == 404
}
