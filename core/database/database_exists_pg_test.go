package database

// 真实 PostgreSQL 集成测试：DatabaseExists / EnsureDatabase 的 PG 分支此前没有任何测试经过——
// 那条判据在两个引擎上是两条不同的 SQL（mysql 查 information_schema.schemata，
// postgres 查 pg_database），文本上写错一个词只会让「库不存在」被判成「存在」，
// 而初始化会直接建库失败或跳过建库。本文件把 pg_database 那一支跑成真被执行过。
//
// 守卫：默认跳过；设置 NEXADMIN_IT_PG_DSN 才执行，例如
//
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=secret dbname=postgres sslmode=disable' \
//	  go test ./core/database -run TestITPostgresDatabaseExists -v
//
// 库名要有真值（pgx 的 keyword DSN 没有空值写法，见 AGENTS.md 第 3 节）；那条 dbname 只用于
// 「先连上服务器」，被测库名由用例生成。隔离：只操作自建的 nexadmin_dbexists_pg_<纳秒>，用完 DROP。

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"

	"nexadmin/core/config"
)

// livePGConfig 把 keyword=value 形式的 libpq DSN 变成 config.Database（环境里不含带空格/引号的值）。
func livePGConfig(t *testing.T, name string) config.Database {
	t.Helper()
	dsn := os.Getenv("NEXADMIN_IT_PG_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL test")
	}
	kv := map[string]string{}
	for _, part := range strings.Fields(dsn) {
		if k, v, ok := strings.Cut(part, "="); ok {
			kv[k] = v
		}
	}
	port, err := strconv.Atoi(kv["port"])
	if kv["host"] == "" || kv["user"] == "" || err != nil {
		t.Fatalf("NEXADMIN_IT_PG_DSN 需要 host/port/user（password 可为空）：解析得到 %+v，port 转换 err=%v", kv, err)
	}
	return config.Database{
		Type: "postgres", Host: kv["host"], Port: port, User: kv["user"], Password: kv["password"], Name: name,
	}
}

func TestITPostgresDatabaseExists(t *testing.T) {
	ctx := context.Background()
	name := fmt.Sprintf("nexadmin_dbexists_pg_%d", time.Now().UnixNano())
	cfg := livePGConfig(t, name)

	t.Cleanup(func() {
		drop, err := openRaw(cfg, "", logger.Silent)
		if err != nil {
			t.Errorf("cleanup connect: %v", err)
			return
		}
		defer Close(drop)
		if err := drop.Exec(`DROP DATABASE IF EXISTS "` + name + `"`).Error; err != nil {
			t.Errorf("cleanup drop %s: %v", name, err)
		}
	})

	if got, err := DatabaseExists(ctx, cfg); err != nil || got {
		t.Fatalf("before create: exists=%v err=%v, want false/nil", got, err)
	}
	if err := EnsureDatabase(ctx, cfg); err != nil {
		t.Fatalf("EnsureDatabase: %v", err)
	}
	if got, err := DatabaseExists(ctx, cfg); err != nil || !got {
		t.Fatalf("after create: exists=%v err=%v, want true/nil", got, err)
	}
	// 幂等：同一份配置再跑一次不该报「已存在」。
	if err := EnsureDatabase(ctx, cfg); err != nil {
		t.Fatalf("EnsureDatabase 第二次应当幂等: %v", err)
	}
	// 名称不合法时报错，而不是当成「不存在」悄悄放过（这一条判据在引擎之前，两引擎同口径）。
	if got, err := DatabaseExists(ctx, dbWith(cfg, name+"-dash")); err == nil || got {
		t.Fatalf("invalid name: exists=%v err=%v, want error", got, err)
	}
	// 口令错时必须报连接错误（调用方据此 fail-closed），不能判成「库不存在」。
	wrong := cfg
	wrong.Password += "!wrong"
	if got, err := DatabaseExists(ctx, wrong); err == nil || got {
		t.Fatalf("wrong password: exists=%v err=%v, want connection error and false", got, err)
	}
}
