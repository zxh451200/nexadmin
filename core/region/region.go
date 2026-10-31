// Package region 是内置的省市区数据（go:embed，不建表、无迁移）。
// 业务表只存 6 位行政区划代码；本包提供树/下级查询和代码校验。
//
// data/regions.json 是随包发布的完整快照（31 省级 / 333 地级 / 2847 县级），
// 由 scripts/region_fetch 从公开数据源生成、scripts/region_import 校验，来源与许可证见
// data/manifest.json。运行时可用 Activate 覆盖为同步下来的新版本，Reset 退回内置快照。
package region

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

//go:embed data/regions.json
var rawData []byte

//go:embed data/regions_history.json
var rawHistory []byte

//go:embed data/manifest.json
var rawManifest []byte

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

// Info 描述当前生效的数据版本；Builtin 为 true 表示仍在用随包发布的快照。
type Info struct {
	Builtin     bool   `json:"builtin"`
	SourceURL   string `json:"sourceUrl"`
	DataDate    string `json:"dataDate"`
	License     string `json:"license"`
	SHA256      string `json:"sha256"`
	Provinces   int    `json:"provinces"`
	Prefectures int    `json:"prefectures"`
	Counties    int    `json:"counties"`
	SyncedBy    string `json:"syncedBy"`
	SyncedAt    string `json:"syncedAt"`
}

type snapshot struct {
	roots  []*Node
	byCode map[string]*Node
	info   Info
}

var (
	once      sync.Once
	builtin   *snapshot
	builtinEr error
	active    atomic.Pointer[snapshot]
	history   map[string]Historical
)

// 大陆 31 个省级行政区；港澳台用的是非国标代码段，不在省市区数据范围内。
var mainlandPrefixes = map[string]bool{
	"11": true, "12": true, "13": true, "14": true, "15": true,
	"21": true, "22": true, "23": true,
	"31": true, "32": true, "33": true, "34": true, "35": true, "36": true, "37": true,
	"41": true, "42": true, "43": true, "44": true, "45": true, "46": true,
	"50": true, "51": true, "52": true, "53": true, "54": true,
	"61": true, "62": true, "63": true, "64": true, "65": true,
}

// manifest 是随包数据的来源记录，由 scripts/region_import 生成。
type manifest struct {
	SourceURL string `json:"sourceURL"`
	DataDate  string `json:"dataDate"`
	License   string `json:"license"`
}

func builtinInfo() Info {
	info := Info{Builtin: true}
	var m manifest
	if err := json.Unmarshal(rawManifest, &m); err != nil {
		return info // 缺 manifest 只影响「来源」展示，不影响数据可用性
	}
	info.SourceURL, info.DataDate, info.License = m.SourceURL, m.DataDate, m.License
	return info
}

func load() error {
	once.Do(func() {
		snap, err := build(rawData, builtinInfo())
		if err != nil {
			builtinEr = fmt.Errorf("region: bad embedded data: %w", err)
			return
		}
		builtin = snap
		history = map[string]Historical{}
		if err := json.Unmarshal(rawHistory, &history); err != nil {
			builtinEr = fmt.Errorf("region: bad embedded history data: %w", err)
		}
	})
	return builtinEr
}

func current() *snapshot {
	if err := load(); err != nil {
		return nil
	}
	if s := active.Load(); s != nil {
		return s
	}
	return builtin
}

// Current 返回当前生效的数据版本信息。
func Current() Info {
	if s := current(); s != nil {
		return s.info
	}
	return Info{}
}

// Activate 校验并切换为 raw 描述的完整三级树；失败时保持原数据不变。
// meta 里的来源、版本、许可证等由调用方提供，条数与摘要由本包按 raw 计算。
func Activate(raw []byte, meta Info) (Info, error) {
	if err := load(); err != nil {
		return Info{}, err
	}
	meta.Builtin = false
	snap, err := build(raw, meta)
	if err != nil {
		return Info{}, err
	}
	active.Store(snap)
	return snap.info, nil
}

// Validate 校验 raw 是否是完整可用的三级树；只解析不切换当前数据。
func Validate(raw []byte, meta Info) (Info, error) {
	if err := load(); err != nil {
		return Info{}, err
	}
	meta.Builtin = false
	snap, err := build(raw, meta)
	if err != nil {
		return Info{}, err
	}
	return snap.info, nil
}

// Reset 退回随包发布的内置快照。
func Reset() Info {
	active.Store(nil)
	return Current()
}

// Encode 把树编码为 Activate 接受的字节（紧凑 JSON）。
func Encode(roots []*Node) ([]byte, error) {
	return json.Marshal(roots)
}

