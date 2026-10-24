package file

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type fileSource map[idgen.ID]*tenant.Info

func (f fileSource) TenantInfo(_ context.Context, id idgen.ID) (*tenant.Info, error) {
	return f[id], nil
}

func TestUploadReservations(t *testing.T) {
	const tid = idgen.ID(71)
	tenant.SetSource(fileSource{tid: {ID: tid, MaxStorageMB: 1}, 72: {ID: 72, MaxStorageMB: 1}})
	t.Cleanup(func() { tenant.SetSource(nil) })
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := &Service{db: db, cache: c}
	other := &Service{db: db, cache: c}
	ctx := tenant.With(context.Background(), tid)
	makeUpload := func(id string, tenantID idgen.ID, length int64) *session {
		return &session{ID: id, TenantID: tenantID.String(), Length: length, CreatedAt: time.Now()}
	}
	// 两个 handler/service 共用后端，并发竞争同一租户最后 1 MB。
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < 16; i++ {
		mock.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(0))
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	var winner *session
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u := makeUpload(idgen.ID(100+i).String(), tid, mb)
			svc := s
			if i%2 == 0 {
				svc = other
			}
			if err := svc.reserveStorage(ctx, u); err == nil {
				accepted.Add(1)
				mu.Lock()
				winner = u
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d uploads", accepted.Load())
	}
	// 已入库但未释放时，SQL 排除该预占 ID，只计算一次；重复释放不能扣掉别人的空间。
	mock.ExpectQuery("id NOT IN").WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(0))
	if err := other.reserveStorage(ctx, winner); err != nil {
		t.Fatal(err)
	}
	if err := other.releaseStorage(winner); err != nil {
		t.Fatal(err)
	}
	if err := s.releaseStorage(winner); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(mb))
	if err := s.reserveStorage(ctx, makeUpload("200", tid, 1)); err == nil {
		t.Fatal("committed usage ignored")
	}
	mock.ExpectQuery("tenant_id = \\?").WithArgs(idgen.ID(72)).WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(0))
	u := makeUpload("201", 72, mb)
	if err := other.reserveStorage(tenant.With(context.Background(), 72), u); err != nil {
		t.Fatal(err)
	}
	if err := s.reserveStorage(ctx, u); err == nil {
		t.Fatal("cross tenant accepted")
	}
	if err := s.reserveStorage(ctx, makeUpload("202", tid, -1)); err == nil {
		t.Fatal("negative accepted")
	}
	if err := (&Service{}).reserveStorage(ctx, makeUpload("203", tid, 1)); err == nil {
		t.Fatal("missing atomic capability accepted")
	}
	mock.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(1))
	if err := s.reserveStorage(ctx, makeUpload("204", tid, math.MaxInt64)); err == nil {
		t.Fatal("overflow accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTusVerifyRealHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, kind, declared, data, want string
		reject                           bool
	}{
		{"fake.png", "image", "image/png", "this is plain text", "", true},
		{"fake.mp4", "video", "video/mp4", "this is plain text", "", true},
		{"fake.pdf", "file", "application/pdf", "<!DOCTYPE html><html><script>alert(1)</script></html>", "", true},
		{"real.png", "image", "image/jpeg", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png", false},
		{"real.pdf", "file", "application/octet-stream", "%PDF-1.7\n", "application/pdf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "header")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString(tc.data); err != nil {
				t.Fatal(err)
			}
			s := &session{Name: tc.name, Kind: tc.kind, Type: tc.declared, Length: int64(len(tc.data))}
			err = (&tusHandler{}).verify(s, f)
			if (err != nil) != tc.reject {
				t.Fatalf("verify error = %v", err)
			}
			if !tc.reject && (!s.Verified || s.Type != tc.want) {
				t.Fatalf("detected %+v", s)
			}
		})
	}
}

