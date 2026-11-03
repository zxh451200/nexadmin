package ticket

// 合并重复工单 + 关联单的用例。夹具复用 service_test.go 的 mockDB / nopLogger / testNow（不修改它）。
// 边界矩阵逐条覆盖：自合并、被合并方再合并、终态单当主单、链路成环、坏数据的跳数上限、
// 并发反向合并的加锁定序，以及「回复与附件不搬」——每个用例都按精确语句序列断言，
// 一旦实现去 UPDATE/INSERT plg_ticket_reply 或 plg_ticket_attachment，sqlmock 会直接报 unexpected call。

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// mergeRow 是 mergeTicket 投影行的夹具；mergedInto 传 nil 表示这张单没被合并过。
func mergeRow(no string, id int64, status string, mergedInto any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "ticket_no", "biz_type", "biz_no", "title", "status", "priority",
		"merged_into_id", "closed_at", "resolution_due_at", "sla_paused_at",
	}).AddRow(id, no, "trade_group", "G-1", "order broken", status, PriorityNormal, mergedInto, nil, nil, nil)
}

func linkRows(pairs [][2]int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "created_at", "updated_at", "ticket_a_id", "ticket_b_id", "link_by"})
	for i, pair := range pairs {
		rows.AddRow(int64(3001+i), testNow, testNow, pair[0], pair[1], "alice")
	}
	return rows
}

func countRow(n int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count"}).AddRow(n)
}

func emptyRow(columns ...string) *sqlmock.Rows {
	if len(columns) == 0 {
		columns = []string{"id"}
	}
	return sqlmock.NewRows(columns)
}

// mustKey 断言「HTTP 状态 + i18n key」都对：只校验状态码会让写错的 key 悄悄上线。
func mustKey(t *testing.T, err error, status int, key string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want %d %s, got nil", status, key)
	}
	var e *response.Error
	if !errors.As(err, &e) {
		t.Fatalf("want %d %s, got %T: %v", status, key, err, err)
	}
	if e.Status != status || e.Key != key {
		t.Fatalf("got %d/%s, want %d/%s", e.Status, e.Key, status, key)
	}
}

const (
	sqlTicketByNo   = "SELECT .* FROM `plg_ticket` WHERE ticket_no = \\?"
	sqlTicketByID   = "SELECT .* FROM `plg_ticket` WHERE id = \\?"
	sqlTicketIn     = "SELECT .* FROM `plg_ticket` WHERE id IN"
	sqlTicketUpdate = "UPDATE `plg_ticket` SET"
	sqlActivity     = "INSERT INTO `plg_ticket_activity`"
	sqlMergedInto   = "SELECT .* FROM `plg_ticket` WHERE merged_into_id = \\?"
	sqlMergedCount  = "SELECT count\\(\\*\\) FROM `plg_ticket` WHERE merged_into_id = \\?"
	sqlLinkDup      = "SELECT count\\(\\*\\) FROM `plg_ticket_link` WHERE ticket_a_id = \\? AND ticket_b_id = \\?"
	sqlLinkSide     = "SELECT count\\(\\*\\) FROM `plg_ticket_link`"
	sqlLinkScan     = "SELECT .* FROM `plg_ticket_link`"
	sqlLinkInsert   = "INSERT INTO `plg_ticket_link`"
	sqlLinkDelete   = "DELETE FROM `plg_ticket_link` WHERE ticket_a_id = \\? AND ticket_b_id = \\?"
)

// 合并成功：被合并方写指针并落终态、双方各一条动态、主单计数即时反查；回复与附件一行不动。
func TestMergeWritesPointerAndBothTrails(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m.ExpectExec(sqlTicketUpdate).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery(sqlMergedCount).WithArgs(int64(2002)).WillReturnRows(countRow(1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	res, err := s.Merge(testCtx(), "TK1001", "TK2002", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "TK1001" || res.Target != "TK2002" || res.MergedCount != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 自合并与空工单号：400 / 400，且不发出任何 SQL。
func TestMergeRejectsSelf(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	mustKey(t, mergeErr(s, "TK1001", "TK1001"), http.StatusBadRequest, "ticket.merge_self")
	mustKey(t, mergeErr(s, "", "TK2002"), http.StatusBadRequest, "error.bad_request")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Errorf("自合并/空工单号不该发出任何 SQL: %v", err)
	}
}

func mergeErr(s *Service, from, to string) error {
	_, err := s.Merge(testCtx(), from, to, "alice")
	return err
}

// 被合并方已经合并过：409 拒绝，不改写指针（历史只有一次）。
func TestMergeRejectsAlreadyMergedSource(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, int64(2002)))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m.ExpectRollback()

	mustKey(t, mergeErr(s, "TK1001", "TK2002"), http.StatusConflict, "ticket.already_merged")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 终态单当主单：已关闭、已解决都 409（合并不能把客户引到一条死胡同上）。
func TestMergeRejectsTerminalTarget(t *testing.T) {
	for _, status := range []string{StatusClosed, StatusResolved} {
		db, m := mockDB(t)
		s := NewService(db, nopLogger{})
		m.ExpectBegin()
		m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
		m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, status, nil))
		m.ExpectRollback()
		mustKey(t, mergeErr(s, "TK1001", "TK2002"), http.StatusConflict, "ticket.merge_target_terminal")
		if err := m.ExpectationsWereMet(); err != nil {
			t.Errorf("%s: %v", status, err)
		}
	}
}

