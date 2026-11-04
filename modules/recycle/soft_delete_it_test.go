// 守卫：默认跳过；设置 NEXADMIN_IT_MYSQL_DSN / NEXADMIN_IT_PG_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC' \
//	NEXADMIN_IT_PG_DSN='host=localhost port=5432 user=postgres password=pass dbname=postgres sslmode=disable' \
//	go test ./modules/recycle -run TestIT -count=1 -v
//
// MySQL 侧库名必须留空（AGENTS.md）：夹具自己换成一次性库名，DSN 里写了库名就当场失败。
//
// 为什么这条不能只有 sqlmock：本项的核心改动是三段 DDL——加 deleted_at，把
// uk_sys_dict_type_code / uk_sys_dict_data_type_value / uk_sys_post_tenant_code 从唯一
// 降级成普通索引。MySQL 写 `DROP INDEX uk_x ON tbl`、PG 写 `DROP INDEX uk_x`，逐字节比对
// 三引擎文件一致也照不出语法错，只有真执行才知道。降级之后「同码可以重建」与「还原撞码
// 回 409」全靠应用层判定，而 sqlmock 从不执行 SQL，那些断言等于没断。
package recycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/migrate"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/recbin"
	"nexadmin/modules/system/dict"
	"nexadmin/modules/system/post"
	"nexadmin/modules/system/user"
	"nexadmin/pkg/idgen"
)

// itTenant 取一个远离演示数据的租户号，避免和同机其它真库用例串台。
const itTenant = 900007

func itCtx() context.Context { return tenant.With(context.Background(), idgen.ID(itTenant)) }

func itDSNMySQL(t *testing.T) (rootDSN, dbDSN, dbname string) {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL integration test")
	}
	d, err := mysql.ParseDSN(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.DBName != "" {
		t.Fatalf("MySQL DSN 的库名必须留空（夹具自建一次性库），实得 %q", d.DBName)
	}
	name := fmt.Sprintf("nexadmin_recyc_%d", time.Now().UnixNano())
	root := d.FormatDSN()
	d.DBName = name
	return root, d.FormatDSN(), name
}

// itOpenMySQL 建一次性库（库名由时间戳生成，不是请求输入）并跑完 mysql/mariadb 目录的框架迁移。
func itOpenMySQL(t *testing.T, engine string) *gorm.DB {
	t.Helper()
	rootDSN, dbDSN, name := itDSNMySQL(t)
	root, err := sql.Open("mysql", rootDSN)
	if err != nil {
		t.Fatal(err)
	}
	// 库名来自上面的时间戳拼接，无外部输入。
	if _, err := root.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		_ = root.Close()
		t.Fatalf("建一次性库失败: %v", err)
	}
	// 清场排在最后：先删库再关连接。反过来（defer 关连接）会让 DROP 撞上 "database is closed"，
	// 一次性库就一直堆在服务器上。
	t.Cleanup(func() {
		if _, err := root.Exec("DROP DATABASE " + name); err != nil {
			t.Errorf("DROP DATABASE %s: %v", name, err)
		}
		_ = root.Close()
	})
	db, err := gorm.Open(gormmysql.Open(dbDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	itApplyMigrations(t, db, engine)
	return db
}

// itOpenPG 同样建一次性库；DSN 里已写 dbname 的按原样替换，没写则追加。
func itOpenPG(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv("NEXADMIN_IT_PG_DSN")
	if raw == "" {
		t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL integration test")
	}
	name := fmt.Sprintf("nexadmin_recyc_%d", time.Now().UnixNano())
	root, err := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("PG 连不上: %v", err)
	}
	if err := root.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("建一次性库失败: %v", err)
	}
	sqlDB, err := root.DB()
	if err != nil {
		t.Fatal(err)
	}
	dsn := raw
	if i := strings.Index(raw, "dbname="); i >= 0 {
		end := len(raw)
		for j := i + len("dbname="); j < len(raw); j++ {
			if raw[j] == ' ' || raw[j] == ';' {
				end = j
				break
			}
		}
		dsn = raw[:i] + "dbname=" + name + raw[end:]
	} else {
		dsn = raw + " dbname=" + name
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	itApplyMigrations(t, db, "postgres")
	t.Cleanup(func() {
		// 先断开连到一次性库的那一路会话，再 DROP：PG 在有连接时删库直接报 55006。
		if d, derr := db.DB(); derr == nil {
			_ = d.Close()
		}
		if _, err := sqlDB.Exec("DROP DATABASE " + name); err != nil {
			t.Errorf("DROP DATABASE %s: %v", name, err)
		}
		_ = sqlDB.Close()
	})
	return db
}

