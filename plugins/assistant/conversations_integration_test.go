package assistant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/config"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This harness never loads application configuration. Both the destructive-test
// acknowledgement and a dedicated disposable DSN must be explicitly supplied.
func TestConversationPersistenceDisposableDB(t *testing.T) {
	dsn := os.Getenv("ASSISTANT_TEST_DISPOSABLE_DSN")
	if dsn == "" {
		t.Skip("set ASSISTANT_TEST_DISPOSABLE_DSN, ASSISTANT_TEST_DB_DRIVER and ASSISTANT_TEST_ALLOW_DISPOSABLE=1")
	}
	if os.Getenv("ASSISTANT_TEST_ALLOW_DISPOSABLE") != "1" {
		t.Fatal("refusing database access without ASSISTANT_TEST_ALLOW_DISPOSABLE=1")
	}
	var dialect gorm.Dialector
	switch os.Getenv("ASSISTANT_TEST_DB_DRIVER") {
	case "postgres":
		dialect = postgres.Open(dsn)
	case "mysql":
		dialect = mysql.New(mysql.Config{DSN: dsn, DefaultStringSize: 191})
	default:
		t.Fatal("ASSISTANT_TEST_DB_DRIVER must be postgres or mysql")
	}
	openDB := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(dialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal("cannot open disposable database (connection details suppressed)")
		}
		return db
	}
	db := openDB()
	engine := os.Getenv("ASSISTANT_TEST_DB_ENGINE")
	if engine == "" {
		engine = os.Getenv("ASSISTANT_TEST_DB_DRIVER")
	}
	if engine != "postgres" && engine != "mysql" && engine != "mariadb" {
		t.Fatal("ASSISTANT_TEST_DB_ENGINE must be postgres, mysql or mariadb")
	}
	if (engine == "postgres") != (os.Getenv("ASSISTANT_TEST_DB_DRIVER") == "postgres") {
		t.Fatal("migration engine does not match database driver")
	}
	migration, err := os.ReadFile(filepath.Join("migrations", engine, "001_conversations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal("cannot get disposable DB connection")
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Never adopt or clear an existing application table.
	if db.Migrator().HasTable(&conversation{}) {
		t.Fatal("refusing database with existing assistant conversation table")
	}
	// These migrations contain plain DDL only: no quoted semicolons, routines,
	// or comments with delimiters. Execute each statement without multiStatements.
	for _, statement := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal("conversation migration failed")
		}
	}
	t.Cleanup(func() {
		if err := db.Migrator().DropTable(&conversation{}); err != nil {
			t.Error(err)
		}
	})
	mem, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mem.Close() })
	s := &service{db: db, cache: mem}
	ctx := conversationContext(7, 11)
	key := "test:conversation:" + testConversationID
	now := time.Now().UTC()
	if err := db.Model(&conversation{}).Create(map[string]any{
		"id": testConversationID, "tenant_id": 7, "user_id": 11,
		"slot": 0, "name": "private", "messages": "[]", "revision": 0,
		"created_at": now, "updated_at": now,
	}).Error; err != nil {
		t.Fatal("seed insert failed")
	}

	insertSlot := func(id string, tid, uid int64, slot int) error {
		return db.Model(&conversation{}).Create(map[string]any{
			"id": id, "tenant_id": tid, "user_id": uid, "slot": slot,
			"name": "slot test", "messages": "[]", "revision": 0,
			"created_at": now, "updated_at": now,
		}).Error
	}
	// Uniqueness applies to the full owner tuple, not globally to a slot.
	if err := insertSlot(strings.Repeat("a", 32), 7, 11, 0); err == nil {
		t.Fatal("migration allowed duplicate owner slot")
	}
	if err := insertSlot(strings.Repeat("b", 32), 8, 11, 0); err != nil {
		t.Fatal("migration rejected same slot for another tenant")
	}
	if err := insertSlot(strings.Repeat("c", 32), 7, 12, 0); err != nil {
		t.Fatal("migration rejected same slot for another user")
	}
	for slot := 1; slot < 30; slot++ {
		id := strings.Repeat("d", 30) + string("0123456789abcdef"[slot/16]) + string("0123456789abcdef"[slot%16])
		if err := insertSlot(id, 7, 11, slot); err != nil {
			t.Fatal("valid slot rejected")
		}
	}
	for _, slot := range []int{-1, 30} {
		if err := insertSlot(strings.Repeat("e", 32), 7, 11, slot); err == nil {
			t.Fatalf("migration failed to enforce slot bounds: %d", slot)
		}
	}
	var count int64
	if err := s.conversationScope(ctx).Count(&count).Error; err != nil || count != 30 {
		t.Fatalf("owner count=%d, want 30 (query failed=%v)", count, err != nil)
	}

	// Two independent runners observed revision zero. The real database must
	// admit exactly one winner even when they race without the cache lock.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.saveConversation(ctx, key, 0, conversationHistory())
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if err.Error() != "conversation changed" {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("CAS winners=%d, want exactly one", winners)
	}

	// Reopen a fresh connection pool to prove the transcript is durable.
	reopened := openDB()
	reopenedConn, err := reopened.DB()
	if err != nil {
		t.Fatal("cannot get reopened DB connection")
	}
	t.Cleanup(func() { _ = reopenedConn.Close() })
	s.db = reopened

	for _, foreign := range []struct{ tid, uid int64 }{{8, 11}, {7, 12}} {
		other := conversationContext(foreign.tid, foreign.uid)
		if _, err := s.ownedConversation(other, testConversationID); err != gorm.ErrRecordNotFound {
			t.Fatalf("foreign owner could read: %v", err)
		}
		if err := s.saveConversation(other, key, 1, nil); err == nil || err.Error() != "conversation changed" {
			t.Fatalf("foreign owner could save: %v", err)
		}
	}
	if err := s.saveConversation(ctx, key, 0, nil); err == nil || err.Error() != "conversation changed" {
		t.Fatalf("stale save accepted: %v", err)
	}
	row, err := s.ownedConversation(ctx, testConversationID)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(conversationHistory())
	if row.Revision != 1 || row.Messages != string(expected) {
		t.Fatalf("losing writes changed durable history: revision=%d messages=%s", row.Revision, row.Messages)
	}
	loaded, _, err := s.load(ctx, key)
	if err != nil || loaded.revision != 1 || len(loaded.Messages) != 2 {
		t.Fatalf("durable load after cache miss: transcript=%+v err=%v", loaded, err)
	}
	// A valid newer save still works after stale/foreign attempts.
	if err := s.saveConversation(ctx, key, 1, nil); err != nil {
		t.Fatal(err)
	}
	row, err = s.ownedConversation(ctx, testConversationID)
	if err != nil || row.Revision != 2 || row.Messages != "null" {
		t.Fatalf("new revision not persisted: row=%+v err=%v", row, err)
	}
}
