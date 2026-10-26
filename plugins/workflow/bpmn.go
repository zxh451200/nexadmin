package workflow

// bpmn.go 是一套自研的 BPMN 2.0 子集流程引擎，移植自 nexGrain 用户区的
// internal/x/bpmn（会签/加签/回退/超时升级/排他网关/XML 导出已在生产验证），
// 运行期不引入外部 BPMN 引擎以规避供应链与授权不确定性。
// 本文件是纯函数实现（无数据库、无框架依赖），由插件服务层宿主。

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
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

// Node 是一个流程节点。Approvers 用「用户 ID 字符串」标识审批人（与 im/ticket 的
// sender 口径一致，模块层把当前登录用户 ID 传进来比对）；空 = 任意登录用户可审。
type Node struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Kind           NodeKind `json:"kind"`
	Approvers      []string `json:"approvers,omitempty"`
	Mode           string   `json:"mode,omitempty"`           // 仅审批节点：any（默认）/ all
	EscalateTo     string   `json:"escalateTo,omitempty"`     // 仅审批节点：超时升级目标节点 key
	TimeoutMinutes int      `json:"timeoutMinutes,omitempty"` // 仅审批节点：>0 时由清扫器自动升级
}

// Edge 是一条顺序流（连线）。When 为条件表达式，如 "amount>10000"；空 = 默认/无条件。
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

// Instance 是一个流程实例。token 用 Current（当前活动节点集合）表示。
type Instance struct {
	DefKey         string              `json:"defKey"`
	Status         string              `json:"status"`
	Current        []string            `json:"current"`
	Done           []string            `json:"done"`
	Votes          map[string]Vote     `json:"votes"`
	ExtraApprovers map[string][]string `json:"extraApprovers,omitempty"` // 加签追加的审批人
	Vars           map[string]string   `json:"vars"`
	History        []Event             `json:"history"`
}

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

// Validate 校验流程定义的完整性，返回全部问题（空切片表示合法）。
func Validate(def Definition) []string {
	var errs []string
	if strings.TrimSpace(def.Key) == "" {
		errs = append(errs, "流程 key 不能为空")
	}
	if len(def.Nodes) == 0 {
		errs = append(errs, "流程至少需要一个节点")
		return errs
	}
	idx := make(map[string]Node, len(def.Nodes))
	for _, n := range def.Nodes {
		key := strings.TrimSpace(n.Key)
		if key == "" {
			errs = append(errs, "存在 key 为空的节点")
			continue
		}
		if _, dup := idx[key]; dup {
			errs = append(errs, fmt.Sprintf("节点 key 重复: %s", key))
			continue
		}
		idx[key] = n
	}
	for _, n := range def.Nodes {
		if strings.TrimSpace(n.Key) == "" {
			continue
		}
		switch n.Kind {
		case KindStart, KindApproval, KindExclusive, KindEnd:
		default:
			errs = append(errs, fmt.Sprintf("节点 %s 的类型非法: %s", n.Key, n.Kind))
		}
		if n.Kind != KindApproval {
			continue
		}
		if m := modeOf(n); m != ModeAny && m != ModeAll {
			errs = append(errs, fmt.Sprintf("节点 %s 的审批模式非法: %s", n.Key, n.Mode))
		}
		if t := strings.TrimSpace(n.EscalateTo); t != "" {
			if _, ok := idx[t]; !ok {
				errs = append(errs, fmt.Sprintf("节点 %s 的升级目标不存在: %s", n.Key, t))
			}
		}
		if n.TimeoutMinutes < 0 {
			errs = append(errs, fmt.Sprintf("节点 %s 的超时分钟数不能为负", n.Key))
		}
	}
	if strings.TrimSpace(def.Start) == "" {
		errs = append(errs, "未指定开始节点")
	} else if n, ok := idx[def.Start]; !ok {
		errs = append(errs, "开始节点不存在: "+def.Start)
	} else if n.Kind != KindStart {
		errs = append(errs, "开始节点类型必须是 start: "+def.Start)
	}
	endCount := 0
	for _, n := range def.Nodes {
		if n.Kind == KindEnd {
			endCount++
		}
	}
	if endCount == 0 {
		errs = append(errs, "流程至少需要一个结束节点")
	}
	for _, e := range def.Edges {
		if _, ok := idx[e.From]; !ok {
			errs = append(errs, fmt.Sprintf("连线起点不存在: %s", e.From))
		}
		if _, ok := idx[e.To]; !ok {
			errs = append(errs, fmt.Sprintf("连线终点不存在: %s", e.To))
		}
	}
	for _, n := range def.Nodes {
		if n.Kind != KindExclusive {
			continue
		}
		edges := outEdges(def, n.Key)
		if len(edges) == 0 {
			errs = append(errs, fmt.Sprintf("排他网关 %s 没有出边", n.Key))
			continue
		}
		hasDefault := false
		for _, e := range edges {
			if strings.TrimSpace(e.When) == "" {
				hasDefault = true
			}
		}
		if !hasDefault {
			errs = append(errs, fmt.Sprintf("排他网关 %s 缺少默认出边（when 为空）", n.Key))
		}
	}
	if def.Start != "" {
		if _, ok := idx[def.Start]; ok {
			reach := reachable(def, def.Start)
			for _, n := range def.Nodes {
				if strings.TrimSpace(n.Key) == "" {
					continue
				}
				if !reach[n.Key] {
					errs = append(errs, fmt.Sprintf("节点 %s 从开始节点不可达", n.Key))
				}
			}
		}
	}
	return errs
}

