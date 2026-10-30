package website

import (
	"sort"

	"nexadmin/core/response"
)

// 导航层级规则：顶级 = 1 级，最多 3 级；父子关系用 parentId 表达，所有校验都是纯函数，便于单测。

const navMaxDepth = 3

func navBadRequest(key string, args ...any) error {
	return response.BadRequest(key, args...)
}

// navDepth 计算把节点 id 挂到 parentID 下后的层级；id=0 表示新建节点。
// 同时拒绝父级不存在、挂到自己或后代下、以及历史数据里的环。
func navDepth(parents map[uint64]uint64, id, parentID uint64) (int, error) {
	level := 1
	seen := make(map[uint64]bool)
	for cur := parentID; cur != 0; {
		if cur == id {
			return 0, navBadRequest("website.nav_self_parent")
		}
		if seen[cur] {
			return 0, navBadRequest("website.nav_cycle")
		}
		seen[cur] = true
		parent, ok := parents[cur]
		if !ok {
			return 0, navBadRequest("website.nav_parent_missing")
		}
		level++
		if level > navMaxDepth {
			return 0, navBadRequest("website.nav_depth_exceeded", navMaxDepth)
		}
		cur = parent
	}
	return level, nil
}

// validateNavParents 校验整棵导航树：父级都存在、无环、层级不超限。
func validateNavParents(parents map[uint64]uint64) error {
	ids := make([]uint64, 0, len(parents))
	for id := range parents {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, err := navDepth(parents, id, parents[id]); err != nil {
			return err
		}
	}
	return nil
}

// navParentMap 把导航行压成 id → parentId 的映射。
func navParentMap(rows []Nav) map[uint64]uint64 {
	out := make(map[uint64]uint64, len(rows))
	for _, r := range rows {
		out[uint64(r.ID)] = uint64(r.ParentID)
	}
	return out
}

// navSubtreeHeight 返回以 root 为根的子树层数（叶子 = 1）；脏数据里的环按已访问处理。
func navSubtreeHeight(parents map[uint64]uint64, root uint64) int {
	children := make(map[uint64][]uint64, len(parents))
	for id, pid := range parents {
		children[pid] = append(children[pid], id)
	}
	seen := make(map[uint64]bool, len(parents))
	var walk func(id uint64) int
	walk = func(id uint64) int {
		if seen[id] {
			return 0
		}
		seen[id] = true
		height := 1
		for _, c := range children[id] {
			if h := walk(c) + 1; h > height {
				height = h
			}
		}
		return height
	}
	return walk(root)
}

// navSubtreeIDs 返回 root 及其全部后代的 ID（级联删除用）；root 不在 parents 里时返回空。
func navSubtreeIDs(parents map[uint64]uint64, root uint64) []uint64 {
	children := make(map[uint64][]uint64, len(parents))
	for id, pid := range parents {
		children[pid] = append(children[pid], id)
	}
	out := make([]uint64, 0, 4)
	stack := []uint64{root}
	seen := make(map[uint64]bool)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		if _, ok := parents[cur]; !ok {
			continue
		}
		out = append(out, cur)
		stack = append(stack, children[cur]...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// buildNavTree 把已按 sort,id 排好序的启用导航组装成树。
// 父级不在 rows 里的节点（父级被停用/删除，或父级自身不可见）连同子树一起丢弃；
// 超过层级上限的历史脏数据直接截断，避免递归失控。
func buildNavTree(rows []Nav) []Nav {
	var attach func(parentID uint64, depth int) []Nav
	attach = func(parentID uint64, depth int) []Nav {
		var out []Nav
		if depth > navMaxDepth {
			return out
		}
		for _, n := range rows {
			if uint64(n.ParentID) != parentID {
				continue
			}
			n.Children = attach(uint64(n.ID), depth+1)
			out = append(out, n)
		}
		return out
	}
	return attach(0, 1)
}
