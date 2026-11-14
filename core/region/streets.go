package region

// 街道/乡镇层：随包发布的第 4 级增强数据（data/streets.json.gz，约 4.1 万条街/镇/乡，
// gzip 后约 350KB）。与三级主树分离的原因：业务表只存 6 位行政区划代码（见包注释），
// 街道层只供选择器展示与「折进详细地址」，不进存储契约，也不参与同步快照的校验口径——
// Activate/Validate 仍按三级树校验；同步来的快照没有街道层时 Tree(4) 自动没有第 4 级，
// 街道按县级代码挂接，县级代码不存在（数据源更替）的地方自然没有。
//
// 数据来源与许可：github.com/modood/Administrative-divisions-of-China（MIT，国家统计局
// 四级区划编码，街道为 9 位代码）。重新生成：scripts/region_fetch 之外，把该仓库的
// dist/pcas-code.json 转成 {"6位县级代码": [["9位街道代码","名称"], …]} 再 gzip 即可。

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed data/streets.json.gz
var rawStreets []byte

var (
	streetOnce sync.Once
	// streetIndex：6 位县级代码 → 该县下辖街道（9 位代码）。加载失败保持 nil（没有第 4 级）。
	streetIndex map[string][]*Node
	streetTotal int
)

func loadStreets() map[string][]*Node {
	streetOnce.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(rawStreets))
		if err != nil {
			return
		}
		var raw map[string][][2]string
		if err := json.NewDecoder(zr).Decode(&raw); err != nil {
			return
		}
		idx := make(map[string][]*Node, len(raw))
		total := 0
		for district, list := range raw {
			nodes := make([]*Node, 0, len(list))
			for _, st := range list {
				if len(st[0]) != 9 || st[1] == "" {
					continue
				}
				nodes = append(nodes, &Node{Code: st[0], Name: st[1]})
				total++
			}
			if len(nodes) > 0 {
				idx[district] = nodes
			}
		}
		streetIndex, streetTotal = idx, total
	})
	return streetIndex
}

// StreetCount 返回街道层数据量；0 表示内置街道数据缺失（Tree(4) 会退化为三级）。
func StreetCount() int {
	loadStreets()
	return streetTotal
}

// attachStreets 把街道挂到三级树里所有有街道数据的县级节点下（节点全部是新副本，调用方
// 拿到的树可以随意修改）。街道不生成拼音：4 万条名称做拼音转换不值得，名称搜索已够用。
func attachStreets(roots []*Node) {
	idx := loadStreets()
	if len(idx) == 0 {
		return
	}
	var walk func(list []*Node)
	walk = func(list []*Node) {
		for _, n := range list {
			if sts, ok := idx[n.Code]; ok {
				clone := make([]*Node, len(sts))
				for i, s := range sts {
					clone[i] = &Node{Code: s.Code, Name: s.Name}
				}
				n.Children = clone
			}
			walk(n.Children)
		}
	}
	walk(roots)
}
