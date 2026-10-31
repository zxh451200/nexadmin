package backup

import (
	"context"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDebugOrder(t *testing.T) {
	sqlDB, mock, _ := sqlmock.New()
	defer sqlDB.Close()
	lg := logger.New(Writer{os.Stdout}, logger.Config{LogLevel: logger.Info, Colorful: false})
	db, _ := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: lg})
	s := svcOn(db, t.TempDir())
	_ = zap.NewNop()
	mock.MatchExpectationsInOrder(false)
	mock.ExpectAny()
	s.Start(context.Background(), "manual", 1)
}

type Writer struct{ f *os.File }

func (w Writer) Printf(format string, args ...any) { fmt2(w.f, format, args...) }
