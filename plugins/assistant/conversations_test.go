package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	coreai "nexadmin/core/ai"
	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testConversationID = "0123456789abcdef0123456789abcdef"

func conversationTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		_ = conn.Close()
	})
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func conversationContext(tid, uid int64) context.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(uid), TenantID: idgen.ID(tid), Super: true})
	return tenant.With(c.Request.Context(), idgen.ID(tid))
}

func conversationHistory() []coreai.Message {
	return []coreai.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "world"}}
}

func TestConversationOwnershipAndRevisionSQL(t *testing.T) {
	for _, owner := range []struct {
		name     string
		tid, uid int64
	}{{"owner", 7, 11}, {"other tenant same user", 8, 11}, {"same tenant other user", 7, 12}} {
		t.Run(owner.name, func(t *testing.T) {
			db, mock := conversationTestDB(t)
			s := &service{db: db}
			ctx := conversationContext(owner.tid, owner.uid)
			rows := sqlmock.NewRows([]string{"id", "messages", "revision"})
			if owner.name == "owner" {
				rows.AddRow(testConversationID, "[]", 4)
			}
			mock.ExpectQuery("SELECT .* FROM `plg_assistant_conversation` WHERE \\(tenant_id = \\? AND user_id = \\?\\) AND id = \\?").
				WithArgs(owner.tid, owner.uid, testConversationID, 1).WillReturnRows(rows)
			row, err := s.ownedConversation(ctx, testConversationID)
			if owner.name == "owner" {
				if err != nil || row.Revision != 4 {
					t.Fatalf("owner read: revision=%d err=%v", row.Revision, err)
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("foreign read must be invisible: %v", err)
			}
			// A successful first writer advances the revision. Replaying that
			// revision (or using another owner) must fail on zero affected rows.
			attempts := []int64{0}
			if owner.name == "owner" {
				attempts = []int64{1, 0}
			}
			for _, affected := range attempts {
				mock.ExpectBegin()
				mock.ExpectExec("UPDATE `plg_assistant_conversation` SET `messages`=\\?,`revision`=\\?,`updated_at`=\\? WHERE \\(tenant_id = \\? AND user_id = \\?\\) AND \\(id = \\? AND revision = \\?\\)").
					WithArgs(`[{"role":"user","content":"hello"},{"role":"assistant","content":"world"}]`,
						5, sqlmock.AnyArg(), owner.tid, owner.uid, testConversationID, 4).
					WillReturnResult(sqlmock.NewResult(0, affected))
				mock.ExpectCommit()
				err := s.saveConversation(ctx, "test:conversation:"+testConversationID, 4, conversationHistory())
				if affected == 1 && err != nil || affected == 0 && (err == nil || err.Error() != "conversation changed") {
					t.Fatalf("affected=%d save error=%v", affected, err)
				}
			}
		})
	}
}

func TestConversationInvalidHistoryBeforeDB(t *testing.T) {
	cases := map[string][]coreai.Message{
		"incomplete pair":       {{Role: "user", Content: "hello"}},
		"system injection":      {{Role: "system"}, {Role: "assistant"}},
		"reversed roles":        {{Role: "assistant"}, {Role: "user"}},
		"tool result":           {{Role: "user"}, {Role: "tool", ToolCallID: "secret"}},
		"tool call":             {{Role: "user"}, {Role: "assistant", ToolCalls: []coreai.ToolCall{{ID: "secret"}}}},
		"hidden tool reference": {{Role: "user", ToolCallID: "secret"}, {Role: "assistant"}},
		"invalid utf8":          {{Role: "user", Content: string([]byte{0xff})}, {Role: "assistant"}},
		"oversized":             {{Role: "user", Content: strings.Repeat("x", maxHistory)}, {Role: "assistant"}},
		"too many pairs":        make([]coreai.Message, 14),
	}
	for i := range cases["too many pairs"] {
		cases["too many pairs"][i].Role = []string{"user", "assistant"}[i%2]
	}
	for name, messages := range cases {
		t.Run(name, func(t *testing.T) {
			db, _ := conversationTestDB(t) // No SQL expectations: touching DB is a failure.
			err := (&service{db: db}).saveConversation(conversationContext(7, 11),
				"test:conversation:"+testConversationID, 0, messages)
			if err == nil || err.Error() != "invalid history" {
				t.Fatalf("expected validation before DB, got %v", err)
			}
		})
	}
}

func TestConversationMissingIdentityBeforeDB(t *testing.T) {
	for _, ctx := range []context.Context{context.Background(), conversationContext(0, 11), conversationContext(7, 0)} {
		db, _ := conversationTestDB(t)
		s := &service{db: db}
		if _, err := s.ownedConversation(ctx, testConversationID); err == nil || err.Error() != "storage unavailable" {
			t.Fatalf("read without identity: %v", err)
		}
		if err := s.saveConversation(ctx, "test:conversation:"+testConversationID, 0, conversationHistory()); err == nil || err.Error() != "storage unavailable" {
			t.Fatalf("save without identity: %v", err)
		}
	}
}

func TestConversationCanonicalIDs(t *testing.T) {
	for _, id := range []string{testConversationID, strings.Repeat("0", 32), strings.Repeat("f", 32)} {
		if !validConversationID(id) {
			t.Errorf("canonical hex ID rejected: %q", id)
		}
	}
	for _, id := range []string{"", "1", strings.Repeat("a", 31), strings.Repeat("a", 33),
		strings.ToUpper(testConversationID), strings.Repeat("g", 32), " " + testConversationID,
		testConversationID + "\n", strings.Repeat("é", 16)} {
		if validConversationID(id) {
			t.Errorf("noncanonical ID accepted: %q", id)
		}
	}
}

type conversationProvider struct{}

func (conversationProvider) Login(context.Context, auth.LoginRequest) (*auth.User, error) {
	return nil, errors.New("login unused")
}
func (conversationProvider) Profile(_ context.Context, uid idgen.ID) (*auth.User, error) {
	return &auth.User{ID: uid, Perms: []string{"assistant:use"}}, nil
}

func conversationAuth(t *testing.T) {
	t.Helper()
	previous := auth.Default()
	s := &auth.Service{}
	s.SetProvider(conversationProvider{})
	auth.SetDefault(s)
	t.Cleanup(func() { auth.SetDefault(previous) })
}

func TestCreateConversationSlots(t *testing.T) {
	conversationAuth(t) // Global auth service: intentionally no parallel subtests.
	for _, tc := range []struct {
		name   string
		used   []int
		slot   int
		status int
	}{
		{"empty", nil, 0, 200},
		{"reuse gap", []int{0, 2, 3}, 1, 200},
		{"last slot", sequenceSlots(29), 29, 200},
		{"full", sequenceSlots(30), 0, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := conversationTestDB(t)
			mem, err := cache.New(config.Cache{Driver: "memory"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = mem.Close() })
			s := &service{db: db, cache: mem}
			rows := sqlmock.NewRows([]string{"slot"})
			for _, slot := range tc.used {
				rows.AddRow(slot)
			}
			mock.ExpectQuery("SELECT `slot` FROM `plg_assistant_conversation` WHERE tenant_id = \\? AND user_id = \\? LIMIT \\?").
				WithArgs(7, 11, 30).WillReturnRows(rows)
			if tc.status == 200 {
				mock.ExpectBegin()
				mock.ExpectExec("INSERT INTO `plg_assistant_conversation`").
					WithArgs(sqlmock.AnyArg(), 7, 11, tc.slot, "new conversation", "[]", 0, sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/conversations", strings.NewReader(`{"name":" new conversation "}`)).
				WithContext(conversationContext(7, 11))
			s.createConversation(c)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.status == 200 {
				var body struct {
					Data struct {
						ID   string
						Name string
					}
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if !validConversationID(body.Data.ID) || len(body.Data.ID) != 32 || body.Data.Name != "new conversation" {
					t.Fatalf("invalid created conversation: %s", w.Body)
				}
			}
			// Every exit must release the personal-list lease.
			_, key, err := identity(c.Request.Context())
			if err != nil {
				t.Fatal(err)
			}
			if locked, err := mem.Exists(c.Request.Context(), key+":conversations:lock"); err != nil || locked {
				t.Fatalf("list lock leaked: locked=%v err=%v", locked, err)
			}
		})
	}
}

func sequenceSlots(n int) []int {
	slots := make([]int, n)
	for i := range slots {
		slots[i] = i
	}
	return slots
}
