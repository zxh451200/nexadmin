package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/setup"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 这些方法让用户模块成为初始化向导的 Migrator / Seeder / AdminCreator / AdminChecker。

// 出厂 ID（与前端 mock 一致）。
const (
	adminUserID idgen.ID = 1800000000000000001
	roleAdmin   idgen.ID = 2100000000000000001
	roleOper    idgen.ID = 2100000000000000002
	roleAudit   idgen.ID = 2100000000000000003
	roleGuest   idgen.ID = 2100000000000000004
	roleEditor  idgen.ID = 2100000000000000005

	deptHQ  idgen.ID = 1860000000000000001
	deptRD  idgen.ID = 1860000000000000002
	deptQA  idgen.ID = 1860000000000000008
	deptOps idgen.ID = 1860000000000000004
	deptFin idgen.ID = 1860000000000000005
)

// skipTenant 返回一个跳过租户隔离的会话；种子与建管理员都显式写 tenant_id。
func skipTenant(db *gorm.DB) *gorm.DB {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return db.WithContext(tenant.Skip(ctx))
}

// HasAdmin 判断库里是否已有超级管理员（用于判断系统是否已初始化）。
func (*module) HasAdmin(db *gorm.DB) (bool, error) {
	var n int64
	err := skipTenant(db).Model(&User{}).Where("is_super = ?", true).Count(&n).Error
	return n > 0, err
}

// CreateAdmin 创建向导里填写的超级管理员，并挂上内置“超级管理员”角色。可重复执行：
// 同名超管已存在则只更新密码；同名普通用户已存在则报错，不会悄悄提权。
func (*module) CreateAdmin(ctx context.Context, db *gorm.DB, a setup.Admin) error {
	db = db.WithContext(tenant.Skip(ctx))
	hash, err := auth.HashPassword(a.Password)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var u User
		err := tx.Unscoped().Where("tenant_id = ? AND username = ?", int64(tenant.DefaultID), a.Username).First(&u).Error
		switch {
		case err == nil && !u.IsSuper:
			return fmt.Errorf("user %q already exists and is not an administrator", a.Username)
		case err == nil:
			return tx.Unscoped().Model(&User{}).Where("id = ?", int64(u.ID)).
				Updates(map[string]any{"password": hash, "status": statusOn, "deleted_at": nil}).Error
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		id := adminUserID
		var n int64
		if err := tx.Unscoped().Model(&User{}).Where("id = ?", int64(id)).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			id = 0 // 出厂 ID 已被占用，交给雪花生成
		}
		u = User{Username: a.Username, Password: hash, Nickname: a.Username, Gender: "0", Status: statusOn, IsSuper: true, Remark: "系统内置管理员"}
		u.ID, u.TenantID = id, tenant.DefaultID
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		var rid int64
		if err := tx.Table("sys_role").Select("id").Where("tenant_id = ? AND code = ?", int64(tenant.DefaultID), "admin").Limit(1).Scan(&rid).Error; err != nil {
			return err
		}
		if rid == 0 {
			return nil // 角色种子缺失时不阻断初始化；超管靠 is_super 本身就拥有全部权限
		}
		return tx.Create(&userRole{UserID: u.ID, RoleID: idgen.ID(rid)}).Error
	})
}

type seedUser struct {
	idx                      int64
	username, nickname       string
	phone, email, gender     string
	dept                     idgen.ID
	roles                    []idgen.ID
	status                   string
	province, city, district string
	created                  string
}

// 出厂演示用户（与前端 user.mock.ts 的 2-18 号一致；1 号管理员由向导创建）。
// 密码为空 = 不可登录，管理员在后台“重置密码”后才能使用，避免内置已知口令。
var (
	bj = [3]string{"110000", "110100", "110105"}
	sh = [3]string{"310000", "310100", "310104"}
	sz = [3]string{"440000", "440300", "440305"}
	hz = [3]string{"330000", "330100", "330106"}
)

func su(idx int64, username, nickname, phone, email, gender string, dept idgen.ID, roles []idgen.ID, status string, r [3]string, created string) seedUser {
	return seedUser{idx, username, nickname, phone, email, gender, dept, roles, status, r[0], r[1], r[2], created}
}

