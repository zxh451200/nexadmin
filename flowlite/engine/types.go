// Package engine 是 flowlite 的执行内核：一个零依赖、可嵌入的 BPMN 2.0 子集引擎。
//
// 支持的节点与语义：
//
//	start       开始事件
//	approval    人工审批（或签 any / 会签 all，可加签、可超时升级）
//	exclusive   排他网关（按条件表达式选择唯一分支，必须有默认分支）
//	end         结束事件
//
// 引擎只处理纯状态流转：不访问数据库、不认识多租户/HTTP/日志。持久化由 store 端口
// 提供，编排（版本快照、乐观锁、待办任务、事件）由 runtime 包完成。
package engine

import (
	"strings"
	"time"
)

// NodeKind 是节点类型。
type NodeKind string

const (
	KindStart     NodeKind = "start"     // 开始事件
	KindApproval  NodeKind = "approval"  // 人工审批任务
	KindExclusive NodeKind = "exclusive" // 排他网关（条件分支）
	KindEnd       NodeKind = "end"       // 结束事件
)

// 审批模式。
const (
	ModeAny = "any" // 或签：任一审批人同意即通过
	ModeAll = "all" // 会签：全部审批人同意才通过
)

// 实例状态。
const (
	StatusRunning   = "running"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCancelled = "cancelled"
)

// 流转动作（同时作为历史事件的动作名）。
const (
	ActionStart       = "start"
	ActionApprove     = "approve"
	ActionReject      = "reject"
	ActionCountersign = "countersign"
	ActionRollback    = "rollback"
	ActionEscalate    = "escalate"
	ActionWithdraw    = "withdraw"
)

// 单条投票的取值。
const (
	VoteApprove = "approve"
	VoteReject  = "reject"
)

// Node 是一个流程节点。
//
// Approvers 用宿主自己的主体标识（通常是用户 ID 字符串）表示审批人。空表示
// 「任意已登录用户可审批」——这是显式语义，Approve 与 Reject 对它的处理一致。
// 需要按角色/部门/发起人上级分配时，由宿主在发起前解析成具体主体并写入。
type Node struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Kind           NodeKind `json:"kind"`
	Approvers      []string `json:"approvers,omitempty"`
	Mode           string   `json:"mode,omitempty"`           // 仅审批节点：any（默认）/ all
	EscalateTo     string   `json:"escalateTo,omitempty"`     // 仅审批节点：超时升级目标节点 key
	TimeoutMinutes int      `json:"timeoutMinutes,omitempty"` // 仅审批节点：>0 时由调用方定时升级
}

// Edge 是一条顺序流。When 为条件表达式（如 "amount>10000"）；空 = 默认/无条件。
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	When string `json:"when,omitempty"`
}