// 主单自己也是旧副本（重开过的已合并单）：顺指针落到最终主单，不新建第二个头。
func TestMergeResolvesChainToFinalMain(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusProcessing, int64(2003)))
	// 链路解析的下一跳：按 id 读，不加锁（异常数据不该扩大锁面）。
	m.ExpectQuery(sqlTicketByID).WithArgs(int64(2003), 1).WillReturnRows(mergeRow("TK2003", 2003, StatusOpen, nil))
	m.ExpectExec(sqlTicketUpdate).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery(sqlMergedCount).WithArgs(int64(2003)).WillReturnRows(countRow(2))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	res, err := s.Merge(testCtx(), "TK1001", "TK2002", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "TK1001" || res.Target != "TK2003" {
		t.Fatalf("应落到最终主单 TK2003，得到 %+v", res)
	}
	if res.MergedCount != 2 {
		t.Fatalf("主单计数 = %d, want 2", res.MergedCount)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 环（回到被合并方自身）：409，绝不写自指行。
// 这条路径是真实可达的：回复会把已关闭工单重开为 processing，于是 A→B 之后 B→A 只差一次操作。
func TestMergeRejectsCycleBackToSource(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusProcessing, int64(1001)))
	m.ExpectQuery(sqlTicketByID).WithArgs(int64(1001), 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
	m.ExpectRollback()

	mustKey(t, mergeErr(s, "TK1001", "TK2002"), http.StatusConflict, "ticket.merge_cycle")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 环（链路中途成环，与被合并方无关）：走过的节点重复出现即 409。
func TestMergeRejectsCycleInsideChain(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusProcessing, int64(2003)))
	m.ExpectQuery(sqlTicketByID).WithArgs(int64(2003), 1).WillReturnRows(mergeRow("TK2003", 2003, StatusOpen, int64(2004)))
	m.ExpectQuery(sqlTicketByID).WithArgs(int64(2004), 1).WillReturnRows(mergeRow("TK2004", 2004, StatusOpen, int64(2003)))
	m.ExpectRollback()

	mustKey(t, mergeErr(s, "TK1001", "TK2002"), http.StatusConflict, "ticket.merge_cycle")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 坏数据（超跳数的长链，不闭环）：按环处理拒绝，而不是走满全表或死循环。
func TestMergeRejectsOverLongChain(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusProcessing, int64(2003)))
	// 2003 → 2004 → … 一路不闭环，走满 maxMergeHops-1 跳后由上限兜住。
	for id := int64(2003); id < int64(2003)+maxMergeHops; id++ {
		m.ExpectQuery(sqlTicketByID).WithArgs(id, 1).
			WillReturnRows(mergeRow("TKX", id, StatusOpen, id+1))
	}
	m.ExpectRollback()

	mustKey(t, mergeErr(s, "TK1001", "TK2002"), http.StatusConflict, "ticket.merge_cycle")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 并发反向合并的兜底：两张单按工单号定序加锁，与调用参数顺序无关（串行而不是死锁）。
