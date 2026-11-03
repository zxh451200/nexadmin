package region

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPinyinKnownPolyphones(t *testing.T) {
	for name, want := range map[string]string{
		"重庆市":   "chongqing",
		"长沙市":   "changsha",
		"厦门市":   "xiamen",
		"西藏自治区": "xizang",
		"六安市":   "luan",
		"番禺区":   "panyu",
		"蔚县":    "yu",
		"单县":    "shan",
		"吕梁市":   "lvliang",
		"矿区":    "kuangqu",
		"北京市":   "beijing",
		"玄武区":   "xuanwu",
	} {
		if got := Pinyin(name); got != want {
			t.Errorf("Pinyin(%q) = %q, 期望 %q", name, got, want)
		}
	}
}

func TestBuiltinInfoCarriesProvenance(t *testing.T) {
	info := Current()
	if !info.Builtin {
		t.Fatal("未同步时应是内置")
	}
	if !strings.HasPrefix(info.SourceURL, "https://") {
		t.Errorf("内置数据应带 manifest 里的来源地址，得到 %q", info.SourceURL)
	}
	if info.DataDate == "" || info.License == "" {
		t.Errorf("内置数据应带数据日期与许可，得到 %q / %q", info.DataDate, info.License)
	}
}

func TestEmbeddedDatasetComplete(t *testing.T) {
	tree, err := Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 34 {
		t.Fatalf("省级 %d 个，期望 34（大陆 31 + 港澳台）", len(tree))
	}
	var provinces, prefectures, counties int
	var walk func(list []*Node, depth int)
	walk = func(list []*Node, depth int) {
		for _, n := range list {
			if n.Code == "" || n.Name == "" {
				t.Fatalf("节点缺少代码或名称: %+v", n)
			}
			if n.Pinyin == "" {
				t.Errorf("%s %s 没有拼音", n.Code, n.Name)
			}
			switch depth {
			case 1:
				provinces++
			case 2:
				prefectures++
			default:
				counties++
			}
			walk(n.Children, depth+1)
		}
	}
	walk(tree, 1)
	if provinces != 34 || prefectures < 300 || counties < 2000 {
		t.Fatalf("数据不完整: 省 %d / 地级 %d / 县级 %d", provinces, prefectures, counties)
	}
}

// 港澳台：内置快照只有大陆 31 省，港澳台由 data/regions_sar.json 补全后才生效。
func TestSpecialRegionsArePresent(t *testing.T) {
	tree, err := Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Node{}
	for _, n := range tree {
		got[n.Code] = n
	}
	for code, want := range map[string]string{
		"710000": "台湾省",
		"810000": "香港特别行政区",
		"820000": "澳门特别行政区",
	} {
		n := got[code]
		if n == nil || n.Name != want {
			t.Fatalf("省级 %s 应为 %s，得到 %+v", code, want, n)
		}
	}
	// 港澳有下级的区/堂区，台湾省只到省级（公开数据源不发布它的下级）。
	if len(got["710000"].Children) != 0 {
		t.Errorf("台湾省不该有编造的下级: %+v", got["710000"].Children)
	}
	if len(got["810000"].Children) != 18 {
		t.Errorf("香港下辖区 %d 个，期望 18", len(got["810000"].Children))
	}
	if len(got["820000"].Children) != 9 {
		t.Errorf("澳门下辖堂区 %d 个，期望 9", len(got["820000"].Children))
	}
	for code, want := range map[string]string{
		"810101": "中西区",
		"810118": "离岛区",
		"820103": "大堂区",
		"820109": "澳门新城",
	} {
		if name, historical := Resolve(code); name != want || historical {
			t.Errorf("Resolve(%s) = %q/%v，期望 %q", code, name, historical, want)
		}
	}
	// 省市区选择器把二级节点存进 city 字段（与直辖市同一形态），校验得认。
	if err := Check("810000", "810101", ""); err != nil {
		t.Errorf("港岛选择结果被拒: %v", err)
	}
	if err := Check("710000", "", ""); err != nil {
		t.Errorf("台湾省只填省级应通过: %v", err)
	}
}

// 港澳台区划的读音：通名表会剥掉「特别行政区/区/堂区」的「区」，剩下部分不能读错或读空。
// 大埔与广东大埔县取同一个默认读音（dapu），不单独给港澳改口径。
func TestSpecialRegionPinyin(t *testing.T) {
	for name, want := range map[string]string{
		"台湾省":     "taiwan",
		"香港特别行政区": "xianggang",
		"澳门特别行政区": "aomen",
		"中西区":     "zhongxi",
		"湾仔区":     "wanzai",
		"深水埗区":    "shenshuibu",
		"大埔区":     "dapu",
		"荃湾区":     "quanwan",
		"离岛区":     "lidao",
		"路氹城":     "ludangcheng",
		"花地玛堂区":   "huadimatang",
		"圣方济各堂区":  "shengfangjigetang",
	} {
		if got := Pinyin(name); got != want {
			t.Errorf("Pinyin(%q) = %q, 期望 %q", name, got, want)
		}
	}
}

