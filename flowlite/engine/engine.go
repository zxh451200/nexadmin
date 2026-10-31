package engine

import (
	"fmt"
	"strings"
	"time"
)

// maxAutoSteps 防止定义成环时死循环。
const maxAutoSteps = 1000

// Start 依据定义新建并启动一个实例：token 从开始节点自动推进到第一个审批节点。
func Start(def Definition, vars map[string]string, actor string, now time.Time) (*Instance, error) {
	if errs := Validate(def); len(errs) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDefinition, strings.Join(errs, "；"))
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
	inst.Revision = 1
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
				return fmt.Errorf("%w: %s", ErrUnknownNode, key)
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
		if len(inst.Current) == 0 {
			inst.Status = StatusApproved
			return nil
		}
		if !changed {
			break
		}
	}
	return nil
}

// follow 计算节点的后继节点：排他网关走第一条命中的分支，其余节点走全部出边。
func follow(def Definition, inst *Instance, n Node) ([]string, error) {
	edges := outEdges(def, n.Key)
	if len(edges) == 0 {
		return nil, nil // 无出边的分支：token 消失（Validate 只放行结束节点，这里防御定义被改坏）
	}
	if n.Kind == KindExclusive {
		for _, e := range edges {
			if EvalCondition(e.When, inst.Vars) {
				return []string{e.To}, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrNoBranch, n.Key)
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
	found := make([]Node, 0, 1)
	for _, key := range dedup(inst.Current) {
		if n, ok := idx[key]; ok && n.Kind == KindApproval {
			found = append(found, n)
		}
	}
	if len(found) == 0 {
		return Node{}, ErrNoActiveTask
	}
	if len(found) > 1 {
		return Node{}, ErrMultipleActiveTasks
	}
	return found[0], nil
}

func requireRunning(inst *Instance) error {
	if inst.Status != StatusRunning {
		return fmt.Errorf("%w: %s", ErrNotRunning, inst.Status)
	}
	return nil
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
	if !canAct(approvers, actor) {
		return fmt.Errorf("%w: user %s is not approver of %s", ErrNotApprover, actor, nodeName(node))
	}
	if inst.Votes[node.Key] == nil {
		inst.Votes[node.Key] = Vote{}
	}
	inst.Votes[node.Key][actor] = VoteApprove
	inst.History = append(inst.History, Event{At: now, Node: node.Key, Actor: actor, Action: ActionApprove, Comment: comment})
	inst.Revision++
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
	approvers := effectiveApprovers(node, inst)
	if !canAct(approvers, actor) {
		return fmt.Errorf("%w: user %s is not approver of %s", ErrNotApprover, actor, nodeName(node))
	}
	if inst.Votes[node.Key] == nil {
		inst.Votes[node.Key] = Vote{}
	}
	inst.Votes[node.Key][actor] = VoteReject
	inst.History = append(inst.History, Event{At: now, Node: node.Key, Actor: actor, Action: ActionReject, Comment: comment})
	inst.Done = appendUnique(inst.Done, node.Key)
	inst.Current = nil
	inst.Status = StatusRejected
	inst.Revision++
	return nil
}

// Countersign 加签：往当前审批节点追加一个审批人（all 模式下需其也同意）。
func Countersign(def Definition, inst *Instance, actor, approver, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	approver = strings.TrimSpace(approver)
	if approver == "" {
		return fmt.Errorf("加签人不能为空")
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
	inst.Revision++
	return nil
}

// Rollback 回退：把 token 退回某个审批节点，清除该节点及其下游的投票与完成标记，
// 使其可重新审批。只允许回退到审批节点。是否允许某个人回退由宿主鉴权决定。
func Rollback(def Definition, inst *Instance, actor, toNode, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	toNode = strings.TrimSpace(toNode)
	if toNode == "" {
		return fmt.Errorf("回退目标节点不能为空")
	}
	target, ok := nodeMap(def)[toNode]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownNode, toNode)
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
	inst.Revision++
	return autoAdvance(def, inst)
}

// Escalate 超时升级：把当前审批节点让给其 EscalateTo 指定的节点（通常更高级别），
// 并清除目标节点的历史投票。可由调用方定时扫描超时实例触发。
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
		return fmt.Errorf("%w: %s", ErrNoEscalateTarget, nodeName(node))
	}
	if _, ok := nodeMap(def)[target]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownNode, target)
	}
	inst.Done = appendUnique(inst.Done, node.Key)
	inst.Done = removeKey(inst.Done, target)
	delete(inst.Votes, target)
	delete(inst.ExtraApprovers, target)
	inst.Current = []string{target}
	inst.History = append(inst.History, Event{At: now, Node: target, Actor: actor, Action: ActionEscalate, Comment: comment})
	inst.Revision++
	return autoAdvance(def, inst)
}

// Withdraw 撤回：提交人把运行中的实例置为 cancelled。
func Withdraw(inst *Instance, actor, comment string, now time.Time) error {
	if err := requireRunning(inst); err != nil {
		return err
	}
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("撤回人不能为空")
	}
	inst.Status = StatusCancelled
	inst.History = append(inst.History, Event{At: now, Node: currentLabel(inst), Actor: actor, Action: ActionWithdraw, Comment: comment})
	inst.Revision++
	return nil
}

// currentLabel 取当前活动节点标签（撤回历史用；引擎不感知定义时退化为逗号拼接）。
func currentLabel(inst *Instance) string {
	return strings.Join(dedup(inst.Current), ",")
}

// IsActiveApprover 报告 actor 是否是当前活动审批节点的审批人（空审批人 = 公开任务）。
// 供宿主在展示层判断「能不能点同意」；真正的强制校验在 Approve/Reject 内部。
func IsActiveApprover(def Definition, inst *Instance, actor string) bool {
	if inst == nil || inst.Status != StatusRunning {
		return false
	}
	idx := nodeMap(def)
	for _, key := range dedup(inst.Current) {
		n, ok := idx[key]
		if !ok || n.Kind != KindApproval {
			continue
		}
		if canAct(effectiveApprovers(n, inst), actor) {
			return true
		}
	}
	return false
}

// OverdueApprovals 返回当前待审批节点里「已超过 TimeoutMinutes 且配置了升级目标」的节点。
// since 通常是实例最后一次变更时间；由宿主定时调用，再对返回的实例执行 Escalate。
func OverdueApprovals(def Definition, inst *Instance, since, now time.Time) []Node {
	if inst == nil || inst.Status != StatusRunning {
		return nil
	}
	idx := nodeMap(def)
	var out []Node
	for _, key := range dedup(inst.Current) {
		n, ok := idx[key]
		if !ok || n.Kind != KindApproval {
			continue
		}
		if n.TimeoutMinutes <= 0 || strings.TrimSpace(n.EscalateTo) == "" {
			continue
		}
		if time.Duration(n.TimeoutMinutes)*time.Minute <= now.Sub(since) {
			out = append(out, n)
		}
	}
	return out
}
