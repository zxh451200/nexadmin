package menu

import (
	"errors"
	"testing"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 按钮是权限点：脱离父菜单就成了孤儿行（侧栏看不见、授权树里也定位不到归属），
// 新增/修改与拖拽共用这条判定，所以三条路都得拦下。
func TestCheckButtonParent(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		pid     idgen.ID
		wantErr bool
	}{
		{"按钮挂到根级", typeButton, 0, true},
		{"按钮挂在菜单下", typeButton, 7, false},
		{"菜单挂到根级", typeMenu, 0, false},
		{"目录挂到根级", typeDir, 0, false},
	}
	for _, c := range cases {
		err := checkButtonParent(c.typ, c.pid)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
		if err == nil {
			continue
		}
		var re *response.Error
		if !errors.As(err, &re) || re.Key != "menu.button_needs_parent" {
			t.Fatalf("%s: 期望 key menu.button_needs_parent，实得 %v", c.name, err)
		}
	}
}

// check 是新增/修改的入口：没填上级的按钮必须在这里就拒掉，别等落库。
func TestMenuInputCheckRejectsParentlessButton(t *testing.T) {
	orphan := menuInput{Type: typeButton, Title: "执行导入", Perm: "imports:run"}
	if _, err := orphan.check(); err == nil {
		t.Fatal("按钮未填上级却通过校验")
	}
	attached := menuInput{Type: typeButton, Title: "执行导入", Perm: "imports:run", ParentID: "123"}
	if _, err := attached.check(); err != nil {
		t.Fatalf("按钮带上级不应报错：%v", err)
	}
	topMenu := menuInput{Type: typeMenu, Title: "导入中心", Path: "/imports/jobs"}
	if _, err := topMenu.check(); err != nil {
		t.Fatalf("菜单挂顶级不应报错：%v", err)
	}
}

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