// itApplyMigrations 按文件名顺序执行 migrations/framework/<engine>/ 下全部 SQL。
// 逐条执行，降级索引的 DROP INDEX 才会真说话。
func itApplyMigrations(t *testing.T, db *gorm.DB, engine string) {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations", "framework", engine)
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("没读到 %s 下的迁移文件（%d 个）", dir, len(files))
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, stmt := range migrate.Split(string(b)) {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("%s: %v\n语句：%s", filepath.Base(f), err, firstLine(stmt))
			}
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// itSoftDeleteColumns 钉住「四张表都有 deleted_at」——迁移文件写了不等于列真在。
func itSoftDeleteColumns(t *testing.T, db *gorm.DB, dialect string) {
	t.Helper()
	for _, table := range []string{"sys_role", "sys_dict_type", "sys_dict_data", "sys_post"} {
		var n int64
		if dialect == "postgres" {
			if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'deleted_at'", table).Scan(&n).Error; err != nil {
				t.Fatal(err)
			}
		} else {
			if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = 'deleted_at'", table).Scan(&n).Error; err != nil {
				t.Fatal(err)
			}
		}
		if n != 1 {
			t.Errorf("%s 上没有 deleted_at（查到 %d 列）", table, n)
		}
	}
}

// itUniqueDowngraded 钉住三条原唯一索引已经变成普通索引：还是唯一的话，软删除行会永久占住
// 编码，「同码重建」与还原都会撞数据库错误，而应用层 409 的语义就被顶掉了。
func itUniqueDowngraded(t *testing.T, db *gorm.DB, dialect string) {
	t.Helper()
	cases := []struct{ table, index string }{
		{"sys_dict_type", "uk_sys_dict_type_code"},
		{"sys_dict_data", "uk_sys_dict_data_type_value"},
		{"sys_post", "uk_sys_post_tenant_code"},
	}
	for _, c := range cases {
		var unique bool
		var err error
		if dialect == "postgres" {
			var n int64
			err = db.Raw(`SELECT COUNT(*)
				FROM pg_index ix
				JOIN pg_class i ON i.oid = ix.indexrelid
				JOIN pg_class t ON t.oid = ix.indrelid
				WHERE t.relname = ? AND i.relname = ? AND ix.indisunique`, c.table, c.index).Scan(&n).Error
			unique = n > 0
		} else {
			var n int64
			err = db.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? AND non_unique = 0", c.table, c.index).Scan(&n).Error
			unique = n > 0
		}
		if err != nil {
			t.Fatal(err)
		}
		if unique {
			t.Errorf("%s.%s 仍是唯一索引：唯一性降级没生效", c.table, c.index)
		}
		// 索引本身还得在，否则「同码查重」全靠应用层，性能上也退化了。
		var exists bool
		if dialect == "postgres" {
			var n int64
			if err := db.Raw("SELECT COUNT(*) FROM pg_class i JOIN pg_index ix ON ix.indexrelid = i.oid WHERE i.relname = ?", c.index).Scan(&n).Error; err != nil {
				t.Fatal(err)
			}
			exists = n > 0
		} else {
			var n int64
			if err := db.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?", c.table, c.index).Scan(&n).Error; err != nil {
				t.Fatal(err)
			}
			exists = n > 0
		}
		if !exists {
			t.Errorf("%s 上的索引 %s 不见了", c.table, c.index)
		}
	}
}

// itTrash 用与业务同一条路径把一行打进回收站：打 deleted_at + 同事务登记 recbin.Add。
func itTrash(t *testing.T, db *gorm.DB, model any, table string, typ string, id idgen.ID, name string) idgen.ID {
	t.Helper()
	var entry recbin.Entry
	err := db.WithContext(itCtx()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(model).Where("id = ?", int64(id)).Delete(model).Error; err != nil {
			return err
		}
		if err := recbin.Add(itCtx(), tx, typ, id, name, "it"); err != nil {
			return err
		}
		entry = recbin.Entry{}
		return tx.Where("ref_id = ?", int64(id)).First(&entry).Error
	})
	if err != nil {
		t.Fatalf("打进回收站失败: %v", err)
	}
	return entry.ID
}