func TestMergeLocksTicketsInStableOrder(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	// 传参是 (TK2002 → TK1001)，期望里 TK1001 必须先被锁住。
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusProcessing, nil))
	m.ExpectExec(sqlTicketUpdate).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectQuery(sqlMergedCount).WithArgs(int64(1001)).WillReturnRows(countRow(1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	res, err := s.Merge(testCtx(), "TK2002", "TK1001", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "TK2002" || res.Target != "TK1001" {
		t.Fatalf("unexpected result %+v", res)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 工单不存在（被合并方/主单各一侧）：404 并回滚。
func TestMergeTicketNotFound(t *testing.T) {
	for _, missing := range []struct{ no string }{{"TK1001"}, {"TK2002"}} {
		db, m := mockDB(t)
		s := NewService(db, nopLogger{})
		m.ExpectBegin()
		if missing.no == "TK1001" {
			m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(emptyRow())
		} else {
			m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusProcessing, nil))
			m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(emptyRow())
		}
		m.ExpectRollback()
		mustKey(t, mergeErr(s, "TK1001", "TK2002"), http.StatusNotFound, "ticket.not_found")
		if err := m.ExpectationsWereMet(); err != nil {
			t.Errorf("%s: %v", missing.no, err)
		}
	}
}

// 关联：规范化成（小 id, 大 id）一行，两侧各落一条动态。
// 这里故意让「工单号顺序」与「id 顺序」相反（TK1001 的 id 是 2002、TK2002 的 id 是 1001）：
// 加锁按工单号定序，落库必须按 id 定序，两条不变量分别被这条用例钉住。
func TestLinkStoresCanonicalPairAndBothTrails(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 2002, StatusClosed, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 1001, StatusOpen, nil))
	m.ExpectQuery(sqlLinkDup).WithArgs(int64(1001), int64(2002)).WillReturnRows(countRow(0))
	m.ExpectQuery(sqlLinkSide).WithArgs(int64(1001), int64(1001)).WillReturnRows(countRow(1))
	m.ExpectQuery(sqlLinkSide).WithArgs(int64(2002), int64(2002)).WillReturnRows(countRow(0))
	m.ExpectExec(sqlLinkInsert).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	// 传入顺序与 id 大小相反：存下来仍必须是 a=1001、b=2002。
	link, err := s.Link(testCtx(), "TK2002", "TK1001", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if link.TicketAID != idgen.ID(1001) || link.TicketBID != idgen.ID(2002) {
		t.Fatalf("canonical pair = %d/%d, want 1001/2002", link.TicketAID, link.TicketBID)
	}
	if link.LinkBy != "alice" {
		t.Fatalf("linkBy = %q", link.LinkBy)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 关联守卫：自关联 400 且不碰库、重复 409、单侧超上限 400、对侧不存在 404。
func TestLinkGuards(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	_, err := s.Link(testCtx(), "TK1001", "TK1001", "alice")
	mustKey(t, err, http.StatusBadRequest, "ticket.link_self")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Errorf("自关联不该发出任何 SQL: %v", err)
	}

	db2, m2 := mockDB(t)
	s2 := NewService(db2, nopLogger{})
	m2.ExpectBegin()
	m2.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m2.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m2.ExpectQuery(sqlLinkDup).WillReturnRows(countRow(1))
	m2.ExpectRollback()
	_, err = s2.Link(testCtx(), "TK1001", "TK2002", "alice")
	mustKey(t, err, http.StatusConflict, "ticket.link_exists")

	db3, m3 := mockDB(t)
	s3 := NewService(db3, nopLogger{})
	m3.ExpectBegin()
	m3.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m3.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m3.ExpectQuery(sqlLinkDup).WillReturnRows(countRow(0))
	m3.ExpectQuery(sqlLinkSide).WillReturnRows(countRow(maxLinksPerTicket))
	m3.ExpectRollback()
	_, err = s3.Link(testCtx(), "TK1001", "TK2002", "alice")
	mustKey(t, err, http.StatusBadRequest, "ticket.link_limit")

	db4, m4 := mockDB(t)
	s4 := NewService(db4, nopLogger{})
	m4.ExpectBegin()
	m4.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m4.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(emptyRow())
	m4.ExpectRollback()
	_, err = s4.Link(testCtx(), "TK1001", "TK2002", "alice")
	mustKey(t, err, http.StatusNotFound, "ticket.not_found")
}

// 解除：删掉规范化那一行，两侧各一条动态；行不存在时 404。
func TestUnlinkDeletesCanonicalRow(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	// 传参顺序与 id 大小相反：读的还是各自那一行，DELETE 必须落在规范化的 (1001, 2002) 上。
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m.ExpectExec(sqlLinkDelete).WithArgs(int64(1001), int64(2002)).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec(sqlActivity).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()
	if err := s.Unlink(testCtx(), "TK2002", "TK1001", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}

	db2, m2 := mockDB(t)
	s2 := NewService(db2, nopLogger{})
	m2.ExpectBegin()
	m2.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m2.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m2.ExpectExec(sqlLinkDelete).WithArgs(int64(1001), int64(2002)).WillReturnResult(sqlmock.NewResult(0, 0))
	m2.ExpectRollback()
	mustKey(t, s2.Unlink(testCtx(), "TK1001", "TK2002", "alice"), http.StatusNotFound, "ticket.link_not_found")

	mustKey(t, s2.Unlink(testCtx(), "TK1001", "TK1001", "alice"), http.StatusBadRequest, "ticket.link_self")
	mustKey(t, s2.Unlink(testCtx(), "", "TK2002", "alice"), http.StatusBadRequest, "error.bad_request")
}

// 读接口带出关联列表：两个方向都要能看见对面那张。
func TestListLinksReadsBothDirections(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK2002", 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	// 2002 既出现在 a 列也出现在 b 列：两条都要归一到对面。
	m.ExpectQuery(sqlLinkScan).WithArgs(int64(2002), int64(2002), int64(linkListLimit)).
		WillReturnRows(linkRows([][2]int64{{1001, 2002}, {2002, 3003}}))
	m.ExpectQuery(sqlTicketIn).WillReturnRows(
		mergeRow("TK1001", 1001, StatusClosed, nil).
			AddRow(3003, "TK3003", "trade_group", "G-1", "other", StatusOpen, PriorityLow, nil, nil, nil, nil))

	list, err := s.ListLinks(testCtx(), "TK2002")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("links = %+v", list)
	}
	if list[0].TicketNo != "TK1001" || list[0].Kind != KindLink || !list[0].LinkedAt.Equal(testNow) {
		t.Fatalf("first link = %+v", list[0])
	}
	if list[1].TicketNo != "TK3003" {
		t.Fatalf("second link = %+v", list[1])
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 对侧被删（悬挂引用）：跳过那一条而不是让整个列表读失败；没有关联时不必回查工单表。
func TestListLinksSkipsDanglingAndEmpty(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m.ExpectQuery(sqlLinkScan).WithArgs(int64(1001), int64(1001), int64(linkListLimit)).
		WillReturnRows(linkRows([][2]int64{{1001, 9999}}))
	m.ExpectQuery(sqlTicketIn).WillReturnRows(emptyRow(
		"id", "ticket_no", "biz_type", "biz_no", "title", "status", "priority",
		"merged_into_id", "closed_at", "resolution_due_at", "sla_paused_at"))

	list, err := s.ListLinks(testCtx(), "TK1001")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty, got %+v", list)
	}

	db2, m2 := mockDB(t)
	s2 := NewService(db2, nopLogger{})
	m2.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	m2.ExpectQuery(sqlLinkScan).WithArgs(int64(1001), int64(1001), int64(linkListLimit)).WillReturnRows(linkRows(nil))
	list, err = s2.ListLinks(testCtx(), "TK1001")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty, got %+v", list)
	}
	// 这里必须没有第三条查询：没有关联行就不去回查工单表。
	if err := m2.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}

	db3, m3 := mockDB(t)
	s3 := NewService(db3, nopLogger{})
	m3.ExpectQuery(sqlTicketByNo).WithArgs("TK9999", 1).WillReturnRows(emptyRow())
	_, err = s3.ListLinks(testCtx(), "TK9999")
	mustKey(t, err, http.StatusNotFound, "ticket.not_found")
}