// EnsureSpecialRegions 是同步链路的补全入口：缺则补，源里带了就以源为准。
func TestEnsureSpecialRegionsFillsGaps(t *testing.T) {
	full, err := Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	var mainlandOnly []*Node
	for _, n := range full {
		if n.Code[:2] != "71" && n.Code[:2] != "81" && n.Code[:2] != "82" {
			mainlandOnly = append(mainlandOnly, n)
		}
	}
	raw, err := Encode(mainlandOnly)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := EnsureSpecialRegions(raw)
	if err != nil {
		t.Fatal(err)
	}
	info, err := Validate(completed, Info{})
	if err != nil {
		t.Fatalf("补全后应可用: %v", err)
	}
	if info.Provinces != 34 {
		t.Fatalf("补全后省级 %d，期望 34", info.Provinces)
	}

	// 已经带了就不动：字节完全相同，落库摘要不会因为补全而变化。
	again, err := EnsureSpecialRegions(completed)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(completed) {
		t.Error("重复补全不应改动数据")
	}

	// 源里自己的写法优先，不用内置名称覆盖。
	for _, n := range full {
		if n.Code == "810000" {
			n.Name = "香港特別行政區"
		}
	}
	raw, err = Encode(full)
	if err != nil {
		t.Fatal(err)
	}
	again, err = EnsureSpecialRegions(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Error("源里已有港澳台时应原样保留")
	}
}

func TestEmbeddedMunicipalitiesHaveDistrictsDirectly(t *testing.T) {
	list, ok, err := Children("110000")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(list) < 10 {
		t.Fatalf("北京市下级 %d 个，期望区县直接挂在省下", len(list))
	}
	for _, n := range list {
		if n.Name == "市辖区" {
			t.Fatal("直辖市不应出现「市辖区」这一层")
		}
	}
}

func TestActivateReplacesAndResetRestores(t *testing.T) {
	t.Cleanup(func() { Reset() })
	raw, err := json.Marshal([]*Node{{
		Code: "110000", Name: "北京市改名测试",
		Children: []*Node{{Code: "110101", Name: "东城区"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Activate(raw, Info{SourceURL: "https://example.com/r.json"}); err == nil {
		t.Fatal("省级不全的数据应被拒绝")
	}
	if name, _ := Resolve("110000"); name != "北京市" {
		t.Fatalf("失败的激活不应改动现有数据，得到 %q", name)
	}

	tree, err := Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	tree[0].Name = "北京市改名测试"
	raw, err = Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	info, err := Activate(raw, Info{SourceURL: "https://example.com/r.json", DataDate: "2026-04-18"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Builtin || info.Provinces != 34 || info.SHA256 == "" || info.SourceURL != "https://example.com/r.json" {
		t.Fatalf("激活结果不对: %+v", info)
	}
	if name, _ := Resolve("110000"); name != "北京市改名测试" {
		t.Fatalf("激活后名称未生效: %q", name)
	}
	if Current().Builtin {
		t.Fatal("Current 应显示非内置")
	}
	Reset()
	if name, _ := Resolve("110000"); name != "北京市" {
		t.Fatalf("Reset 后未恢复内置数据: %q", name)
	}
	if !Current().Builtin {
		t.Fatal("Reset 后 Builtin 应为 true")
	}
}

func TestActivateRejectsBrokenTrees(t *testing.T) {
	t.Cleanup(func() { Reset() })
	cases := map[string][]*Node{
		"空数据": nil,
		"代码位数不对": {
			{Code: "11000", Name: "北京市改名测试"},
		},
		"代码段不在范围内": {
			{Code: "990000", Name: "不存在省"},
		},
		"代码重复": {
			{Code: "110000", Name: "甲", Children: []*Node{{Code: "110101", Name: "乙"}, {Code: "110101", Name: "丙"}}},
		},
		"上级不匹配": {
			{Code: "110000", Name: "甲", Children: []*Node{{Code: "320102", Name: "玄武区"}}},
		},
	}
	for name, tree := range cases {
		raw, err := json.Marshal(tree)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Activate(raw, Info{}); err == nil {
			t.Errorf("%s: 应被拒绝", name)
		}
	}
}

func TestFromFlatBuildsThreeLevels(t *testing.T) {
	raw, err := FromFlat(map[string]string{
		"110000": "北京市",
		"110101": "东城区",
		"320000": "江苏省",
		"320100": "南京市",
		"320102": "玄武区",
		"810000": "香港特别行政区",
		"810101": "中西区",
		"abc":    "无效",
	})
	if err != nil {
		t.Fatal(err)
	}
	var roots []*Node
	if err := json.Unmarshal(raw, &roots); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 3 {
		t.Fatalf("省级 %d 个，期望 3（港澳台代码段一并收下）", len(roots))
	}
	if roots[0].Children[0].Code != "110101" {
		t.Fatalf("直辖市的区县应直接挂在省下: %+v", roots[0].Children)
	}
	if roots[1].Children[0].Children[0].Name != "玄武区" {
		t.Fatalf("普通省份的下级结构不对: %+v", roots[1])
	}
	// 港澳没有地级这一层，区/堂区直接挂省级——和直辖市同一形态，选择器才能少一列。
	if roots[2].Code != "810000" || len(roots[2].Children) != 1 || roots[2].Children[0].Code != "810101" {
		t.Fatalf("香港的下级应直接挂在省下: %+v", roots[2])
	}

	if _, err := FromFlat(map[string]string{"320102": "玄武区"}); err == nil {
		t.Fatal("缺少上级时应报错")
	}
}

func TestFromFlatRoundTrip(t *testing.T) {
	raw := []byte(`{"110000":"北京市","110101":"东城区"}`)
	var flat map[string]string
	if err := json.Unmarshal(raw, &flat); err != nil {
		t.Fatal(err)
	}
	tree, err := FromFlat(flat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Activate(tree, Info{}); err == nil || !strings.Contains(err.Error(), "省级") {
		t.Fatalf("省级不全时应拒绝，得到 %v", err)
	}
}
