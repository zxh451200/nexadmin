package menu

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// 出厂菜单 ID 按 buildSeed 的定义顺序发号：一旦有人把新菜单插在中间，后面所有条目的
// 提议 ID 都会顺移并撞上存量库里已有的行，启动直接失败（Duplicate entry）。
// reserveFreeIDs 必须把「撞号的新条目」让到号段尾部空号，而「已存在的 code」一律不动。
func TestReserveFreeIDsRelocatesCollidingNewMenus(t *testing.T) {
	db, mk := mockDB(t)
	items := buildSeed()
	if len(items) < 60 {
		t.Fatalf("出厂条目数=%d，构造不出对账场景", len(items))
	}
	const base = factoryIDBase
	ghostAt := base + 36         // 第 36 号被一个存量行的别的 code 占着 → 该位新条目必须让号
	heldCode := items[49].m.Code // 第 50 号就是它自己的 code → 已存在，不该动
	heldAt := base + 50
	mk.ExpectQuery("SELECT .+ FROM `sys_menu`").WillReturnRows(
		sqlmock.NewRows([]string{"id", "code"}).
			AddRow(heldAt, heldCode).
			AddRow(ghostAt, "ghost.code.from.other.row"),
	)

	before36 := items[35].m.ID
	before50 := items[49].m.ID
	if err := reserveFreeIDs(db, items); err != nil {
		t.Fatal(err)
	}
	if items[35].m.ID == before36 {
		t.Fatalf("撞号的新条目没被让位：仍是 %d（该号被别的 code 占着）", int64(before36))
	}
	if int64(items[35].m.ID) <= base+50 {
		t.Fatalf("让位后的 ID=%d 应落在号段尾部的空号（> %d）", int64(items[35].m.ID), base+50)
	}
	if items[49].m.ID != before50 {
		t.Fatalf("已存在的 code %q 的 ID 被动了：%d → %d", heldCode, int64(before50), int64(items[49].m.ID))
	}
	// 其余条目不受影响
	moved := 0
	for i, it := range items {
		if int64(it.m.ID) != base+int64(i+1) {
			moved++
		}
	}
	if moved != 1 {
		t.Errorf("应只有 1 个条目被让号，实际 %d 个", moved)
	}
	if err := mk.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 新装（号段里没有存量行）时必须完全保持「base + 定义顺序」，与前端 mock、租户套餐号段一致。
func TestReserveFreeIDsKeepsSequentialOnFreshInstall(t *testing.T) {
	db, mk := mockDB(t)
	mk.ExpectQuery("SELECT .+ FROM `sys_menu`").WillReturnRows(sqlmock.NewRows([]string{"id", "code"}))
	items := buildSeed()
	if err := reserveFreeIDs(db, items); err != nil {
		t.Fatal(err)
	}
	for i, it := range items {
		if int64(it.m.ID) != factoryIDBase+int64(i+1) {
			t.Fatalf("第 %d 个条目 ID 被改了：%d", i+1, int64(it.m.ID))
		}
	}
	if err := mk.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
