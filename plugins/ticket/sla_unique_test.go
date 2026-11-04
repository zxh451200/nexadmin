package ticket

// SLA 策略编码唯一性的封口用例（迁移 018 + 驱动重复键折 4xx）。
// 本文件只有 sqlmock：钉住「认索引冲突的文本判据」与「Create 撞唯一索引后回什么错误」，
// 真库上的清洗与并发行为在 sla_unique_it_test.go（sqlmock 从不执行 SQL，那条必须分开跑）。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"

	"nexadmin/core/migrate"
	"nexadmin/core/response"
)

// 两引擎驱动的真实重复键文本（取自 MySQL 8.0 / MariaDB 13 / PG 16 的实际返回）。
const (
	mysqlDupKeyText = "Error 1062 (23000): Duplicate entry 'vip-desk' for key 'plg_ticket_sla.uk_plg_ticket_sla_code'"
	mariaDupKeyText = "Error 1062 (23000): Duplicate entry '' for key 'plg_ticket_sla.uk_plg_ticket_sla_code'"
	pgDupKeyText    = `duplicate key value violates unique constraint "uk_plg_ticket_sla_code" (SQLSTATE 23505)`
)

// 反例：不是唯一索引拦的错误，一条都不许被认成撞码（否则真故障会被折成 409 业务错误骗过去）。
var notDupKeyTexts = []string{
	"Error 1054 (42S22): Unknown column 'nope' in 'field list'",
	"Error 1146 (42S02): Table 'nexadmin.plg_ticket_sla' doesn't exist",
	"Error 1213 (40001): Deadlock found when trying to get lock; try restarting transaction",
	"dial tcp 127.0.0.1:3306: connect: connection refused",
	"context deadline exceeded",
	"SQLSTATE 22P02: invalid input syntax for type integer",
}

func TestIsSLADuplicateKeyErr(t *testing.T) {
	positives := map[string]string{
		"mysql":    mysqlDupKeyText,
		"maria":    mariaDupKeyText,
		"postgres": pgDupKeyText,
		"wrapped":  fmt.Sprintf("创建行失败: %v", errors.New(pgDupKeyText)),
	}
	for name, text := range positives {
		if !isSLADuplicateKeyErr(errors.New(text)) {
			t.Errorf("%s 的重复键文本没被认出来：%s", name, text)
		}
	}
	for _, text := range notDupKeyTexts {
		if isSLADuplicateKeyErr(errors.New(text)) {
			t.Errorf("非唯一键冲突被误判成撞码：%s", text)
		}
	}
	if isSLADuplicateKeyErr(nil) {
		t.Error("nil 不该被判成撞码")
	}
	if isSLADuplicateKeyErr(gorm.ErrRecordNotFound) {
		t.Error("ErrRecordNotFound 不该被判成撞码")
	}
}