// Definition 是一份流程定义。
type Definition struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Start string `json:"start"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Vote 是某个节点上「审批人 → 意见」的投票记录，意见取 approve / reject。
type Vote map[string]string

// Event 是一条流转历史。
type Event struct {
	At      time.Time `json:"at"`
	Node    string    `json:"node"`
	Actor   string    `json:"actor"`
	Action  string    `json:"action"`
	Comment string    `json:"comment,omitempty"`
}

// Instance 是一个流程实例的状态快照。令牌用 Current（当前活动节点集合）表示。
//
// Revision 在每次成功变更后自增，供持久化层的乐观锁使用：调用方保存时必须带上
// 读到的 Revision，比对失败即说明并发冲突，应重读重放而不是覆盖。
type Instance struct {
	DefKey         string              `json:"defKey"`
	Status         string              `json:"status"`
	Current        []string            `json:"current"`
	Done           []string            `json:"done"`
	Votes          map[string]Vote     `json:"votes"`
	ExtraApprovers map[string][]string `json:"extraApprovers,omitempty"`
	Vars           map[string]string   `json:"vars"`
	History        []Event             `json:"history"`
	Revision       int64               `json:"revision"`
}

// PendingTask 是某个待审批节点上的一位待办人。
type PendingTask struct {
	NodeKey  string
	NodeName string
	Assignee string
}

// NodeMap 按 key 建索引（导出给宿主展示用）。
func NodeMap(def Definition) map[string]Node { return nodeMap(def) }

// PendingTasks 返回当前所有待审批任务。审批人未配置时返回一条 Assignee 为空的
// 任务，表示「任意已登录用户可办」；不支持候选组的引擎用空串表达公开任务。
func PendingTasks(def Definition, inst *Instance) []PendingTask {
	if inst == nil || inst.Status != StatusRunning {
		return nil
	}
	idx := nodeMap(def)
	var out []PendingTask
	for _, key := range dedup(inst.Current) {
		n, ok := idx[key]
		if !ok || n.Kind != KindApproval {
			continue
		}
		approvers := effectiveApprovers(n, inst)
		if len(approvers) == 0 {
			out = append(out, PendingTask{NodeKey: n.Key, NodeName: nodeName(n)})
			continue
		}
		for _, a := range approvers {
			out = append(out, PendingTask{NodeKey: n.Key, NodeName: nodeName(n), Assignee: a})
		}
	}
	return out
}

// NodeDisplayName 返回节点的展示名（无名字时退回 key）。
func NodeDisplayName(n Node) string { return nodeName(n) }

func nodeMap(def Definition) map[string]Node {
	m := make(map[string]Node, len(def.Nodes))
	for _, n := range def.Nodes {
		if n.Key != "" {
			m[n.Key] = n
		}
	}
	return m
}

func outEdges(def Definition, key string) []Edge {
	var out []Edge
	for _, e := range def.Edges {
		if e.From == key {
			out = append(out, e)
		}
	}
	return out
}

// reachable 返回从 from 出发可达的全部节点（含 from）。
func reachable(def Definition, from string) map[string]bool {
	seen := map[string]bool{from: true}
	stack := []string{from}
	for len(stack) > 0 {
		k := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range outEdges(def, k) {
			if !seen[e.To] {
				seen[e.To] = true
				stack = append(stack, e.To)
			}
		}
	}
	return seen
}

// modeOf 归一化审批模式：空串按 any。
func modeOf(n Node) string {
	m := strings.ToLower(strings.TrimSpace(n.Mode))
	if m == "" {
		return ModeAny
	}
	return m
}

func effectiveApprovers(n Node, inst *Instance) []string {
	out := append([]string(nil), n.Approvers...)
	for _, a := range inst.ExtraApprovers[n.Key] {
		if !contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}

// satisfied 判断节点是否满足通过条件：
// 未配置审批人时任意一人表态即完成；any 任一同意；all 全部同意。
func satisfied(mode string, approvers []string, votes Vote) bool {
	if len(approvers) == 0 {
		return len(votes) > 0
	}
	if mode == ModeAll {
		for _, a := range approvers {
			if votes[a] != VoteApprove {
				return false
			}
		}
		return true
	}
	for _, a := range approvers {
		if votes[a] == VoteApprove {
			return true
		}
	}
	return false
}

// canAct 报告 actor 是否可以在该节点表态。空审批人 = 任意人可办（与 Reject 口径一致）。
func canAct(approvers []string, actor string) bool {
	return len(approvers) == 0 || contains(approvers, actor)
}

func nodeName(n Node) string {
	if strings.TrimSpace(n.Name) != "" {
		return n.Name
	}
	return n.Key
}

func trimComment(comment, fallback string) string {
	if c := strings.TrimSpace(comment); c != "" {
		return c
	}
	return fallback
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func appendUnique(list []string, v string) []string {
	if contains(list, v) {
		return list
	}
	return append(list, v)
}

func removeKey(list []string, v string) []string {
	out := list[:0:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func removeKeys(list []string, drop map[string]bool) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if !drop[x] {
			out = append(out, x)
		}
	}
	return out
}

func dedup(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, x := range list {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