// FromFlat 把 code→name 的扁平表（公开数据集常见格式）转成三级树字节。
// 直辖市的区县与省直辖县级市直接挂在省级下面，与内置快照的结构一致。
func FromFlat(flat map[string]string) ([]byte, error) {
	codes := make([]string, 0, len(flat))
	for code := range flat {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	var roots []*Node
	index := map[string]*Node{}
	for _, code := range codes {
		name := strings.TrimSpace(flat[code])
		if len(code) != 6 || !mainlandPrefixes[code[:2]] || name == "" {
			continue
		}
		node := &Node{Code: code, Name: name}
		switch {
		case strings.HasSuffix(code, "0000"):
			roots = append(roots, node)
		case strings.HasSuffix(code, "00"):
			parent := index[code[:2]+"0000"]
			if parent == nil {
				return nil, fmt.Errorf("region: %s %s 缺少所属省级", code, name)
			}
			parent.Children = append(parent.Children, node)
		default:
			parent := index[code[:4]+"00"]
			if parent == nil {
				parent = index[code[:2]+"0000"]
			}
			if parent == nil {
				return nil, fmt.Errorf("region: %s %s 缺少上级", code, name)
			}
			parent.Children = append(parent.Children, node)
		}
		index[code] = node
	}
	return Encode(roots)
}

// 单次同步的数据量上限（解开后约 300KB，留足余量）。
const maxNodes = 20000

func build(raw []byte, meta Info) (*snapshot, error) {
	var roots []*Node
	if err := json.Unmarshal(raw, &roots); err != nil {
		return nil, fmt.Errorf("region: 解析失败: %w", err)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("region: 数据为空")
	}
	byCode := map[string]*Node{}
	total := 0
	var counts [3]int
	var walk func(list []*Node, depth int, parent string) error
	walk = func(list []*Node, depth int, parent string) error {
		for _, n := range list {
			if n == nil {
				return fmt.Errorf("region: 存在空节点")
			}
			total++
			if total > maxNodes {
				return fmt.Errorf("region: 节点数超过 %d", maxNodes)
			}
			name := strings.TrimSpace(n.Name)
			if name == "" || name != n.Name {
				return fmt.Errorf("region: 名称缺失或含首尾空白: %q", n.Name)
			}
			lvl, err := codeLevel(n.Code)
			if err != nil {
				return err
			}
			if depth > 3 || lvl < depth {
				return fmt.Errorf("region: %s %s 层级与位置不符", n.Code, n.Name)
			}
			if depth == 1 && lvl != 1 {
				return fmt.Errorf("region: 省级 %s 代码不规范", n.Code)
			}
			if depth > 1 && !parentMatches(parent, n.Code, lvl) {
				return fmt.Errorf("region: %s %s 不属于上级 %s", n.Code, n.Name, parent)
			}
			if _, dup := byCode[n.Code]; dup {
				return fmt.Errorf("region: 代码重复 %s", n.Code)
			}
			byCode[n.Code] = n
			counts[lvl-1]++
			if n.Pinyin == "" {
				n.Pinyin = Pinyin(n.Name)
			}
			if err := walk(n.Children, depth+1, n.Code); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(roots, 1, ""); err != nil {
		return nil, err
	}
	if counts[0] != len(mainlandPrefixes) {
		return nil, fmt.Errorf("region: 省级应为大陆 %d 个，实际 %d", len(mainlandPrefixes), counts[0])
	}
	for _, n := range roots {
		if len(n.Children) == 0 {
			return nil, fmt.Errorf("region: 省级 %s %s 没有下级", n.Code, n.Name)
		}
	}
	if counts[1] < 300 || counts[2] < 2000 {
		return nil, fmt.Errorf("region: 地级/县级数据不完整（%d/%d）", counts[1], counts[2])
	}
	meta.SHA256 = sha256Hex(raw)
	meta.Provinces, meta.Prefectures, meta.Counties = counts[0], counts[1], counts[2]
	return &snapshot{roots: roots, byCode: byCode, info: meta}, nil
}

func codeLevel(code string) (int, error) {
	if len(code) != 6 {
		return 0, fmt.Errorf("region: 代码 %q 不是 6 位", code)
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("region: 代码 %q 含非数字", code)
		}
	}
	if !mainlandPrefixes[code[:2]] {
		return 0, fmt.Errorf("region: 代码 %q 不在大陆省级范围内", code)
	}
	switch {
	case strings.HasSuffix(code, "0000"):
		return 1, nil
	case strings.HasSuffix(code, "00"):
		return 2, nil
	}
	return 3, nil
}

// parentMatches：普通地级市/下辖县按 4 位前缀归属；直辖市的区县与省直辖县级市直接挂在省下。
func parentMatches(parent, code string, lvl int) bool {
	if parent == code[:2]+"0000" {
		return true
	}
	return lvl == 3 && parent == code[:4]+"00"
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
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
	snap := current()
	if snap == nil {
		return nil, load()
	}
	if level < 1 || level > 3 {
		level = 3
	}
	return trim(snap.roots, level), nil
}

// Children 返回 parent 的直接下级（不含更深层）；parent 为空返回省级列表，代码不存在返回 false。
func Children(parent string) ([]*Node, bool, error) {
	snap := current()
	if snap == nil {
		return nil, false, load()
	}
	if parent == "" {
		return trim(snap.roots, 1), true, nil
	}
	n, ok := snap.byCode[parent]
	if !ok {
		return nil, false, nil
	}
	return trim(n.Children, 1), true, nil
}

// Name 返回行政区划代码对应的名称。
func Name(code string) (string, bool) {
	snap := current()
	if snap == nil {
		return "", false
	}
	n, ok := snap.byCode[code]
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
	if snap := current(); snap != nil {
		if n, ok := snap.byCode[code]; ok {
			return n.Name, false
		}
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
	if snap := current(); snap != nil {
		if _, ok := snap.byCode[strings.TrimSpace(code)]; ok {
			return Historical{}, false
		}
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
	snap := current()
	out := make([]string, 0, len(h.Successors))
	for _, next := range h.Successors {
		if snap != nil {
			if _, exists := snap.byCode[next]; exists {
				out = append(out, next)
			}
		}
	}
	return out
}

// Check 校验省/市/区代码存在且上下级一致；city、district 可为空（只填到哪级校验到哪级）。
func Check(province, city, district string) error {
	snap := current()
	if snap == nil {
		return load()
	}
	p, ok := snap.byCode[province]
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
