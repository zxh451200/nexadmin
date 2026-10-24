// Package region 是内置的省市区数据（go:embed，不建表、无迁移）。
// 业务表只存 6 位行政区划代码；本包提供树/下级查询和代码校验。
// 注意：data/regions.json 目前只含与前端 mock 相同的 5 个省份样例，完整的统计局三级数据需由框架脚本按年生成后覆盖该文件。
package region

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/regions.json
var rawData []byte

//go:embed data/regions_history.json
var rawHistory []byte

// Node 是一个行政区划节点，字段与前端 RegionNode 一致。
type Node struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Pinyin   string  `json:"pinyin"`
	Children []*Node `json:"children,omitempty"`
}

// Historical 是已撤销或改名的行政区划代码。
// Successors 只记录已知继任代码；空表示没有可靠的继任映射。
type Historical struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Successors []string `json:"successors,omitempty"`
}

var (
	once    sync.Once
	roots   []*Node
	byCode  map[string]*Node
	history map[string]Historical
	loadEr  error
)

func load() error {
	once.Do(func() {
		if err := json.Unmarshal(rawData, &roots); err != nil {
			loadEr = fmt.Errorf("region: bad embedded data: %w", err)
			return
		}
		byCode = map[string]*Node{}
		var walk func(list []*Node)
		walk = func(list []*Node) {
			for _, n := range list {
				byCode[n.Code] = n
				walk(n.Children)
			}
		}
		walk(roots)
		history = map[string]Historical{}
		if err := json.Unmarshal(rawHistory, &history); err != nil {
			loadEr = fmt.Errorf("region: bad embedded history data: %w", err)
			return
		}
	})
	return loadEr
}

// trim 复制节点，只保留到 depth 层（1 = 只有本层）。
func trim(list []*Node, depth int) []*Node {
	out := make([]*Node, 0, len(list))
	for _, n := range list {
		c := &Node{Code: n.Code, Name: n.Name, Pinyin: n.Pinyin}
		if depth > 1 {
			c.Children = trim(n.Children, depth-1)
			if len(c.Children) == 0 {
				c.Children = nil
			}
		}
		out = append(out, c)
	}
	return out
}

// Tree 返回前 level（1-3）级的完整树。
func Tree(level int) ([]*Node, error) {
	if err := load(); err != nil {
		return nil, err
	}
	if level < 1 || level > 3 {
		level = 3
	}
	return trim(roots, level), nil
}

// Children 返回 parent 的直接下级（不含更深层）；parent 为空返回省级列表，代码不存在返回 false。
func Children(parent string) ([]*Node, bool, error) {
	if err := load(); err != nil {
		return nil, false, err
	}
	if parent == "" {
		return trim(roots, 1), true, nil
	}
	n, ok := byCode[parent]
	if !ok {
		return nil, false, nil
	}
	return trim(n.Children, 1), true, nil
}

// Name 返回行政区划代码对应的名称。
func Name(code string) (string, bool) {
	if load() != nil {
		return "", false
	}
	n, ok := byCode[code]
	if !ok {
		return "", false
	}
	return n.Name, true
}

// Resolve 返回代码的显示名称。现行代码优先，历史代码其次。
// 对未知代码返回原代码作为显示值，避免历史业务记录因数据不全而显示为空。
func Resolve(code string) (name string, historical bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", false
	}
	if err := load(); err != nil {
		return code, false
	}
	if n, ok := byCode[code]; ok {
		return n.Name, false
	}
	if h, ok := history[code]; ok && h.Name != "" {
		return h.Name, true
	}
	return code, false
}

// HistoricalInfo 返回历史代码信息；现行代码和未知代码均返回 false。
func HistoricalInfo(code string) (Historical, bool) {
	if load() != nil {
		return Historical{}, false
	}
	h, ok := history[strings.TrimSpace(code)]
	return h, ok
}

// CurrentCodes 返回历史代码对应的已知继任代码，结果只包含现行区划。
func CurrentCodes(code string) []string {
	h, ok := HistoricalInfo(code)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(h.Successors))
	for _, next := range h.Successors {
		if _, exists := byCode[next]; exists {
			out = append(out, next)
		}
	}
	return out
}

// Check 校验省/市/区代码存在且上下级一致；city、district 可为空（只填到哪级校验到哪级）。
func Check(province, city, district string) error {
	if err := load(); err != nil {
		return err
	}
	p, ok := byCode[province]
	if !ok {
		return fmt.Errorf("region: unknown province code %q", province)
	}
	if city == "" {
		if district != "" {
			return fmt.Errorf("region: district %q given without city", district)
		}
		return nil
	}
	c := find(p.Children, city)
	if c == nil {
		return fmt.Errorf("region: city %q does not belong to province %q", city, province)
	}
	if district != "" && find(c.Children, district) == nil {
		return fmt.Errorf("region: district %q does not belong to city %q", district, city)
	}
	return nil
}

func find(list []*Node, code string) *Node {
	for _, n := range list {
		if n.Code == code {
			return n
		}
	}
	return nil
}