// maxAutoSteps 防止定义成环时死循环。
const maxAutoSteps = 1000

// Start 依据定义新建并启动一个实例：token 从开始节点自动推进到第一个审批节点。
func Start(def Definition, vars map[string]string, actor string, now time.Time) (*Instance, error) {
	if errs := Validate(def); len(errs) > 0 {
		return nil, errors.New("流程定义非法: " + strings.Join(errs, "；"))
	}
	inst := &Instance{
		DefKey:         def.Key,
		Status:         StatusRunning,
		Current:        []string{def.Start},
		Done:           []string{},
		Votes:          map[string]Vote{},
		ExtraApprovers: map[string][]string{},
		Vars:           map[string]string{},
		History:        []Event{},
	}
	for k, v := range vars {
		inst.Vars[k] = v
	}
	inst.History = append(inst.History, Event{At: now, Node: def.Start, Actor: actor, Action: ActionStart})
	if err := autoAdvance(def, inst); err != nil {
		return nil, err
	}
	return inst, nil
}

// autoAdvance 反复消费 start / exclusive / end 节点，直到 Current 只剩审批节点或为空。
// Current 变空说明所有分支都已到达结束事件 —— 实例自动通过。
func autoAdvance(def Definition, inst *Instance) error {
	idx := nodeMap(def)
	for step := 0; step < maxAutoSteps; step++ {
		changed := false
		out := make([]string, 0, len(inst.Current))
		for _, key := range dedup(inst.Current) {
			n, ok := idx[key]
			if !ok {
				return fmt.Errorf("节点不存在: %s", key)
			}
			switch n.Kind {
			case KindStart, KindExclusive:
				targets, err := follow(def, inst, n)
				if err != nil {
					return err
				}
				inst.Done = appendUnique(inst.Done, key)
				out = append(out, targets...)
				changed = true
			case KindEnd:
				inst.Done = appendUnique(inst.Done, key)
				changed = true
			default: // approval：等待人工投票
				out = append(out, key)
			}
		}
		inst.Current = dedup(out)
		if !changed {
			break
		}
	}
	if len(inst.Current) == 0 {
		inst.Status = StatusApproved
	}
	return nil
}

// follow 计算节点的后继节点：排他网关走第一条命中的分支，其余节点走全部出边。
func follow(def Definition, inst *Instance, n Node) ([]string, error) {
	edges := outEdges(def, n.Key)
	if len(edges) == 0 {
		return nil, nil // 无出边的分支：token 消失
	}
	if n.Kind == KindExclusive {
		for _, e := range edges {
			if EvalCondition(e.When, inst.Vars) {
				return []string{e.To}, nil
			}
		}
		return nil, fmt.Errorf("排他网关 %s 没有可用分支", n.Key)
	}
	targets := make([]string, 0, len(edges))
	for _, e := range edges {
		targets = append(targets, e.To)
	}
	return targets, nil
}

// activeApproval 返回当前唯一的活动审批节点；没有或多个都报错。
func activeApproval(def Definition, inst *Instance) (Node, error) {
	idx := nodeMap(def)
	var found []Node
	for _, key := range dedup(inst.Current) {
		if n, ok := idx[key]; ok && n.Kind == KindApproval {
			found = append(found, n)
		}
	}
	if len(found) == 0 {
		return Node{}, errors.New("当前没有待审批节点")
	}
	if len(found) > 1 {
		return Node{}, errors.New("当前存在多个待审批节点，无法确定操作目标")
	}
	return found[0], nil
}

func requireRunning(inst *Instance) error {
	if inst.Status != StatusRunning {
		return fmt.Errorf("流程实例已结束（%s），不能继续操作", inst.Status)
	}
	return nil
}

// effectiveApprovers 合并节点配置的审批人与加签追加的审批人。
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