// 合并痕迹：本单被并进哪张 + 哪些单并到了本单。
func TestMergeSummaryShowsBothSides(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusClosed, int64(2002)))
	m.ExpectQuery(sqlTicketByID).WithArgs(int64(2002), 1).WillReturnRows(mergeRow("TK2002", 2002, StatusOpen, nil))
	m.ExpectQuery(sqlMergedInto).WithArgs(int64(1001), int64(linkListLimit)).WillReturnRows(
		mergeRow("TK3003", 3003, StatusClosed, int64(1001)))

	sum, err := s.MergeSummary(testCtx(), "TK1001")
	if err != nil {
		t.Fatal(err)
	}
	if sum.MergedInto == nil || sum.MergedInto.TicketNo != "TK2002" || sum.MergedInto.Kind != KindMergedInto {
		t.Fatalf("mergedInto = %+v", sum.MergedInto)
	}
	if sum.MergedCount != 1 || len(sum.MergedFrom) != 1 || sum.MergedFrom[0].TicketNo != "TK3003" {
		t.Fatalf("mergedFrom = %+v count=%d", sum.MergedFrom, sum.MergedCount)
	}
	if sum.MergedFrom[0].Kind != KindMergedFrom {
		t.Fatalf("kind = %q", sum.MergedFrom[0].Kind)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 被合并方指向的主单已被删除：合并痕迹留空，不影响其余读取。
func TestMergeSummaryToleratesDanglingPointer(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusClosed, int64(2002)))
	m.ExpectQuery(sqlTicketByID).WithArgs(int64(2002), 1).WillReturnRows(emptyRow())
	m.ExpectQuery(sqlMergedInto).WithArgs(int64(1001), int64(linkListLimit)).WillReturnRows(emptyRow(
		"id", "ticket_no", "biz_type", "biz_no", "title", "status", "priority",
		"merged_into_id", "closed_at", "resolution_due_at", "sla_paused_at"))

	sum, err := s.MergeSummary(testCtx(), "TK1001")
	if err != nil {
		t.Fatal(err)
	}
	if sum.MergedInto != nil || sum.MergedCount != 0 {
		t.Fatalf("悬挂指针应留空，得到 %+v", sum)
	}
}

