package menu

import (
	"testing"

	"nexadmin/pkg/idgen"
)

// placeAfter 决定「拖到第 N 行之后」究竟插在哪：落点是插入线，位置必须精确。
func TestPlaceAfter(t *testing.T) {
	mk := func(ids ...idgen.ID) []Menu {
		out := make([]Menu, 0, len(ids))
		for i, id := range ids {
			var row Menu
			row.ID = id
			row.Sort = (i + 1) * 10
			out = append(out, row)
		}
		return out
	}
	sibs := mk(1, 2, 3)
	got := func(v []idgen.ID) []idgen.ID {
		t.Helper()
		return v
	}
	cases := []struct {
		name    string
		sibs    []Menu
		id      idgen.ID
		afterID string
		want    []idgen.ID
	}{
		{"插到最前", sibs, 9, "", got([]idgen.ID{9, 1, 2, 3})},
		{"插到中间", sibs, 9, "2", got([]idgen.ID{1, 2, 9, 3})},
		{"追加到末尾", sibs, 9, "3", got([]idgen.ID{1, 2, 3, 9})},
		{"同级不存在时兜底到末尾", sibs, 9, "404", got([]idgen.ID{1, 2, 3, 9})},
		{"目标组为空", nil, 9, "", got([]idgen.ID{9})},
	}
	for _, c := range cases {
		gotv := placeAfter(c.sibs, c.id, c.afterID)
		if len(gotv) != len(c.want) {
			t.Fatalf("%s: 长度 %d != %d (%v)", c.name, len(gotv), len(c.want), gotv)
		}
		for i := range gotv {
			if gotv[i] != c.want[i] {
				t.Fatalf("%s: 第 %d 项 %v != %v（整体 %v）", c.name, i, gotv[i], c.want[i], gotv)
			}
		}
	}
}