func factoryUsers() []seedUser {
	return []seedUser{
		su(2, "zhangwei", "张伟", "13800138002", "zhangwei@nexadmin.dev", "1", deptRD, []idgen.ID{roleOper}, "1", bj, "2026-01-10"),
		su(3, "lixiaoling", "李晓玲", "13900139003", "lixl@nexadmin.dev", "2", deptOps, []idgen.ID{roleOper}, "1", sh, "2026-01-14"),
		su(4, "wangfang", "王芳", "13700137004", "wangfang@nexadmin.dev", "2", deptFin, []idgen.ID{roleAudit}, "1", hz, "2026-01-21"),
		su(5, "chenjie", "陈杰", "13600136005", "chenjie@nexadmin.dev", "1", deptRD, []idgen.ID{roleEditor}, "0", sz, "2026-02-02"),
		su(6, "emma.wilson", "Emma Wilson", "13500135006", "emma.wilson@example.com", "2", deptOps, []idgen.ID{roleOper, roleEditor}, "1", sh, "2026-02-09"),
		su(7, "liuyang", "刘洋", "13400134007", "liuyang@nexadmin.dev", "1", deptQA, []idgen.ID{roleAudit}, "1", bj, "2026-02-17"),
		su(8, "james.smith", "James Smith", "13300133008", "james.smith@example.com", "1", deptHQ, []idgen.ID{roleGuest}, "0", sz, "2026-02-25"),
		su(9, "zhaomin", "赵敏", "13200132009", "zhaomin@nexadmin.dev", "2", deptFin, []idgen.ID{roleGuest}, "1", hz, "2026-03-03"),
		su(10, "sunhao", "孙浩", "15800158010", "sunhao@nexadmin.dev", "1", deptRD, []idgen.ID{roleOper}, "1", bj, "2026-03-11"),
		su(11, "zhouxin", "周欣", "15900159011", "zhouxin@nexadmin.dev", "2", deptQA, []idgen.ID{roleAudit, roleGuest}, "1", sh, "2026-03-19"),
		su(12, "wuqiang", "吴强", "15700157012", "wuqiang@nexadmin.dev", "1", deptOps, []idgen.ID{roleEditor}, "0", sz, "2026-03-28"),
		su(13, "olivia.brown", "Olivia Brown", "15600156013", "olivia.brown@example.com", "2", deptHQ, []idgen.ID{roleGuest}, "1", sh, "2026-04-06"),
		su(14, "zhengtao", "郑涛", "15500155014", "zhengtao@nexadmin.dev", "1", deptRD, []idgen.ID{roleOper}, "1", hz, "2026-04-15"),
		su(15, "mayu", "马玉", "15300153015", "mayu@nexadmin.dev", "0", deptFin, []idgen.ID{roleAudit}, "1", bj, "2026-04-22"),
		su(16, "huangbo", "黄波", "18800188016", "huangbo@nexadmin.dev", "1", deptQA, []idgen.ID{roleGuest}, "1", sz, "2026-05-04"),
		su(17, "linna", "林娜", "18900189017", "linna@nexadmin.dev", "2", deptOps, []idgen.ID{roleOper}, "1", sh, "2026-05-13"),
		su(18, "noah.davis", "Noah Davis", "18700187018", "noah.davis@example.com", "1", deptRD, []idgen.ID{roleEditor, roleGuest}, "0", hz, "2026-05-30"),
	}
}

// Seed 幂等种入演示用户：按（默认租户, 账号）判断，包括已删除的，已存在一律不动。
func (*module) Seed(db *gorm.DB) error {
	return skipTenant(db).Transaction(func(tx *gorm.DB) error {
		for _, s := range factoryUsers() {
			var n int64
			if err := tx.Unscoped().Model(&User{}).Where("tenant_id = ? AND username = ?", int64(tenant.DefaultID), s.username).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			at, _ := time.Parse("2006-01-02", s.created)
			at = at.Add(9 * time.Hour)
			u := &User{
				Username: s.username, Nickname: s.nickname, Phone: s.phone, Email: s.email, Gender: s.gender,
				DeptID: s.dept, Status: s.status, Province: s.province, City: s.city, District: s.district,
			}
			u.ID, u.TenantID, u.CreatedAt, u.UpdatedAt = idgen.ID(1800000000000000000+s.idx), tenant.DefaultID, at, at
			if err := tx.Create(u).Error; err != nil {
				return err
			}
			if err := replaceRelations(tx, u.ID, s.roles, nil); err != nil {
				return err
			}
		}
		return nil
	})
}
