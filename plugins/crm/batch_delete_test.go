package crm

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// 批量删除的回归基线。三条守卫各配一条「把它拆掉就该红」的用例：
//   - scopedIDs 的 owner 裁剪（删掉 tx.Where(cond, args...) → 用例 2 红）；
//   - 不可见即 404 且一写都不发（提前返回被去掉 → 用例 3 红）；
//   - 仍挂商机的客户计入 blocked、全部占用才 409（exclude/inUse 判据被去掉 → 用例 1、4 红）。
//
// sqlmock 只做正则匹配、从不执行 SQL，所以每条期望都把 SQL 片段与 WithArgs 一起钉死：
// 参数少了/多了、条件顺序变了都会失配，而不是「看起来还是那条语句」就放过。

func idRows(ids ...int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id"})
	for _, id := range ids {
		rows.AddRow(id)
	}
	return rows
}

func permScope() *datascope.Scope { return &datascope.Scope{All: true} }

// 用例 1：混合场景。点名 3 个客户，1002 仍挂商机 → 只删 1001/1003，
// 且在同一个事务里按 customer_id 级联清掉联系人与跟进记录。
func TestDeleteCustomersBatchMixedInUse(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// scopedIDs：All 范围不裁剪 owner，id IN 三点名。
	m.ExpectQuery("SELECT `id` FROM `plg_crm_customer` WHERE id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(1001), int64(1002), int64(1003)).
		WillReturnRows(idRows(1001, 1002, 1003))
	// customerIDsInUse：DISTINCT customer_id 只回 1002。
	m.ExpectQuery("SELECT DISTINCT `customer_id` FROM `plg_crm_opportunity` WHERE customer_id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(1001), int64(1002), int64(1003)).
		WillReturnRows(idRows(1002))
	// 级联删：free 只剩 1001/1003，联系人与跟进的 IN 参数必须同批、不带 1002。
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_crm_contact` WHERE customer_id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 4))
	m.ExpectExec("DELETE FROM `plg_crm_activity` WHERE customer_id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 7))
	m.ExpectExec("DELETE FROM `plg_crm_customer` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectCommit()

	deleted, blocked, err := s.DeleteCustomers(tenantCtx, permScope(), []idgen.ID{1001, 1002, 1003})
	if err != nil {
		t.Fatal(err)
	}
	// blocked = 点名总数 - 实删：1002 挂着商机没删，但不能让整批失败。
	if deleted != 2 || blocked != 1 {
		t.Fatalf("want deleted=2 blocked=1, got deleted=%d blocked=%d", deleted, blocked)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用例 2：受限数据范围。SELECT 必须带上 UsernameCond 生成的 owner 子查询，
// 且裁剪发生在占用判定之前——少了这一步，别人的客户会被当成自己的删掉。
func TestDeleteCustomersBatchAppliesScopeCond(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// self 范围（UserID=42、无部门）→ UsernameCond 出 `owner IN (SELECT ...)`，
	// GORM 把裸字符串条件整体括在括号里：`AND (owner IN (...))`。
	m.ExpectQuery("SELECT `id` FROM `plg_crm_customer` WHERE id IN \\(\\?,\\?,\\?\\) "+
		"AND \\(owner IN \\(SELECT username FROM sys_user WHERE id = \\? AND deleted_at IS NULL\\)\\)").
		WithArgs(int64(1001), int64(1002), int64(1003), int64(42)).
		WillReturnRows(idRows(1001, 1003)) // 1002 归别人，可见集合里没有它
	m.ExpectQuery("SELECT DISTINCT `customer_id` FROM `plg_crm_opportunity` WHERE customer_id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnRows(sqlmock.NewRows([]string{"customer_id"}))
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_crm_contact` WHERE customer_id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM `plg_crm_activity` WHERE customer_id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("DELETE FROM `plg_crm_customer` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1003)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectCommit()

	deleted, blocked, err := s.DeleteCustomers(
		tenantCtx, &datascope.Scope{UserID: 42, Preset: "self"}, []idgen.ID{1001, 1002, 1003})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 || blocked != 1 {
		t.Fatalf("want deleted=2 blocked=1, got deleted=%d blocked=%d", deleted, blocked)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用例 3：点名的客户一条都不在可见范围内 → 404，且不发任何写操作
// （这里只注册了那条 SELECT，任何 DELETE/UPDATE 都会以「未注册期望」失败）。
func TestDeleteCustomersBatchAllOutsideScopeIsNotFound(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT `id` FROM `plg_crm_customer` WHERE id IN \\(\\?,\\?\\) "+
		"AND \\(owner IN \\(SELECT username FROM sys_user WHERE id = \\? AND deleted_at IS NULL\\)\\)").
		WithArgs(int64(1001), int64(1002), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	deleted, blocked, err := s.DeleteCustomers(
		tenantCtx, &datascope.Scope{UserID: 42, Preset: "self"}, []idgen.ID{1001, 1002})
	if !containsKey(err, "crm.not_found") {
		t.Fatalf("want crm.not_found, got %v", err)
	}
	if deleted != 0 || blocked != 2 {
		t.Fatalf("want deleted=0 blocked=2, got deleted=%d blocked=%d", deleted, blocked)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用例 4：可见客户全都仍挂商机 → 与单条同口径的 409 crm.customer_in_use，同样不能有写操作。
func TestDeleteCustomersBatchAllInUseIsConflict(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT `id` FROM `plg_crm_customer` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1002)).
		WillReturnRows(idRows(1001, 1002))
	m.ExpectQuery("SELECT DISTINCT `customer_id` FROM `plg_crm_opportunity` WHERE customer_id IN \\(\\?,\\?\\)").
		WithArgs(int64(1001), int64(1002)).
		WillReturnRows(idRows(1001, 1002))

	deleted, blocked, err := s.DeleteCustomers(tenantCtx, permScope(), []idgen.ID{1001, 1002})
	if !containsKey(err, "crm.customer_in_use") {
		t.Fatalf("want crm.customer_in_use, got %v", err)
	}
	if deleted != 0 || blocked != 2 {
		t.Fatalf("want deleted=0 blocked=2, got deleted=%d blocked=%d", deleted, blocked)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 用例 5：商机批量删只删可见的那几条，范围外的计入 blocked；商机没有级联，一条 DELETE 收尾。
func TestDeleteOpportunitiesBatchDeletesVisibleOnly(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectQuery("SELECT `id` FROM `plg_crm_opportunity` WHERE id IN \\(\\?,\\?,\\?\\) "+
		"AND \\(owner IN \\(SELECT username FROM sys_user WHERE id = \\? AND deleted_at IS NULL\\)\\)").
		WithArgs(int64(3001), int64(3002), int64(3003), int64(42)).
		WillReturnRows(idRows(3001, 3003))
	// 单条 Delete 没写显式事务，GORM 的默认事务包装（SkipDefaultTransaction=false）仍会 Begin/Commit。
	m.ExpectBegin()
	// 跟进记录摘掉商机引用（记录本身留给客户时间线），参数按 SET opportunity_id=?, updated_at=? + WHERE IN 排。
	m.ExpectExec("UPDATE `plg_crm_activity` SET").
		WithArgs(int64(0), sqlmock.AnyArg(), int64(3001), int64(3003)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("DELETE FROM `plg_crm_opportunity` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(3001), int64(3003)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectCommit()

	deleted, blocked, err := s.DeleteOpportunities(
		tenantCtx, &datascope.Scope{UserID: 42, Preset: "self"}, []idgen.ID{3001, 3002, 3003})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 || blocked != 1 {
		t.Fatalf("want deleted=2 blocked=1, got deleted=%d blocked=%d", deleted, blocked)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