func TestDropReleasesReservationOnce(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	h := &tusHandler{svc: &Service{cache: c, store: &store{root: root}}}
	s := &session{ID: "501", TenantID: "71", Length: mb, Reserved: true, CreatedAt: time.Now()}
	l, _ := cache.As[cache.Lease](c)
	err = l.UpdateLeases(context.Background(), reservationKey(71), sessionMaxAge, func(entries map[string]cache.LeaseEntry) error {
		entries[s.ID] = cache.LeaseEntry{Bytes: mb, Until: time.Now().Add(time.Hour)}
		entries["502"] = cache.LeaseEntry{Bytes: mb, Until: time.Now().Add(time.Hour)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.saveSession(s); err != nil {
		t.Fatal(err)
	}
	if err := h.drop(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.drop(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.UpdateLeases(context.Background(), reservationKey(71), sessionMaxAge, func(entries map[string]cache.LeaseEntry) error {
		if len(entries) != 1 || entries["502"].Bytes != mb {
			t.Fatalf("remaining %+v", entries)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTusReservationLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"delete", "cleanup", "completed-retry", "invalid-header", "finish-success", "finish-failure"} {
		t.Run(mode, func(t *testing.T) {
			ca, err := cache.New(config.Cache{Driver: "memory"})
			if err != nil {
				t.Fatal(err)
			}
			defer ca.Close()
			st, err := newStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			h := &tusHandler{svc: &Service{cache: ca, store: st}}
			s := &session{ID: "601", TenantID: "71", UserID: "91", Length: 7,
				Name: "fake.png", Kind: "image", Type: "image/png", Reserved: true, CreatedAt: time.Now()}
			if mode == "completed-retry" {
				s.Completed = true
				s.Offset = s.Length
			}
			var mock sqlmock.Sqlmock
			if strings.HasPrefix(mode, "finish-") {
				s.Kind, s.Type, s.Name = "file", "text/plain", "hello.txt"
				sqlDB, sqlMock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer sqlDB.Close()
				mock = sqlMock
				db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				h.svc.db = db
				st.blobs = &memStore{data: map[string][]byte{}}
				mock.ExpectBegin()
				if mode == "finish-success" {
					mock.ExpectExec("INSERT INTO `sys_file`").WillReturnResult(sqlmock.NewResult(601, 1))
					mock.ExpectCommit()
				} else {
					mock.ExpectExec("INSERT INTO `sys_file`").WillReturnError(errors.New("insert failed"))
					mock.ExpectRollback()
				}
			}
			l, _ := cache.As[cache.Lease](ca)
			if err := l.UpdateLeases(context.Background(), reservationKey(71), sessionMaxAge, func(entries map[string]cache.LeaseEntry) error {
				entries[s.ID] = cache.LeaseEntry{Bytes: s.Length, Until: time.Now().Add(time.Hour)}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := h.saveSession(s); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(st.tmpPath(s.ID), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "cleanup" {
				old := time.Now().Add(-25 * time.Hour)
				for _, p := range []string{st.infoPath(s.ID), st.tmpPath(s.ID)} {
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
				h.cleanup()
			} else {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				body := ""
				if mode == "invalid-header" {
					body = "not png"
				}
				if strings.HasPrefix(mode, "finish-") {
					body = "hello!!"
				}
				c.Request = httptest.NewRequest(http.MethodPatch, "/upload/601", strings.NewReader(body))
				c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 71))
				c.Request.Header.Set("Tus-Resumable", tusVersion)
				c.Request.Header.Set("Content-Type", "application/offset+octet-stream")
				c.Request.Header.Set("Upload-Offset", "0")
				c.Params = gin.Params{{Key: "id", Value: s.ID}}
				auth.SetClaims(c, &auth.Claims{UserID: 91})
				if mode == "delete" {
					// 其它租户不能通过 DELETE 释放当前租户的预占。
					c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 72))
					h.Terminate(c)
					if w.Code != http.StatusNotFound {
						t.Fatalf("cross tenant status %d", w.Code)
					}
					w = httptest.NewRecorder()
					c, _ = gin.CreateTestContext(w)
					c.Request = httptest.NewRequest(http.MethodDelete, "/upload/601", nil)
					c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 71))
					c.Request.Header.Set("Tus-Resumable", tusVersion)
					c.Params = gin.Params{{Key: "id", Value: s.ID}}
					auth.SetClaims(c, &auth.Claims{UserID: 91})
					h.Terminate(c)
				} else {
					h.Patch(c)
				}
				c.Writer.WriteHeaderNow()
				want := http.StatusNoContent
				if mode == "invalid-header" {
					want = http.StatusUnsupportedMediaType
				}
				if mode == "finish-failure" {
					want = http.StatusInternalServerError
				}
				if w.Code != want {
					t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
				}
			}
			if mock != nil {
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
				n := len(st.blobs.(*memStore).data)
				if mode == "finish-success" && n != 1 || mode == "finish-failure" && n != 0 {
					t.Fatalf("stored objects %d", n)
				}
			}
			if _, err := os.Stat(st.infoPath(s.ID)); !os.IsNotExist(err) {
				t.Fatalf("session retained: %v", err)
			}
			if err := l.UpdateLeases(context.Background(), reservationKey(71), sessionMaxAge, func(entries map[string]cache.LeaseEntry) error {
				if len(entries) != 0 {
					t.Fatalf("reservation leaked: %+v", entries)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func (fileSource) TenantByDomain(context.Context, string) (idgen.ID, bool, error) {
	return 0, false, nil
}

const mb = int64(1 << 20)

// 用 sqlmock 验证，没有连接真实数据库。
func TestEnsureStorageQuotaAndCaching(t *testing.T) {
	const tid = idgen.ID(31)
	tenant.SetSource(fileSource{tid: {ID: tid, Status: "active", MaxStorageMB: 10}, 32: {ID: 32, Status: "active"}})
	tenant.StorageMeter.Invalidate(tid)
	t.Cleanup(func() { tenant.SetSource(nil); tenant.StorageMeter.Invalidate(tid) })

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db}
	ctx := tenant.With(context.Background(), tid)

	// 只汇总一次：已用 8 MB（仅本地文件，外链 storage_path 为空不计）
	mock.ExpectQuery("COALESCE\\(SUM\\(size\\), 0\\) FROM `sys_file` WHERE storage_path <> ''").
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(8 * mb))
	if err := s.ensureStorage(ctx, 2*mb); err != nil {
		t.Fatalf("exactly at the limit must pass: %v", err)
	}
	err = s.ensureStorage(ctx, 2*mb+1)
	var e *response.Error
	if !errors.As(err, &e) || e.Status != 403 || e.Key != "tenant.quota_storage" || e.Reason != tenant.ReasonQuotaStorage {
		t.Fatalf("over limit must be rejected: %v", err)
	}
	// 增量修正：上传完成 +1 MB 后，再传 1 MB 刚好到上限，传 1 MB+1 被拒；全程不再查库
	tenant.StorageMeter.Add(tid, mb)
	if err := s.ensureStorage(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureStorage(ctx, mb+1); err == nil {
		t.Fatal("incremental counter not applied")
	}
	// 删除文件后释放额度
	tenant.StorageMeter.Add(tid, -5*mb)
	if err := s.ensureStorage(ctx, 6*mb); err != nil {
		t.Fatalf("freed space should be reusable: %v", err)
	}
	// 不限制与平台级操作：没有额外 SQL
	if err := s.ensureStorage(tenant.With(context.Background(), 32), 1<<40); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureStorage(tenant.Skip(ctx), 1<<40); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
