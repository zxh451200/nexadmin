package realname

// mariadb 方言门禁：本机没有 MariaDB 实例可跑真引擎，所以把"mariadb 与 mysql 逐字节相同"
// 变成会红的断言——mysql 那份已在真 MySQL 8 上执行通过（it_mysql_test.go），
// 只要有人单独改了 mariadb 文件而没同步，这里就失败。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMariaDBMigrationIsByteIdenticalToMySQL(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("migrations", "mysql", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find mysql migrations: %v (%d)", err, len(files))
	}
	for _, f := range files {
		other := filepath.Join("migrations", "mariadb", filepath.Base(f))
		a, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(other)
		if err != nil {
			t.Fatalf("read %s: %v", other, err)
		}
		if string(a) != string(b) {
			t.Errorf("%s 与 mariadb 同名文件不再逐字节相同：真引擎只验过 mysql 那份，"+
				"改 mariadb 前请同步 mysql 或在 MariaDB 实例上实跑", filepath.Base(f))
		}
	}
}