// Approve 记录一次同意；满足条件则推进到后继节点。
func Approve(def Definition, inst *Instance, actor, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	node, err := activeApproval(def, inst)
	if err != nil {
		return err
	}
	approvers := effectiveApprovers(node, inst)
	if len(approvers) > 0 && !contains(approvers, actor) {
		return fmt.Errorf("用户 %s 不是节点「%s」的审批人", actor, nodeName(node))
	}
	if inst.Votes[node.Key] == nil {
		inst.Votes[node.Key] = Vote{}
	}
	inst.Votes[node.Key][actor] = VoteApprove
	inst.History = append(inst.History, Event{At: now, Node: node.Key, Actor: actor, Action: ActionApprove, Comment: comment})
	if !satisfied(modeOf(node), approvers, inst.Votes[node.Key]) {
		return nil
	}
	inst.Done = appendUnique(inst.Done, node.Key)
	inst.Current = removeKey(inst.Current, node.Key)
	targets, err := follow(def, inst, node)
	if err != nil {
		return err
	}
	inst.Current = dedup(append(inst.Current, targets...))
	return autoAdvance(def, inst)
}

// Reject 记录一次否决：政策上任一节点被否决即整条实例失败（rejected）。
func Reject(def Definition, inst *Instance, actor, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	node, err := activeApproval(def, inst)
	if err != nil {
		return err
	}
	if !contains(effectiveApprovers(node, inst), actor) {
		return fmt.Errorf("用户 %s 不是节点「%s」的审批人", actor, nodeName(node))
	}
	if inst.Votes[node.Key] == nil {
		inst.Votes[node.Key] = Vote{}
	}
	inst.Votes[node.Key][actor] = VoteReject
	inst.History = append(inst.History, Event{At: now, Node: node.Key, Actor: actor, Action: ActionReject, Comment: comment})
	inst.Done = appendUnique(inst.Done, node.Key)
	inst.Current = nil
	inst.Status = StatusRejected
	return nil
}

// Countersign 加签：往当前审批节点追加一个审批人（all 模式下需其也同意）。
func Countersign(def Definition, inst *Instance, actor, approver, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	approver = strings.TrimSpace(approver)
	if approver == "" {
		return errors.New("加签人不能为空")
	}
	node, err := activeApproval(def, inst)
	if err != nil {
		return err
	}
	if inst.ExtraApprovers == nil {
		inst.ExtraApprovers = map[string][]string{}
	}
	if !contains(effectiveApprovers(node, inst), approver) {
		inst.ExtraApprovers[node.Key] = append(inst.ExtraApprovers[node.Key], approver)
	}
	inst.History = append(inst.History, Event{At: now, Node: node.Key, Actor: actor, Action: ActionCountersign, Comment: trimComment(comment, "加签 "+approver)})
	return nil
}

// Rollback 回退：把 token 退回某个已办审批节点，清除该节点及其下游的投票与完成标记，
// 使其可重新审批。只允许回退到审批节点。
func Rollback(def Definition, inst *Instance, actor, toNode, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	toNode = strings.TrimSpace(toNode)
	if toNode == "" {
		return errors.New("回退目标节点不能为空")
	}
	target, ok := nodeMap(def)[toNode]
	if !ok {
		return fmt.Errorf("回退目标节点不存在: %s", toNode)
	}
	if target.Kind != KindApproval {
		return fmt.Errorf("只能回退到审批节点: %s", toNode)
	}
	down := reachable(def, toNode)
	for key := range down {
		delete(inst.Votes, key)
		delete(inst.ExtraApprovers, key)
	}
	inst.Done = removeKeys(inst.Done, down)
	inst.Current = []string{toNode}
	inst.History = append(inst.History, Event{At: now, Node: toNode, Actor: actor, Action: ActionRollback, Comment: comment})
	return autoAdvance(def, inst)
}

// Escalate 超时升级：把当前审批节点让给其 EscalateTo 指定的节点（通常更高级别），
// 并清除目标节点的历史投票。可由清扫器定时扫描超时实例触发。
func Escalate(def Definition, inst *Instance, actor, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	node, err := activeApproval(def, inst)
	if err != nil {
		return err
	}
	target := strings.TrimSpace(node.EscalateTo)
	if target == "" {
		return fmt.Errorf("节点「%s」未配置升级目标", nodeName(node))
	}
	if _, ok := nodeMap(def)[target]; !ok {
		return fmt.Errorf("升级目标节点不存在: %s", target)
	}
	inst.Done = appendUnique(inst.Done, node.Key)
	inst.Done = removeKey(inst.Done, target)
	delete(inst.Votes, target)
	delete(inst.ExtraApprovers, target)
	inst.Current = []string{target}
	inst.History = append(inst.History, Event{At: now, Node: target, Actor: actor, Action: ActionEscalate, Comment: comment})
	return autoAdvance(def, inst)
}

