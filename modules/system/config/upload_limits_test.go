package config

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func newUploadSvc(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db), mock
}

func uploadRows(img, vid, file, types string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"config_key", "config_value"}).
		AddRow(KeyUploadImage, img).AddRow(KeyUploadVideo, vid).AddRow(KeyUploadFile, file).AddRow(KeyUploadTypes, types)
}

// 设置页的值经 uploadLimits 提供给文件模块；短缓存内不重复读库，保存后立即失效。
func TestUploadLimitsCachedAndInvalidatedOnSave(t *testing.T) {
	s, mock := newUploadSvc(t)
	ctx := context.Background()
	mock.ExpectQuery("FROM `sys_config`").WillReturnRows(uploadRows("5", "0", "20.5", "image/png, .pdf"))

	l, err := s.uploadLimits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if l.MaxImageMB != 5 || l.MaxVideoMB != 0 || l.MaxFileMB != 20.5 || len(l.AllowedTypes) != 2 || l.AllowedTypes[1] != ".pdf" {
		t.Fatalf("limits = %+v", l)
	}
	if _, err := s.uploadLimits(ctx); err != nil { // 命中缓存：没有第二次 ExpectQuery，读库会失败
		t.Fatalf("second call must hit the cache: %v", err)
	}

	// 保存设置页 -> 缓存失效 -> 下一次重新读库拿到新值。
	s.invalidateUpload()
	mock.ExpectQuery("FROM `sys_config`").WillReturnRows(uploadRows("8", "0", "0", ""))
	l, err = s.uploadLimits(ctx)
	if err != nil || l.MaxImageMB != 8 || len(l.AllowedTypes) != 0 {
		t.Fatalf("after invalidate: %+v, %v", l, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 读库失败不缓存、返回错误（文件模块据此回落 config.yaml）。
func TestUploadLimitsErrorIsNotCached(t *testing.T) {
	s, mock := newUploadSvc(t)
	mock.ExpectQuery("FROM `sys_config`").WillReturnError(context.DeadlineExceeded)
	if _, err := s.uploadLimits(context.Background()); err == nil {
		t.Fatal("db error must surface")
	}
	mock.ExpectQuery("FROM `sys_config`").WillReturnRows(uploadRows("3", "0", "0", ""))
	if l, err := s.uploadLimits(context.Background()); err != nil || l.MaxImageMB != 3 {
		t.Fatalf("retry after error: %+v, %v", l, err)
	}
}
