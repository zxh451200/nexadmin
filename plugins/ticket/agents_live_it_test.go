package ticket

// 真库用例：`queryAgents` 此前在真库上从来没跑通过，也没人知道。
// 工单的 IT 一次性库只建插件自己的表，而这条查询（service.go:1166）要 sys_user / sys_user_role /
// sys_role / sys_role_menu / sys_menu 五张框架表，于是它每次都返回 Error 1146「表不存在」，
// 而通知路径 sendNotify 是尽力而为、错误不进返回值——用例照样绿，「通知发给了谁」这一格其实是空跑。
// 这份用例把框架迁移与插件迁移一起建进一次性库，逼这条 SQL 真执行一次，并断言坐席集合按
// 「超管 / 内置角色 / 显式授 ticket:list」三条来源成立、停用的人不在里面。
//
// 守卫与隔离沿用本泳道既有写法：MySQL 库名留空、PG 库名要有真值（AGENTS.md 第 3 节），
// 一次性库直接复用本泳道夹具的命名 nexadmin_tk_sla_<引擎>_<纳秒>（它自建自删），
// 句柄走 database.Open（生产构造器，装租户回调与 UTC 时区）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"

	"nexadmin/core/migrate"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// itAgentsLane 起一条带框架表的一次性库泳道，返回 gorm 句柄与租户 1 的上下文。
func itAgentsLane(t *testing.T, engine string) (*gorm.DB, context.Context) {
	t.Helper()
	if err := idgen.Init(itSLANode); err != nil {
		t.Fatal(err)
	}
	var gdb *gorm.DB
	switch engine {
	case "mysql":
		if os.Getenv("NEXADMIN_IT_MYSQL_DSN") == "" {
			t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL agents test")
		}
		gdb = itSLAMySQLLane(t, "mysql", false)
	case "postgres":
		if os.Getenv("NEXADMIN_IT_PG_DSN") == "" {
			t.Skip("NEXADMIN_IT_PG_DSN not set; skipping real-PostgreSQL agents test")
		}
		gdb = itSLAPGLane(t, false)
	default:
		t.Fatalf("未知引擎 %q", engine)
	}
	itAgentsRunFrameworkMigrations(t, gdb, engine)
	itAgentsSeed(t, gdb)
	return gdb, tenant.With(context.Background(), 1)
}

// itAgentsRunFrameworkMigrations 把 migrations/framework/<engine> 下所有 .sql 按文件名执行进去。
func itAgentsRunFrameworkMigrations(t *testing.T, gdb *gorm.DB, engine string) {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations", "framework", engine)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读框架迁移目录 %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("%s：框架迁移目录里一个 .sql 都没有，闸对空跑", engine)
	}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("读框架迁移 %s: %v", name, err)
		}
		// 逐条语句执行，不是整文件 Exec：MySQL 驱动默认不开 multiStatements，
		// 一个文件里 CREATE TABLE 紧跟 CREATE INDEX 就会被当成一条语句报 1064
		// （backup_001_init.sql 第一次跑就是这么撞的）。切分用生产 runner 同一个 migrate.Split。
		for _, stmt := range migrate.Split(string(raw)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if err := gdb.Exec(stmt).Error; err != nil {
				t.Fatalf("执行框架迁移 %s: %v\n%s", name, err, stmt)
			}
		}
	}
	t.Logf("%s: 框架迁移 %d 个文件执行完毕", engine, len(names))
}