func itErrKey(t *testing.T, err error) (int, string) {
	t.Helper()
	var re *response.Error
	if err == nil {
		return 0, ""
	}
	if !errors.As(err, &re) {
		t.Fatalf("错误类型不是 *response.Error: %#v (%v)", err, err)
	}
	return re.Status, re.Key
}

// runSoftDeleteInvariants 是与引擎无关的四条不变式，MySQL 与 PG 各跑一遍。
func runSoftDeleteInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc := NewService(db, zap.NewNop())
	ctx := itCtx()

	// 1) 岗位软删除：行还在，只是 deleted_at 非空；关联表一条没动。
	p := &post.Post{Name: "工程师", Code: "eng", Sort: 1, Status: "1"}
	if err := db.WithContext(ctx).Create(p).Error; err != nil {
		t.Fatalf("建岗位失败: %v", err)
	}
	entryID := itTrash(t, db, &post.Post{}, "sys_post", recbin.TypePost, p.ID, p.Name)
	var alive int64
	if err := db.WithContext(ctx).Model(&post.Post{}).Where("id = ?", int64(p.ID)).Count(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if alive != 0 {
		t.Fatal("软删除的岗位还能被业务读到，deleted_at 没打上")
	}
	var rawRows int64
	if err := db.WithContext(tenant.With(context.Background(), idgen.ID(itTenant))).Unscoped().
		Model(&post.Post{}).Where("id = ? AND deleted_at IS NOT NULL", int64(p.ID)).Count(&rawRows).Error; err != nil {
		t.Fatal(err)
	}
	if rawRows != 1 {
		t.Fatalf("源行不在了（找到 %d 行）：回收站的还原就无从谈起", rawRows)
	}

	// 2) 唯一索引已降级：同码可以再建一条，不再撞数据库错误。
	dupe := &post.Post{Name: "工程师二号", Code: "eng", Sort: 2, Status: "1"}
	if err := db.WithContext(ctx).Create(dupe).Error; err != nil {
		t.Fatalf("同码重建被数据库挡了（索引没降级？）: %v", err)
	}

	// 3) 还原撞码：同码有未删行时必须回 409 recycle.restore_conflict，而不是造出两条同码。
	if status, key := itErrKey(t, svc.Restore(ctx, entryID, true)); status != 409 || key != "recycle.restore_conflict" {
		t.Fatalf("还原撞码期望 409 recycle.restore_conflict，实得 %d %q", status, key)
	}

	// 4) 把占码的那条物理清掉后，还原必须成功且行回到未删状态。
	if err := db.WithContext(ctx).Unscoped().Where("id = ?", int64(dupe.ID)).Delete(&post.Post{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Restore(ctx, entryID, true); err != nil {
		t.Fatalf("占码行清掉后还原仍失败: %v", err)
	}
	if err := db.WithContext(ctx).Model(&post.Post{}).Where("id = ?", int64(p.ID)).Count(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if alive != 1 {
		t.Fatalf("还原后岗位没回到未删状态（读到 %d 行）", alive)
	}
	var entries int64
	if err := db.WithContext(ctx).Model(&recbin.Entry{}).Where("id = ?", int64(entryID)).Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatal("还原成功却还留着回收登记")
	}

	// 5) 平台级表（字典类型没有 tenant_id）：还原/彻底删除都不能拿租户当谓词，
	// 否则 SQL 引用了不存在的列，第一次真执行就炸。
	dt := &dict.Type{Name: "性别补充", Code: "sys_it_gender", Sort: 9, Status: "1"}
	if err := db.WithContext(ctx).Create(dt).Error; err != nil {
		t.Fatalf("建字典类型失败: %v", err)
	}
	dtEntryID := itTrash(t, db, &dict.Type{}, "sys_dict_type", recbin.TypeDictType, dt.ID, dt.Name)
	if err := svc.Restore(ctx, dtEntryID, true); err != nil {
		t.Fatalf("平台级表还原失败（多半是 srcScope 仍带 tenant_id）: %v", err)
	}
	// 彻底删除：类型进回收站后，其下条目按 recbin.Children 级联物理清掉。
	db.WithContext(ctx).Create(&dict.Data{TypeID: dt.ID, Label: "男", Value: "1", Sort: 1, Status: "1"})
	purgeID := itTrash(t, db, &dict.Type{}, "sys_dict_type", recbin.TypeDictType, dt.ID, dt.Name)
	if err := svc.Purge(ctx, purgeID, true); err != nil {
		t.Fatalf("彻底删除字典类型失败: %v", err)
	}
	var dataLeft int64
	if err := db.WithContext(ctx).Unscoped().Model(&dict.Data{}).Where("type_id = ?", int64(dt.ID)).Count(&dataLeft).Error; err != nil {
		t.Fatal(err)
	}
	if dataLeft != 0 {
		t.Fatalf("彻底删除类型后残留 %d 条字典条目", dataLeft)
	}
}

func TestITMySQLSoftDeleteRecycle(t *testing.T) {
	if err := idgen.Init(31); err != nil {
		t.Fatal(err)
	}
	db := itOpenMySQL(t, "mysql")
	itSoftDeleteColumns(t, db, "mysql")
	itUniqueDowngraded(t, db, "mysql")
	runSoftDeleteInvariants(t, db)
	runUserRestoreInvariants(t, db)
}

func TestITMariaDBSoftDeleteRecycle(t *testing.T) {
	if err := idgen.Init(31); err != nil {
		t.Fatal(err)
	}
	db := itOpenMySQL(t, "mariadb")
	itSoftDeleteColumns(t, db, "mysql")
	itUniqueDowngraded(t, db, "mysql")
	runSoftDeleteInvariants(t, db)
	runUserRestoreInvariants(t, db)
}

func TestITPostgresSoftDeleteRecycle(t *testing.T) {
	if err := idgen.Init(31); err != nil {
		t.Fatal(err)
	}
	db := itOpenPG(t)
	itSoftDeleteColumns(t, db, "postgres")
	itUniqueDowngraded(t, db, "postgres")
	runSoftDeleteInvariants(t, db)
	runUserRestoreInvariants(t, db)
}

// itSeedUser 直接建一行未删用户并返回其 ID（绕开建号服务的配额/角色校验）：
// 夹具库没装租户插件，但 TenantModel.BeforeCreate 仍按 ctx 填 tenant_id，所以它落在 itTenant 下。
// 用软删前的真实列名，字段与 user.User 模型对齐（Password 为空串＝不可登录，测试不登录）。
func itSeedUser(t *testing.T, db *gorm.DB, username, phone string) idgen.ID {
	t.Helper()
	u := &user.User{Username: username, Nickname: username, Phone: phone, Status: "1", Gender: "0"}
	if err := db.WithContext(itCtx()).Create(u).Error; err != nil {
		t.Fatalf("建用户 %s 失败: %v", username, err)
	}
	return u.ID
}

// itUsernameIndexNotUnique 钉住还原前提：三种引擎里 idx_sys_user_tenant_username 都是普通索引。
// 若哪天它变成唯一索引，还原撞码会由数据库抛错而不是应用层 409，本用例的语义就站不住了。
func itUsernameIndexNotUnique(t *testing.T, db *gorm.DB) {
	t.Helper()
	var n int64
	switch db.Dialector.Name() {
	case "postgres":
		if err := db.Raw(`SELECT COUNT(*)
			FROM pg_index ix
			JOIN pg_class i ON i.oid = ix.indexrelid
			JOIN pg_class t ON t.oid = ix.indrelid
			WHERE t.relname = 'sys_user' AND i.relname = 'idx_sys_user_tenant_username' AND ix.indisunique`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
	default: // mysql / mariadb
		if err := db.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'sys_user' AND index_name = 'idx_sys_user_tenant_username' AND non_unique = 0").Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
	}
	if n != 0 {
		t.Fatal("idx_sys_user_tenant_username 是唯一索引：还原同名应由数据库抛错，应用层 409 的语义不成立")
	}
}

// runUserRestoreInvariants 是 user 还原撞码的配对边界（MySQL / MariaDB / PG 各跑一遍）：
//
// 前提钉住：sys_user 的 idx_sys_user_tenant_username 在三种引擎都是普通索引（非唯一），
// 数据库挡不住「软删后又重注同名」——还原同名账号必须靠应用层 409，而不是等数据库抛错。
//
// 五条不变式：① 用户名撞码 → 409 且回收记录仍在、源行仍软删、未删同名仍只有一条（没有半写）；
// ② 手机号撞码同样 409；③ 空手机号不参与查重，两个空号用户不互相误判；
// ④ 占码行物理清掉后还原成功；⑤ 还原后只剩一条未删同名、回收登记消失、源行不再软删。
func runUserRestoreInvariants(t *testing.T, db *gorm.DB) {
	t.Helper()
	itUsernameIndexNotUnique(t, db)
	svc := NewService(db, zap.NewNop())
	ctx := itCtx()

	liveCount := func(username string) int64 {
		var n int64
		if err := db.Table("sys_user").Where("tenant_id = ? AND username = ? AND deleted_at IS NULL", itTenant, username).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	entryAlive := func(entryID idgen.ID) int64 {
		var n int64
		if err := db.WithContext(ctx).Table("sys_recycle").Where("id = ?", int64(entryID)).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	srcStillSoftDeleted := func(id idgen.ID) bool {
		var n int64
		if err := db.Table("sys_user").Where("id = ? AND deleted_at IS NOT NULL", int64(id)).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	// 1) 用户名撞码：老账号软删进回收站后，同名账号被重新注册（未删）→ 还原必须 409。
	old := itSeedUser(t, db, "r_conflict_u", "")
	oldEntry := itTrash(t, db, &user.User{}, "sys_user", recbin.TypeUser, old, old.String())
	if err := db.WithContext(ctx).Create(&user.User{Username: "r_conflict_u", Nickname: "重注", Status: "1", Gender: "0"}).Error; err != nil {
		t.Fatalf("重注同名账号被数据库挡了（索引难道是唯一？）: %v", err)
	}
	if status, key := itErrKey(t, svc.Restore(ctx, oldEntry, true)); status != 409 || key != "recycle.restore_conflict" {
		t.Fatalf("同名还原期望 409 recycle.restore_conflict，实得 %d %q", status, key)
	}
	if entryAlive(oldEntry) != 1 {
		t.Fatal("撞码还原后回收记录应仍在（未半写：记录没被删）")
	}
	if !srcStillSoftDeleted(old) {
		t.Fatal("撞码还原后源行应仍是软删除（未半写：deleted_at 没被清）")
	}
	if liveCount("r_conflict_u") != 1 {
		t.Fatalf("撞码还原不应造出第二条未删同名（实得 %d）", liveCount("r_conflict_u"))
	}

	// 2) 手机号撞码：用户名不同但手机号与一条未删账号相同 → 同样 409（否则短信登录 Limit(2) 命中两行拒登）。
	oldP := itSeedUser(t, db, "r_conflict_p", "13900000002")
	oldPEntry := itTrash(t, db, &user.User{}, "sys_user", recbin.TypeUser, oldP, oldP.String())
	if err := db.WithContext(ctx).Create(&user.User{Username: "r_other_name", Nickname: "占号", Phone: "13900000002", Status: "1", Gender: "0"}).Error; err != nil {
		t.Fatalf("建同号用户失败: %v", err)
	}
	if status, key := itErrKey(t, svc.Restore(ctx, oldPEntry, true)); status != 409 || key != "recycle.restore_conflict" {
		t.Fatalf("同号还原期望 409，实得 %d %q", status, key)
	}

	// 3) 空手机号不误判：两个空号用户不该因 phone='' 互相撞码；用户名不同也应放行。
	oldE := itSeedUser(t, db, "r_empty_a", "")
	oldEEntry := itTrash(t, db, &user.User{}, "sys_user", recbin.TypeUser, oldE, oldE.String())
	if err := db.WithContext(ctx).Create(&user.User{Username: "r_empty_b", Nickname: "也空号", Status: "1", Gender: "0"}).Error; err != nil {
		t.Fatalf("建第二个空号用户失败: %v", err)
	}
	if err := svc.Restore(ctx, oldEEntry, true); err != nil {
		t.Fatalf("空手机号不应误判撞码，还原应成功，实得 %v", err)
	}

	// 4) 配对边界：物理清掉占码的同名账号后，还原必须成功且只剩一条未删同名行。
	if err := db.Unscoped().Table("sys_user").Where("username = ? AND id <> ?", "r_conflict_u", int64(old)).Delete(nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.Restore(ctx, oldEntry, true); err != nil {
		t.Fatalf("占码行清掉后还原仍失败: %v", err)
	}
	if liveCount("r_conflict_u") != 1 {
		t.Fatalf("还原后未删同名应为 1，实得 %d", liveCount("r_conflict_u"))
	}
	if entryAlive(oldEntry) != 0 {
		t.Fatal("还原成功却还留着回收登记")
	}
	if srcStillSoftDeleted(old) {
		t.Fatal("还原成功后源行不应仍是软删除")
	}
}