// SaveSLAPolicy 的 insert 分支撞上唯一索引 → 409 ticket.sla_code_conflict，且驱动原文留在 Wrap 里。
// 变异反证：删掉 sla.go 里的 isSLADuplicateKeyErr 分支，这里拿到的是裸驱动错误、断言立刻红。
func TestSLASavePolicyDuplicateKeyMappedToConflict(t *testing.T) {
	for _, text := range []string{mysqlDupKeyText, mariaDupKeyText, pgDupKeyText} {
		db, m := mockDB(t)
		s := NewService(db, nopLogger{})
		m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
			WillReturnRows(sqlmock.NewRows(slaColumns)) // 预检未命中 → 走建行
		m.ExpectBegin()
		m.ExpectExec("INSERT INTO `plg_ticket_sla`").WillReturnError(errors.New(text))
		m.ExpectRollback()

		_, err := s.SaveSLAPolicy(testCtx(), SLASetting{
			Code: "vip-desk", Label: "VIP", Workdays: []int{1, 2, 3, 4, 5},
			StartMinute: 540, EndMinute: 1080, Targets: DefaultSLA().Targets,
		})
		var e *response.Error
		if !errors.As(err, &e) {
			t.Fatalf("撞唯一索引应折成业务错误（驱动文本：%s），实得 %#v", text, err)
		}
		if e.Status != 409 || e.Key != "ticket.sla_code_conflict" {
			t.Fatalf("撞唯一索引应 409 ticket.sla_code_conflict，实得 %d %q", e.Status, e.Key)
		}
		// 索引名是「谁拦的」唯一取证：驱动原文必须挂在 Err 上进日志，不能只丢一句文案。
		if e.Err == nil || !strings.Contains(e.Err.Error(), text) {
			t.Fatalf("驱动原文没随 Wrap 留住：%#v", e.Err)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	}
}

// SaveSLA（默认策略，code 空串）的建行分支同样受 uk_plg_ticket_sla_code 保护，口径要一致。
func TestSLASaveDefaultPolicyDuplicateKeyMapped(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` ORDER BY id ASC").
		WillReturnRows(sqlmock.NewRows(slaColumns))
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket_sla`").WillReturnError(errors.New(mysqlDupKeyText))
	m.ExpectRollback()

	if _, err := s.SaveSLA(testCtx(), SLASetting{
		Workdays: []int{1, 2, 3, 4, 5}, StartMinute: 540, EndMinute: 1080, Targets: DefaultSLA().Targets,
	}); err == nil {
		t.Fatal("默认策略撞唯一索引应报错")
	} else {
		var e *response.Error
		if !errors.As(err, &e) || e.Status != 409 || e.Key != "ticket.sla_code_conflict" {
			t.Fatalf("默认策略撞唯一索引应 409 ticket.sla_code_conflict，实得 %#v", err)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 非唯一键的建行失败必须原样抛出（500 就是 500），不许被折成 409 业务错误。
func TestSLASavePolicyOtherInsertErrorNotMapped(t *testing.T) {
	for _, text := range notDupKeyTexts {
		db, m := mockDB(t)
		s := NewService(db, nopLogger{})
		m.ExpectQuery("SELECT \\* FROM `plg_ticket_sla` WHERE code = \\? ORDER BY id ASC").
			WillReturnRows(sqlmock.NewRows(slaColumns))
		m.ExpectBegin()
		m.ExpectExec("INSERT INTO `plg_ticket_sla`").WillReturnError(errors.New(text))
		m.ExpectRollback()

		_, err := s.SaveSLAPolicy(testCtx(), SLASetting{
			Code: "vip-desk", Workdays: []int{1, 2, 3, 4, 5},
			StartMinute: 540, EndMinute: 1080, Targets: DefaultSLA().Targets,
		})
		if err == nil {
			t.Fatalf("建行失败被吞掉了（%s）", text)
		}
		var e *response.Error
		if errors.As(err, &e) {
			t.Fatalf("非撞码错误被折成业务错误（%s）：%#v", text, e)
		}
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("原始错误丢了（%s）：%v", text, err)
		}
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	}
}

// 018 的形状闸：三份迁移都得是「先删重复、再建唯一索引」这两条语句，顺序不能反
// （反了唯一索引直接建不起来），索引名与列也要一致。
// 按 migrate.Split 的语句粒度比对——和 runner 看到的完全一样，注释里提到关键字不会被误判。
// 结构闸（plugins/migrations_test.go）只在三个目录的聚合文本之间比对索引签名，
// 漏一个引擎目录这种「三份都没有」的情形它照不出来，这里按文件各钉一次。
func TestSLAUniqueMigrationFilesShape(t *testing.T) {
	reDel := regexp.MustCompile(`(?i)^(DELETE\s+dup\s+FROM\s+plg_ticket_sla|DELETE\s+FROM\s+plg_ticket_sla\s+dup)`)
	reIdx := regexp.MustCompile(`(?i)^CREATE\s+UNIQUE\s+INDEX\s+uk_plg_ticket_sla_code\s+ON\s+plg_ticket_sla\s*\(\s*code\s*\)$`)
	for _, engine := range []string{"mysql", "mariadb", "postgres"} {
		stmts := migrate.Split(readMigrationFile(t, engine, "018_ticket_sla_code_unique.sql"))
		if len(stmts) != 2 {
			t.Fatalf("%s 的 018 应恰好两条语句（清洗 + 建索引），实得 %d 条：%q", engine, len(stmts), stmts)
		}
		if !reDel.MatchString(stmts[0]) {
			t.Errorf("%s 的 018 第 1 条不是「同 code 保留最小 id」的清洗 DELETE：%s", engine, stmts[0])
		}
		if !strings.Contains(stmts[0], "keep.id < dup.id") {
			t.Errorf("%s 的 018 清洗语句没有钉住「保留 id 最小的一行」：%s", engine, stmts[0])
		}
		if !reIdx.MatchString(stmts[1]) {
			t.Errorf("%s 的 018 第 2 条不是 CREATE UNIQUE INDEX uk_plg_ticket_sla_code ON plg_ticket_sla (code)：%s", engine, stmts[1])
		}
		// runner 侧口径：清洗的 DELETE 只该记一条 WARN、不阻断升级（core/migrate 的 DML 规则）；
		// 哪天规则改成阻断，这条先红，别到存量库升级时才炸。
		warns := migrate.DMLWarnings(migrate.File{Scope: migrate.ScopePrefix + "ticket", Name: "018_ticket_sla_code_unique.sql"}, stmts)
		if len(warns) != 1 || !strings.Contains(warns[0], "DELETE") {
			t.Errorf("%s 的 018 在 runner 里的 DML 警告应是 1 条 DELETE，实得 %q", engine, warns)
		}
	}
}

// readMigrationFile 读插件迁移目录里的一份 SQL（引擎目录必须显式给：MariaDB 有自己的目录，
// 不回落到 mysql）。
func readMigrationFile(t *testing.T, engine, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("migrations", engine, name))
	if err != nil {
		t.Fatalf("读 %s/%s: %v", engine, name, err)
	}
	return string(b)
}
