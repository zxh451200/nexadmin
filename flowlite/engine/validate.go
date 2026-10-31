package engine

import (
	"fmt"
	"strings"
)

// Validate 校验流程定义的完整性，返回全部问题（空切片表示合法）。
// 校验覆盖：节点/连线引用、唯一开始事件、结束事件无出边、非结束节点有出边、
// 审批模式与审批人、升级目标、排他网关默认分支、从开始节点可达。
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
	startCount := 0
	for _, n := range def.Nodes {
		if strings.TrimSpace(n.Key) == "" {
			continue
		}
		switch n.Kind {
		case KindStart:
			startCount++
		case KindApproval, KindExclusive, KindEnd:
		default:
			errs = append(errs, fmt.Sprintf("节点 %s 的类型非法: %s", n.Key, n.Kind))
		}
		if n.Kind == KindApproval {
			if m := modeOf(n); m != ModeAny && m != ModeAll {
				errs = append(errs, fmt.Sprintf("节点 %s 的审批模式非法: %s", n.Key, n.Mode))
			}
			if t := strings.TrimSpace(n.EscalateTo); t != "" {
				if _, ok := idx[t]; !ok {
					errs = append(errs, fmt.Sprintf("节点 %s 的升级目标不存在: %s", n.Key, t))
				} else if idx[t].Kind != KindApproval {
					errs = append(errs, fmt.Sprintf("节点 %s 的升级目标必须是审批节点: %s", n.Key, t))
				}
			}
			if n.TimeoutMinutes < 0 {
				errs = append(errs, fmt.Sprintf("节点 %s 的超时分钟数不能为负", n.Key))
			}
			for _, a := range n.Approvers {
				if strings.TrimSpace(a) == "" {
					errs = append(errs, fmt.Sprintf("节点 %s 的审批人列表含空值", n.Key))
					break
				}
			}
		}
	}
	if startCount != 1 {
		errs = append(errs, fmt.Sprintf("流程必须恰好有一个开始节点，实际 %d 个", startCount))
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

	seenEdge := make(map[string]bool, len(def.Edges))
	for _, e := range def.Edges {
		from, okFrom := idx[e.From]
		if !okFrom {
			errs = append(errs, fmt.Sprintf("连线起点不存在: %s", e.From))
		}
		to, okTo := idx[e.To]
		if !okTo {
			errs = append(errs, fmt.Sprintf("连线终点不存在: %s", e.To))
		}
		sig := e.From + "\x00" + e.To + "\x00" + e.When
		if seenEdge[sig] {
			errs = append(errs, fmt.Sprintf("存在重复连线: %s → %s", e.From, e.To))
		}
		seenEdge[sig] = true
		if okFrom && from.Kind == KindEnd {
			errs = append(errs, fmt.Sprintf("结束节点 %s 不能有出边", e.From))
		}
		if okTo && to.Kind == KindStart {
			errs = append(errs, fmt.Sprintf("开始节点 %s 不能有入边", e.To))
		}
	}
	for _, n := range def.Nodes {
		if strings.TrimSpace(n.Key) == "" {
			continue
		}
		edges := outEdges(def, n.Key)
		if n.Kind != KindEnd && len(edges) == 0 {
			errs = append(errs, fmt.Sprintf("节点 %s 没有出边", n.Key))
		}
		if n.Kind != KindExclusive {
			continue
		}
		if len(edges) == 0 {
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