// 动作码是稳定契约（前端字典与开放事件都以它为准），值改动必须让这里变红。
func TestMergeActionCodesAreStable(t *testing.T) {
	cases := [][2]string{
		{ActMergedInto, "merged_into"},
		{ActMergedTickets, "merged_tickets"},
		{ActLinked, "linked"},
		{ActUnlinked, "unlinked"},
	}
	for _, c := range cases {
		if c[0] != c[1] {
			t.Errorf("action code = %q, want %q", c[0], c[1])
		}
	}
	// 客户自助页只露提交与状态变化：合并/关联这类内部协作细节不外泄。
	for _, a := range publicActivityActions() {
		switch a {
		case ActMergedInto, ActMergedTickets, ActLinked, ActUnlinked:
			t.Errorf("公开动态里不该出现 %q", a)
		}
	}
}

// linkPairIDs 的规范化：入参顺序不影响落库形状。
func TestLinkPairIDsCanonical(t *testing.T) {
	lo, hi := linkPairIDs(idgen.ID(9), idgen.ID(3))
	if lo != idgen.ID(3) || hi != idgen.ID(9) {
		t.Fatalf("pair = %d/%d", lo, hi)
	}
	if lo, hi = linkPairIDs(idgen.ID(3), idgen.ID(9)); lo != idgen.ID(3) || hi != idgen.ID(9) {
		t.Fatalf("pair = %d/%d", lo, hi)
	}
}