// itAgentsSeed 造四个人：超管、内置角色者、显式授 ticket:list 者、有角色但没有任何授权菜单者，
// 外加一个「是超管但已停用」的，用来确认 status 谓词不是装饰。
func itAgentsSeed(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	// 引擎中立：列名一致由 TestPluginMigrationsConsistentAcrossEngines 与本仓的三引擎一致性闸保证；
	// 时间用 CURRENT_TIMESTAMP，布尔字面量两边都认。
	stmts := []string{
		`INSERT INTO sys_menu (id,parent_id,code,type,title,icon,path,component,perm,sort,keep_alive,hidden,external,status,created_at,updated_at)
		  VALUES (9001,0,'ticket','C','工单','','/ticket','', 'ticket:list',1,false,false,false,'1',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO sys_menu (id,parent_id,code,type,title,icon,path,component,perm,sort,keep_alive,hidden,external,status,created_at,updated_at)
		  VALUES (9002,0,'other','C','别的','','/other','', 'other:list',2,false,false,false,'1',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO sys_role (id,tenant_id,name,code,sort,status,data_scope,builtin,remark,created_at,updated_at)
		  VALUES (9101,1,'内置坐席','agent-builtin',1,'1','all',true,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO sys_role (id,tenant_id,name,code,sort,status,data_scope,builtin,remark,created_at,updated_at)
		  VALUES (9102,1,'授权坐席','agent-granted',2,'1','all',false,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO sys_role (id,tenant_id,name,code,sort,status,data_scope,builtin,remark,created_at,updated_at)
		  VALUES (9103,1,'空白角色','agent-none',3,'1','all',false,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		// 9102 被显式授 ticket:list（菜单 9001），9103 有菜单但 perm 对不上（菜单 9002 = other:list）——
		// 后者是「有授权记录却不是这个权限点」的反面样本，只靠「角色有没有菜单」判不出来的那格。
		`INSERT INTO sys_role_menu (role_id,menu_id) VALUES (9102,9001)`,
		`INSERT INTO sys_role_menu (role_id,menu_id) VALUES (9103,9002)`,
		itAgentsUser(8001, 1, "agents_super", "1", true),
		itAgentsUser(8002, 1, "agents_builtin", "1", false),
		itAgentsUser(8003, 1, "agents_granted", "1", false),
		itAgentsUser(8004, 1, "agents_none", "1", false),
		itAgentsUser(8005, 1, "agents_disabled", "2", true),
		// 别的租户的超管不该混进来（框架表的 tenant_id 谓词是真的）。
		itAgentsUser(8101, 2, "agents_other_tenant", "1", true),
		`INSERT INTO sys_user_role (user_id,role_id) VALUES (8002,9101)`,
		`INSERT INTO sys_user_role (user_id,role_id) VALUES (8003,9102)`,
		`INSERT INTO sys_user_role (user_id,role_id) VALUES (8004,9103)`,
	}
	for _, s := range stmts {
		if err := gdb.Exec(s).Error; err != nil {
			t.Fatalf("种测试数据失败: %v\n%s", err, s)
		}
	}
}

// itAgentsUser 生成一条 sys_user 插入：列名与值的个数由同一个模板对齐，
// 手写七条时 PG 报过「INSERT 的指定字段数多于表达式」，这类错只有程序化生成才不会犯。
func itAgentsUser(id int64, tenantID int, username, status string, super bool) string {
	return fmt.Sprintf(`INSERT INTO sys_user `+
		`(id,tenant_id,username,password,nickname,avatar,phone,email,gender,dept_id,status,province,city,district,remark,is_super,created_at,updated_at) `+
		`VALUES (%d,%d,'%s','x','%s','','','','',0,'%s','','','','',%t,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		id, tenantID, username, username, status, super)
}

func TestITQueryAgentsLive(t *testing.T) {
	t.Run("mysql", func(t *testing.T) { queryAgentsLiveCase(t, "mysql") })
	t.Run("postgres", func(t *testing.T) { queryAgentsLiveCase(t, "postgres") })
}

func queryAgentsLiveCase(t *testing.T, engine string) {
	gdb, ctx := itAgentsLane(t, engine)
	svc := NewService(gdb, nopLogger{})

	agents, err := svc.queryAgents(ctx, idgen.ID(1))
	if err != nil {
		t.Fatalf("queryAgents 返回错误（框架表已建齐，这条 SQL 必须真能执行）: %v", err)
	}
	got := make(map[int64]bool, len(agents))
	for _, a := range agents {
		if int64(a.TenantID) != 1 {
			t.Errorf("坐席集合里混进了 tenant_id=%d 的用户 %d：tid 谓词没生效", a.TenantID, a.UserID)
		}
		got[int64(a.UserID)] = true
	}
	// 三条来源都要在：8001 超管、8002 内置角色、8003 的角色被显式授了 ticket:list。
	for _, want := range []int64{8001, 8002, 8003} {
		if !got[want] {
			t.Errorf("坐席集合缺 %d（超管 / 内置角色 / 显式授 ticket:list 三条来源之一没进结果）：%v", want, itAgentIDList(got))
		}
	}
	// 三条不该在：8004 的角色只挂了 other:list、8005 是停用账号、8101 属于租户 2。
	for _, no := range []int64{8004, 8005, 8101} {
		if got[no] {
			t.Errorf("不该进来的用户 %d 出现在坐席集合里：%v", no, itAgentIDList(got))
		}
	}
	var rows int64
	if err := gdb.Table("sys_user").Where("id = ?", 8001).Count(&rows).Error; err != nil || rows != 1 {
		t.Fatalf("自检失败：种下的用户查不到（rows=%d err=%v）", rows, err)
	}
}

func itAgentIDList(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
