package ticket

// 删除工单的事务级联闸：回复/活动/关联/附件随单清掉，被合并指针清空，行删完再删存储对象。

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// 全流程：五张表按序清行，merged_into 指针清空，最后删工单本体。
func TestDeleteTicketCascades(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no =").WillReturnRows(ticketRow("TK1", "closed", 3))
	m.ExpectExec("DELETE FROM `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(0, 3))
	m.ExpectExec("DELETE FROM `plg_ticket_activity`").WillReturnResult(sqlmock.NewResult(0, 5))
	m.ExpectExec("DELETE FROM `plg_ticket_link`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_attachment` WHERE ticket_id =").
		WillReturnRows(sqlmock.NewRows([]string{"id", "storage_key"}).AddRow(9, "att/9/x.png"))
	m.ExpectExec("DELETE FROM `plg_ticket_attachment`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE `plg_ticket`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM `plg_ticket`").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()

	if err := s.DeleteTicket(testCtx(), "TK1"); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 不存在的单号 404，不进事务。
func TestDeleteTicketNotFound(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	// 空结果集 = gorm.ErrRecordNotFound → 404；事务开了但查不到就回滚，不触发任何删除。
	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no =").WillReturnRows(emptyRow())
	m.ExpectRollback()

	err := s.DeleteTicket(testCtx(), "TK404")
	mustKey(t, err, 404, "ticket.not_found")
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 事务失败整体回滚：任何一张表删除失败都不落任何一行。
func TestDeleteTicketRollsBackOnError(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})

	m.ExpectBegin()
	m.ExpectQuery("SELECT \\* FROM `plg_ticket` WHERE ticket_no =").WillReturnRows(ticketRow("TK1", "open", 0))
	m.ExpectExec("DELETE FROM `plg_ticket_reply`").WillReturnError(errors.New("boom"))
	m.ExpectRollback()

	if err := s.DeleteTicket(testCtx(), "TK1"); err == nil {
		t.Fatal("want error on delete failure")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
