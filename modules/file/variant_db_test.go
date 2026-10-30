package file

import (
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func ginCtxWithAccept(accept string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/x", nil)
	if accept != "" {
		c.Request.Header.Set("Accept", accept)
	}
	return c
}

// 客户端接受 WebP 且原图有副本时，必须换成副本（URL 不变，靠 Accept 协商）。
func TestNegotiateVariant(t *testing.T) {
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	db, mock := newMockDB(t)
	h := &Handler{svc: &Service{db: db}}
	orig := File{Name: "a.jpg", Category: "image", MimeType: "image/jpeg", StoragePath: "1/20261029/1.jpg"}
	orig.ID = 123

	rows := sqlmock.NewRows([]string{
		"id", "created_at", "updated_at", "tenant_id", "group_id", "name", "storage_path", "url", "poster_url",
		"mime_type", "category", "size", "hash", "visibility", "token", "uploader", "variant_of",
	}).AddRow(456, nil, nil, 1, 0, "a.webp", "1/20261029/2.webp", "", "", "image/webp", "image", 12, "", "public", "tk", "admin", 123)

	mock.ExpectQuery("SELECT \\* FROM `sys_file` WHERE variant_of = \\? AND mime_type = \\? AND storage_path <> '' ORDER BY `sys_file`.`id` LIMIT \\?").
		WithArgs(int64(123), "image/webp", 1).
		WillReturnRows(rows)

	got := h.negotiateVariant(ginCtxWithAccept("image/avif,image/webp,*/*"), orig)
	if got.ID != 456 || got.MimeType != "image/webp" {
		t.Fatalf("should serve variant, got id=%v mime=%q", got.ID, got.MimeType)
	}
	if got.Name != "a.jpg" {
		t.Fatalf("variant keeps original display name, got %q", got.Name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// 不接受 WebP、或本身就是副本时，不得查询数据库、原样返回。
func TestNegotiateVariantSkips(t *testing.T) {
	db, mock := newMockDB(t)
	h := &Handler{svc: &Service{db: db}}
	orig := File{Name: "a.png", Category: "image", MimeType: "image/png", StoragePath: "1/20261029/3.png"}
	orig.ID = 789

	if got := h.negotiateVariant(ginCtxWithAccept("image/png"), orig); got.ID != 789 || got.MimeType != "image/png" {
		t.Fatalf("png client must get original, got %v %q", got.ID, got.MimeType)
	}
	variant := orig
	variant.VariantOf = 123
	if got := h.negotiateVariant(ginCtxWithAccept("image/webp"), variant); got.VariantOf != 123 {
		t.Fatalf("variant row must not be re-negotiated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no query expected: %v", err)
	}
}
