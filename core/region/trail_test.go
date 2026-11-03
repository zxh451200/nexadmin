package region

import (
	"reflect"
	"testing"
)

func TestTrail(t *testing.T) {
	cases := []struct {
		code  string
		path  []string
		level int
	}{
		{"410181", []string{"河南省", "郑州市", "巩义市"}, 3},
		// 直辖市这一份数据里没有「市辖区」中间层，路径两级但确实命中到县级。
		{"110101", []string{"北京市", "东城区"}, 3},
		{"810000", []string{"香港特别行政区"}, 1},
		// 省直辖县级单位：地级那一层本来就不存在，跳过而不是判失败。
		{"419001", []string{"河南省", "济源市"}, 3},
		// 证件口径的 830000 不在区划表里（区划表用 710000 台湾省），这里按"查不到"返回。
		{"830000", nil, 0},
		{"999999", nil, 0},
		{"41018", nil, 0},
	}
	for _, tc := range cases {
		path, level := Trail(tc.code)
		if !reflect.DeepEqual(path, tc.path) || level != tc.level {
			t.Errorf("Trail(%q) = (%v, %d), want (%v, %d)", tc.code, path, level, tc.path, tc.level)
		}
	}
}
