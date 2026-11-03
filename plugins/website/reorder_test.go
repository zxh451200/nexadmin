package website

// ReorderNavs 的孤儿行兜底：父级已被并发删除的行会在整树校验前被静默洗回顶级。
// sqlmock 验证「先查出全表、对孤儿各发一条 UPDATE、再走正常校验与写入」的顺序。

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func navIdRow(id, parent int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "parent_id"}).AddRow(id, parent)
}

func TestReorderNavsWashesOrphans(t *testing.T) {
	db, m := mockDB(t)
	svc := &Service{db: db}

	// 三行：100（顶级）、200（父 100）、300（父 999 已不存在 → 孤儿）。
	m.ExpectBegin()
	m.ExpectQuery("SELECT .id.,.parent_id. FROM .plg_website_nav.").WillReturnRows(
		navIdRow(100, 0).AddRow(200, 100).AddRow(300, 999))
	// 孤儿 300 被洗回顶级。
	m.ExpectExec("UPDATE .plg_website_nav.").WillReturnResult(sqlmock.NewResult(0, 1))
	// 之后的整树校验基于洗过的 map，不再有 nav_parent_missing；排序写入两条。
	m.ExpectExec("UPDATE .plg_website_nav.").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE .plg_website_nav.").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	err := svc.ReorderNavs(context.Background(), []NavOrderItem{
		{ID: 200, ParentID: 100, Sort: 10},
		{ID: 300, ParentID: 0, Sort: 20},
	})
	if err != nil {
		t.Fatalf("reorder with orphan should succeed: %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