// Withdraw 撤回：提交人把运行中的实例置为 cancelled。
func Withdraw(inst *Instance, actor, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	if actor == "" {
		return errors.New("撤回人不能为空")
	}
	inst.Status = StatusCancelled
	inst.History = append(inst.History, Event{At: now, Node: currentLabel(inst), Actor: actor, Action: ActionWithdraw, Comment: comment})
	return nil
}

// currentLabel 取当前活动节点标签（撤回历史用；引擎不感知定义时退化为逗号拼接）。
func currentLabel(inst *Instance) string {
	return strings.Join(dedup(inst.Current), ",")
}

// EvalCondition 计算条件表达式（形如 "amount>10000"、"level==high"）。
// 支持运算符：>= <= == != > <；左值是流程变量名，右值是数字或字符串字面量。
// 空表达式返回 true（默认流）；无法解析的表达式返回 false（宁可不通分支，也不误走分支）。
func EvalCondition(expr string, vars map[string]string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	for _, op := range []string{">=", "<=", "==", "!=", ">", "<"} {
		i := strings.Index(expr, op)
		if i < 0 {
			continue
		}
		left := strings.TrimSpace(expr[:i])
		right := strings.Trim(strings.TrimSpace(expr[i+len(op):]), "\"'")
		if left == "" || right == "" {
			return false
		}
		lv, ok := vars[left]
		if !ok {
			return false
		}
		return compare(op, lv, right)
	}
	return false
}

// compare 先按数字比较，数字不成立再按字符串比较（此时只支持 == / !=）。
func compare(op, a, b string) bool {
	af, aerr := strconv.ParseFloat(strings.TrimSpace(a), 64)
	bf, berr := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if aerr == nil && berr == nil {
		switch op {
		case ">":
			return af > bf
		case "<":
			return af < bf
		case ">=":
			return af >= bf
		case "<=":
			return af <= bf
		case "==":
			return af == bf
		case "!=":
			return af != bf
		}
		return false
	}
	switch op {
	case "==":
		return a == b
	case "!=":
		return a != b
	}
	return false
}

// ExportXML 把定义序列化为 BPMN 2.0 XML（本引擎可表达的子集），便于与 bpmn-js /
// Camunda Modeler 等标准建模工具互换。只导出 start/end/userTask/exclusiveGateway 与顺序流。
func ExportXML(def Definition) (string, error) {
	if errs := Validate(def); len(errs) > 0 {
		return "", fmt.Errorf("流程定义非法: %s", strings.Join(errs, "；"))
	}
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" id="Definitions_nexadmin" targetNamespace="http://nexadmin.local/bpmn">`)
	b.WriteString("\n")
	fmt.Fprintf(&b, "  <bpmn:process id=\"%s\" name=\"%s\" isExecutable=\"true\">\n", xmlAttr(def.Key), xmlAttr(def.Name))
	for _, n := range def.Nodes {
		id, name := xmlAttr(n.Key), xmlAttr(n.Name)
		switch n.Kind {
		case KindStart:
			fmt.Fprintf(&b, "    <bpmn:startEvent id=\"%s\" name=\"%s\"/>\n", id, name)
		case KindEnd:
			fmt.Fprintf(&b, "    <bpmn:endEvent id=\"%s\" name=\"%s\"/>\n", id, name)
		case KindApproval:
			fmt.Fprintf(&b, "    <bpmn:userTask id=\"%s\" name=\"%s\"/>\n", id, name)
		case KindExclusive:
			fmt.Fprintf(&b, "    <bpmn:exclusiveGateway id=\"%s\" name=\"%s\"/>\n", id, name)
		}
	}
	for i, e := range def.Edges {
		fid, from, to := fmt.Sprintf("flow_%d_%s_%s", i, e.From, e.To), xmlAttr(e.From), xmlAttr(e.To)
		if strings.TrimSpace(e.When) == "" {
			fmt.Fprintf(&b, "    <bpmn:sequenceFlow id=\"%s\" sourceRef=\"%s\" targetRef=\"%s\"/>\n", xmlAttr(fid), from, to)
			continue
		}
		fmt.Fprintf(&b, "    <bpmn:sequenceFlow id=\"%s\" sourceRef=\"%s\" targetRef=\"%s\">\n", xmlAttr(fid), from, to)
		fmt.Fprintf(&b, "      <bpmn:conditionExpression xsi:type=\"bpmn:tFormalExpression\">%s</bpmn:conditionExpression>\n", xmlText(e.When))
		b.WriteString("    </bpmn:sequenceFlow>\n")
	}
	b.WriteString("  </bpmn:process>\n")
	b.WriteString("</bpmn:definitions>\n")
	return b.String(), nil
}

func xmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}

func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
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
