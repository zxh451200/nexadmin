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
	if len(tree) != 31 {
		t.Fatalf("省级 %d 个，期望 31", len(tree))
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
	if provinces != 31 || prefectures < 300 || counties < 2000 {
		t.Fatalf("数据不完整: 省 %d / 地级 %d / 县级 %d", provinces, prefectures, counties)
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
	if info.Builtin || info.Provinces != 31 || info.SHA256 == "" || info.SourceURL != "https://example.com/r.json" {
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
		"非大陆代码": {
			{Code: "810000", Name: "香港特别行政区"},
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
		"abc":    "无效",
	})
	if err != nil {
		t.Fatal(err)
	}
	var roots []*Node
	if err := json.Unmarshal(raw, &roots); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("省级 %d 个，期望 2（港澳台应被忽略）", len(roots))
	}
	if roots[0].Children[0].Code != "110101" {
		t.Fatalf("直辖市的区县应直接挂在省下: %+v", roots[0].Children)
	}
	if roots[1].Children[0].Children[0].Name != "玄武区" {
		t.Fatalf("普通省份的下级结构不对: %+v", roots[1])
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