// takeMergeTicket 把 gorm 的漏读收敛成本包的 404；merged_into_id 为 NULL 时读成「没被合并过」。
func TestTakeMergeTicketProjection(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery(sqlTicketByNo).WithArgs("TK404", 1).WillReturnRows(emptyRow())
	_, err := takeMergeTicket(db.Where("ticket_no = ?", "TK404"))
	mustKey(t, err, http.StatusNotFound, "ticket.not_found")

	m.ExpectQuery(sqlTicketByNo).WithArgs("TK1001", 1).WillReturnRows(mergeRow("TK1001", 1001, StatusOpen, nil))
	row, err := takeMergeTicket(db.Where("ticket_no = ?", "TK1001"))
	if err != nil {
		t.Fatal(err)
	}
	if row.mergedIntoID() != 0 {
		t.Fatalf("NULL 指针应读成 0，得到 %d", row.mergedIntoID())
	}
	if ticketRef(row).ID != idgen.ID(1001) || ticketRef(row).TicketNo != "TK1001" {
		t.Fatalf("ticketRef = %+v", ticketRef(row))
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TicketLink.other 取对面：两个方向都要归一到「别人」。
func TestLinkOtherSide(t *testing.T) {
	l := TicketLink{TicketAID: 1001, TicketBID: 2002}
	if l.other(idgen.ID(1001)) != idgen.ID(2002) || l.other(idgen.ID(2002)) != idgen.ID(1001) {
		t.Fatalf("other = %d / %d", l.other(idgen.ID(1001)), l.other(idgen.ID(2002)))
	}
}

// ---- 真实 MySQL ----

// TestITMySQLTicketMerge 在一次性库里验证合并/关联在真实引擎上的行为：
// 迁移 011 建得出列与表、终态与指针真的落库、回复与附件一行不搬、动态两边都留痕、
// 链解析/环检测/重开旧副本这些边界在真 SQL 上成立，以及反向并发合并不死锁。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行。库名 nexadmin_ticketmerge_<纳秒>，收尾删库。
func TestITMySQLTicketMerge(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL merge test")
	}
	if !strings.Contains(dsn, "multiStatements=true") {
		if strings.Contains(dsn, "?") {
			dsn += "&multiStatements=true"
		} else {
			dsn += "?multiStatements=true"
		}
	}
	if !strings.Contains(dsn, "parseTime=true") {
		dsn += "&parseTime=true&loc=UTC"
	}

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open admin conn: %v", err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("mysql not reachable: %v", err)
	}
	dbName := fmt.Sprintf("nexadmin_ticketmerge_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatalf("create throwaway db: %v", err)
	}
	var gdb *gorm.DB
	// 收尾顺序：关业务连接池 → 删库 → 关管理连接（顺序反了 DROP 会撞在已关闭的句柄上）。
	t.Cleanup(func() {
		if gdb != nil {
			if sqlDB, err := gdb.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
			t.Errorf("drop throwaway db %s: %v", dbName, err)
		}
		_ = admin.Close()
	})

	itRunMigrationsOn(t, dsn, dbName)
	cfg, err := itMySQLConfig(dsn, dbName)
	if err != nil {
		t.Fatalf("%v", err)
	}
	gdb, err = database.Open(cfg)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	// 句柄默认绑租户 1：夹具里几十处 gdb.X 逐个 WithContext 不如在这里一次性定住，
	// 需要别的租户的用例（scope_it_test）再显式覆盖。
	gdb = gdb.WithContext(itCtx())

	// 真库用例：ctx 要和 gdb 句柄同属租户 1，用 testCtx() 建出来的行是 tenant_id=0，
	// 句柄按租户 1 查就成了 ticket.not_found。
	ctx := WithActor(itCtx(), ActivityActor{Type: ActorAgent, ID: "11", Name: "alice"})
	svc := NewService(gdb, nopLogger{}).WithTimezone("UTC")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	keyIs := func(err error, status int, key string) {
		t.Helper()
		mustKey(t, err, status, key)
	}
	makeTicket := func(title string) *Ticket {
		row, err := svc.Create(ctx, CreateInput{Title: title, Content: "desc of " + title, ReporterType: ReplierReporter, ReporterName: "visitor"})
		must(err)
		return row
	}
	pointer := func(no string) idgen.ID {
		row, err := takeMergeTicket(gdb.Where("ticket_no = ?", no))
		must(err)
		return row.mergedIntoID()
	}
	statusOf := func(no string) string {
		row, err := takeMergeTicket(gdb.Where("ticket_no = ?", no))
		must(err)
		return row.Status
	}
	repliesOf := func(no string) int {
		_, rows, err := svc.Replies(ctx, no, 0, 200, true)
		must(err)
		return len(rows)
	}
	actsOf := func(no string) []TicketActivity {
		row, err := takeMergeTicket(gdb.Where("ticket_no = ?", no))
		must(err)
		acts, err := svc.TicketActivities(ctx, row.ID, false)
		must(err)
		return acts
	}
	lastAct := func(no, action string) TicketActivity {
		t.Helper()
		found := TicketActivity{}
		acts := actsOf(no)
		for _, a := range acts {
			if a.Action == action {
				found = a
			}
		}
		if found.Action != action {
			t.Fatalf("%s 上没有 %s 动态：%+v", no, action, acts)
		}
		return found
	}

	// 关联：双向可见、重复拒绝、解除后可再建。
	la, lb := makeTicket("link a"), makeTicket("link b")
	link, err := svc.Link(ctx, la.TicketNo, lb.TicketNo, "alice")
	must(err)
	if link.TicketAID == 0 || link.TicketBID == 0 || link.TicketAID > link.TicketBID {
		t.Fatalf("canonical pair 落库异常: %+v", link)
	}
	for _, pair := range [][2]string{{la.TicketNo, lb.TicketNo}, {lb.TicketNo, la.TicketNo}} {
		list, err := svc.ListLinks(ctx, pair[0])
		must(err)
		if len(list) != 1 || list[0].TicketNo != pair[1] || list[0].Kind != KindLink {
			t.Fatalf("%s 的关联列表 = %+v", pair[0], list)
		}
	}
	_, err = svc.Link(ctx, la.TicketNo, lb.TicketNo, "alice")
	keyIs(err, http.StatusConflict, "ticket.link_exists")
	_, err = svc.Link(ctx, la.TicketNo, la.TicketNo, "alice")
	keyIs(err, http.StatusBadRequest, "ticket.link_self")
	must(svc.Unlink(ctx, lb.TicketNo, la.TicketNo, "alice"))
	for _, no := range []string{la.TicketNo, lb.TicketNo} {
		list, err := svc.ListLinks(ctx, no)
		must(err)
		if len(list) != 0 {
			t.Fatalf("解除后 %s 仍有关联: %+v", no, list)
		}
	}
	mustKey(t, svc.Unlink(ctx, la.TicketNo, lb.TicketNo, "alice"), http.StatusNotFound, "ticket.link_not_found")
	// 解除也要留痕：两侧各一条 unlinked。
	lastAct(la.TicketNo, ActUnlinked)
	lastAct(lb.TicketNo, ActUnlinked)
	// 终态单允许被关联：合并与关闭恰恰最需要「这几张是同一件事」。
	if _, err := svc.Link(ctx, la.TicketNo, lb.TicketNo, "alice"); err != nil {
		t.Fatal(err)
	}

	// 合并：被合并方进终态并记指针，回复/附件原地不动，双方各落一条动态。
	dup := makeTicket("duplicate")
	main := makeTicket("main thread")
	if _, _, err := svc.Reply(ctx, dup.TicketNo, ReplyInput{
		ReplierType: ReplierAgent, ReplierID: "11", ReplierName: "alice", Content: "already answered here",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAttachment(ctx, dup.TicketNo, ReplierReporter, "visitor", false, "proof.png", "image/png", 1234, "202611/proof.png"); err != nil {
		t.Fatal(err)
	}
	dupReplies, dupAtts := repliesOf(dup.TicketNo), 0
	if as, err := svc.ListAttachments(ctx, dup.TicketNo, true); err != nil {
		t.Fatal(err)
	} else {
		dupAtts = len(as)
	}
	mainReplies := repliesOf(main.TicketNo)

	res, err := svc.Merge(ctx, dup.TicketNo, main.TicketNo, "alice")
	must(err)
	if res.Source != dup.TicketNo || res.Target != main.TicketNo || res.MergedCount != 1 {
		t.Fatalf("合并结果 = %+v", res)
	}
	if statusOf(dup.TicketNo) != StatusClosed {
		t.Fatalf("被合并方未进终态: %s", statusOf(dup.TicketNo))
	}
	if pointer(dup.TicketNo) != main.ID {
		t.Fatalf("指针没落库: %d", pointer(dup.TicketNo))
	}
	if pointer(main.TicketNo) != 0 {
		t.Fatal("主单不该被写成被合并方")
	}
	// 回复与附件留在原地：条数不变，主单也没被搬进东西。
	if got := repliesOf(dup.TicketNo); got != dupReplies {
		t.Fatalf("被合并方回复条数变了: %d → %d", dupReplies, got)
	}
	if as, err := svc.ListAttachments(ctx, dup.TicketNo, true); err != nil {
		t.Fatal(err)
	} else if len(as) != dupAtts {
		t.Fatalf("被合并方附件条数变了: %d → %d", dupAtts, len(as))
	}
	if got := repliesOf(main.TicketNo); got != mainReplies {
		t.Fatalf("主单回复被搬进来了: %d → %d", mainReplies, got)
	}
	// 动态轨迹：被合并方指到主单，主单记下「合并了 N 张」。
	into := lastAct(dup.TicketNo, ActMergedInto)
	if into.ToValue != StatusClosed || into.Detail != main.TicketNo || into.ActorName != "alice" {
		t.Fatalf("被合并方动态 = %+v", into)
	}
	counted := lastAct(main.TicketNo, ActMergedTickets)
	if counted.ToValue != "1" || counted.Detail != dup.TicketNo {
		t.Fatalf("主单动态 = %+v", counted)
	}
	// 客户自助页不外泄内部协作细节。
	publicActs, err := svc.TicketActivities(ctx, dup.ID, true)
	must(err)
	for _, a := range publicActs {
		switch a.Action {
		case ActMergedInto, ActMergedTickets, ActLinked, ActUnlinked:
			t.Errorf("公开动态里出现了 %q", a.Action)
		}
	}

	// 已合并的单再合并：拒绝，指针不被改写。
	if _, err := svc.Merge(ctx, dup.TicketNo, main.TicketNo, "alice"); err == nil {
		t.Fatal("被合并过的单又被合并了")
	} else {
		keyIs(err, http.StatusConflict, "ticket.already_merged")
	}
	if pointer(dup.TicketNo) != main.ID {
		t.Fatal("指针被改写")
	}

	// 终态单当主单：已关闭、已解决都拒绝。
	dead := makeTicket("closed one")
	if _, err := svc.SetStatus(ctx, dead.TicketNo, StatusClosed, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Merge(ctx, main.TicketNo, dead.TicketNo, "alice")
	keyIs(err, http.StatusConflict, "ticket.merge_target_terminal")
	resolved := makeTicket("resolved one")
	if _, err := svc.SetStatus(ctx, resolved.TicketNo, StatusResolved, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Merge(ctx, main.TicketNo, resolved.TicketNo, "alice")
	keyIs(err, http.StatusConflict, "ticket.merge_target_terminal")
	if statusOf(main.TicketNo) == StatusClosed {
		t.Fatal("被拒绝的合并把被合并方关掉了")
	}

	// 主单自己是旧副本（回复把已合并的单重开，但指针还在）：合并顺指针落到最终主单。
	if _, _, err := svc.Reply(ctx, dup.TicketNo, ReplyInput{
		ReplierType: ReplierReporter, ReplierID: "tok-1", ReplierName: "visitor", Content: "still needs work",
	}); err != nil {
		t.Fatal(err)
	}
	if statusOf(dup.TicketNo) == StatusClosed {
		t.Fatal("用例前提不成立：回复应把已关闭的旧副本重开")
	}
	late := makeTicket("late duplicate")
	res, err = svc.Merge(ctx, late.TicketNo, dup.TicketNo, "alice")
	must(err)
	if res.Target != main.TicketNo {
		t.Fatalf("应落到最终主单 %s，得到 %+v", main.TicketNo, res)
	}
	if pointer(late.TicketNo) != main.ID {
		t.Fatalf("新副本指到了中间节点: %d", pointer(late.TicketNo))
	}
	if lastAct(main.TicketNo, ActMergedTickets).ToValue != "2" {
		t.Fatalf("主单计数没跟着第二次合并走: %+v", lastAct(main.TicketNo, ActMergedTickets))
	}
	summary, err := svc.MergeSummary(ctx, dup.TicketNo)
	must(err)
	if summary.MergedInto == nil || summary.MergedInto.TicketNo != main.TicketNo {
		t.Fatalf("旧副本的合并痕迹 = %+v", summary.MergedInto)
	}
	summary, err = svc.MergeSummary(ctx, main.TicketNo)
	must(err)
	if summary.MergedInto != nil || summary.MergedCount != 2 || len(summary.MergedFrom) != 2 {
		t.Fatalf("主单的合并痕迹 = %+v", summary)
	}
	for _, item := range summary.MergedFrom {
		if item.Kind != KindMergedFrom {
			t.Fatalf("kind = %q", item.Kind)
		}
	}

	// 环：主单并进那个刚被重开的旧副本 → 链解析落回主单自己，必须拒绝。
	_, err = svc.Merge(ctx, main.TicketNo, dup.TicketNo, "alice")
	keyIs(err, http.StatusConflict, "ticket.merge_cycle")
	if pointer(main.TicketNo) != 0 || statusOf(main.TicketNo) == StatusClosed {
		t.Fatalf("被拒绝的成环合并改写了主单: pointer=%d status=%s", pointer(main.TicketNo), statusOf(main.TicketNo))
	}

	// 反向并发合并：定序加锁让两个事务串行，只成一个，另一个按规则被拒，且不留下双指针。
	x := makeTicket("concurrent x")
	y := makeTicket("concurrent y")
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, pair := range [][2]string{{x.TicketNo, y.TicketNo}, {y.TicketNo, x.TicketNo}} {
		wg.Add(1)
		go func(idx int, from, to string) {
			defer wg.Done()
			_, err := svc.Merge(ctx, from, to, "alice")
			results[idx] = err
		}(i, pair[0], pair[1])
	}
	wg.Wait()
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
			continue
		}
		var e *response.Error
		if !errors.As(err, &e) || e.Status < 400 || e.Status >= 500 {
			t.Fatalf("并发合并的失败必须是业务拒绝，得到 %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("反向并发合并该只成一个，得到 %d 个（err=%v）", wins, results)
	}
	if pointer(x.TicketNo) != 0 && pointer(y.TicketNo) != 0 {
		t.Fatalf("两张单互相指过去了: %d / %d", pointer(x.TicketNo), pointer(y.TicketNo))
	}
	// 指针非空的那一侧才是被合并方（落败的合并），另一侧是活着的主单。
	var loser, winner *Ticket
	if pointer(x.TicketNo) == 0 {
		loser, winner = y, x
	} else {
		loser, winner = x, y
	}
	if pointer(loser.TicketNo) != winner.ID || statusOf(loser.TicketNo) != StatusClosed {
		t.Fatalf("落败一侧的指针/终态不对: %d %s", pointer(loser.TicketNo), statusOf(loser.TicketNo))
	}
	// 主单的计数是事务内即时反查出来的，串行后两次合并只算一次。
	if lastAct(winner.TicketNo, ActMergedTickets).ToValue != "1" {
		t.Fatalf("并发后的主单计数 = %+v", lastAct(winner.TicketNo, ActMergedTickets))
	}

	// 空入参与不存在的单：工单不存在 404，且不留下半成品。
	_, err = svc.Merge(ctx, "TK-none", main.TicketNo, "alice")
	keyIs(err, http.StatusNotFound, "ticket.not_found")
	if _, err := svc.MergeSummary(ctx, "TK-none"); err == nil {
		t.Fatal("不存在的单读合并痕迹不该成功")
	} else {
		keyIs(err, http.StatusNotFound, "ticket.not_found")
	}

	// 列表投影必须带上「已合并到哪张」：坐席在列表里要能看出这单已被并走，并能跳回主单。
	var mergedNo, targetNo string
	must(gdb.Raw("SELECT t.ticket_no, h.ticket_no FROM plg_ticket t JOIN plg_ticket h ON h.id = t.merged_into_id "+
		"WHERE t.merged_into_id IS NOT NULL LIMIT 1").Row().Scan(&mergedNo, &targetNo))
	page, _, err := svc.ListTickets(ctx, TicketQuery{Keyword: mergedNo, Page: 1, PageSize: 20})
	must(err)
	got := ""
	for i := range page {
		if page[i].TicketNo == mergedNo {
			got = page[i].MergedIntoNo
		}
	}
	if got != targetNo {
		t.Errorf("列表里的 mergedIntoNo = %q, want %q", got, targetNo)
	}
}
