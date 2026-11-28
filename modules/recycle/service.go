package recycle

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/recbin"
)

// Service 是回收站的业务层。
type Service struct {
	db            *gorm.DB
	log           *zap.Logger
	loc           *time.Location
	retentionDays int
}

// NewService 装配回收站服务（模块注册用；真库用例与二开装配同样走这里）。
func NewService(db *gorm.DB, log *zap.Logger, tz string, retentionDays int) *Service {
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.UTC
	}
	return &Service{db: db, log: log, loc: loc, retentionDays: retentionDays}
}

// Item 与前端 RecycleItem 一致。
type Item struct {
	ID        idgen.ID  `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	DeletedBy string    `json:"deletedBy"`
	DeletedAt time.Time `json:"deletedAt"`
}

// ListFilter 是回收站列表的筛选：类型 / 关键词 / 删除时间范围（from/to 为 YYYY-MM-DD，含当天）。
// Type 逗号拼接多值（?type=user,role），空 = 不限。
type ListFilter struct {
	Type    []string
	Keyword string
	From    string
	To      string
	Page    int
	Size    int
	Sort    string // 表头排序字段（白名单见 Service.List）
	Desc    bool
}

// List 按删除时间倒序。deleted_by 存的是操作人 ID，批量查昵称翻译成展示名
// （查不到——操作人也被删了——就回退原始值）。
func (s *Service) List(ctx context.Context, f ListFilter) ([]Item, int64, error) {
	tx := s.db.WithContext(ctx).Model(&recbin.Entry{})
	if len(f.Type) > 0 {
		tx = tx.Where("type IN ?", f.Type)
	}
	if f.Keyword != "" {
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		tx = tx.Where("LOWER(name) LIKE LOWER(?)", "%"+r.Replace(f.Keyword)+"%")
	}
	// 时间范围：日期按站点时区解释，再换算成 UTC 比较（删除时间落库是 UTC）。
	if f.From != "" {
		if from, err := time.ParseInLocation("2006-01-02", f.From, s.loc); err == nil {
			tx = tx.Where("deleted_at >= ?", from.UTC())
		}
	}
	if f.To != "" {
		if to, err := time.ParseInLocation("2006-01-02", f.To, s.loc); err == nil {
			tx = tx.Where("deleted_at < ?", to.AddDate(0, 0, 1).UTC())
		}
	}
	tx = tx.Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []recbin.Entry
	// 排序白名单：名称/类型/删除时间，白名单外回退 deleted_at
	if err := tx.Order(core.OrderBy(f.Sort, f.Desc, map[string]string{
		"name":       "name",
		"type":       "type",
		"deleted_at": "deleted_at",
	}, "deleted_at")).Offset((f.Page - 1) * f.Size).Limit(f.Size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	names := s.userNames(ctx, rows)
	out := make([]Item, len(rows))
	for i, e := range rows {
		deletedBy := e.DeletedBy
		if nick, ok := names[e.DeletedBy]; ok && nick != "" {
			deletedBy = nick
		}
		out[i] = Item{ID: e.ID, Type: e.Type, Name: e.Name, DeletedBy: deletedBy, DeletedAt: e.DeletedAt}
	}
	return out, total, nil
}

// userNames 批量把「删除人 ID 字符串」翻译成昵称；查询失败静默回退原始值（列表不能因昵称挂掉）。
func (s *Service) userNames(ctx context.Context, rows []recbin.Entry) map[string]string {
	ids := make([]int64, 0, len(rows))
	seen := map[int64]bool{}
	for _, e := range rows {
		id, err := strconv.ParseInt(e.DeletedBy, 10, 64)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	var users []struct {
		ID       idgen.ID `gorm:"column:id"`
		Nickname string   `gorm:"column:nickname"`
	}
	if err := s.db.WithContext(tenant.Skip(ctx)).Table("sys_user").
		Select("id", "nickname").Where("id IN ?", ids).Scan(&users).Error; err != nil {
		return out
	}
	for _, u := range users {
		out[idgen.ID(u.ID).String()] = u.Nickname
	}
	return out
}

func (s *Service) entry(ctx context.Context, db *gorm.DB, id idgen.ID) (*recbin.Entry, string, error) {
	var e recbin.Entry
	if err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", response.NotFound("error.not_found")
		}
		return nil, "", err
	}
	table, ok := recbin.Tables[e.Type]
	if !ok {
		return nil, "", response.BadRequest("recycle.type_unsupported")
	}
	return &e, table, nil
}

// errSourceGone 表示回收记录指向的源行已不存在（被清掉或已不处于软删除状态）。
func errSourceGone() error { return response.NotFound("recycle.source_gone") }

// srcScope 是定位源行的 WHERE 条件与参数。
//
// guardPlatform 挡住「从回收站改平台级源表」这条第二入口：字典类型/字典项这类表没有 tenant_id
// （recbin.Platform），回收站里的可见性虽然按租户算，但还原与彻底删除动的是全局那一行。
// 字典自己的写路由已经只认平台调用方（modules/system/dict/module.go），这里不补，
// 租户就能借 recycle:restore / recycle:delete 绕过那道闸。
func guardPlatform(e *recbin.Entry, platform bool) error {
	if recbin.Platform[e.Type] && !platform {
		return response.Forbidden("error.platform_only")
	}
	return nil
}

// 平台级源表（recbin.Platform：字典这类没有 tenant_id 的表）不能带租户谓词——那一列根本不存在，
// 带上就是 SQL 直接报错；回收记录自己的 tenant_id 只用于「在谁的回收站里可见」。
// 两种情况都必须带 deleted_at IS NOT NULL：不依赖各引擎对“值未变化”的 RowsAffected 口径差异
// （MySQL 默认不计），也不会误动未被删除的行。表名来自 recbin.Tables 白名单，值全部参数绑定。
func srcScope(e *recbin.Entry) (string, []any) {
	ref := int64(e.RefID)
	if recbin.Platform[e.Type] {
		return "id = ? AND deleted_at IS NOT NULL", []any{ref}
	}
	return "id = ? AND tenant_id = ? AND deleted_at IS NOT NULL", []any{ref, int64(e.TenantID)}
}

// lockSource 锁定仍处于软删除状态的源行，保证还原与彻底删除在同一源行上串行。
func (s *Service) lockSource(ctx context.Context, tx *gorm.DB, e *recbin.Entry, table string) (bool, error) {
	where, args := srcScope(e)
	var rows []struct {
		ID idgen.ID `gorm:"column:id"`
	}
	if err := tx.WithContext(ctx).Table(table).Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(where, args...).Find(&rows).Error; err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// keyConflict 在还原前显式检查业务唯一键是否已被别的未删行占用。
//
// role / post / dict 的唯一索引在接软删除时降级成了普通索引（见 recbin.Keys 的说明），
// 数据库不会再抛重复键，不查就会还原出两条同码数据（同码角色 = 同一份权限码被两个角色带着，
// 同码字典项 = Items() 取到随机一条）。错误码沿用降级前的 recycle.restore_conflict，前端与批量回执的
// 计数字段都不用改。user 走 keyUserConflict（两把 OR 关系的键，Keys 表达不了，见 recbin.Keys 说明）；
// notice 不检查：数据库仍按它的索引决定冲突，撞码时由还原 UPDATE 抛重复键兜底。
func (s *Service) keyConflict(ctx context.Context, tx *gorm.DB, e *recbin.Entry, table string) error {
	switch e.Type {
	case recbin.TypeUser:
		return s.keyUserConflict(ctx, tx, e, table)
	case recbin.TypeCustomer:
		return s.keyCustomerConflict(ctx, tx, e, table)
	}
	cols := recbin.Keys[e.Type]
	if len(cols) == 0 {
		return nil
	}
	var src []map[string]any
	// 列名来自 recbin.Keys 常量（不是请求），表名来自 recbin.Tables 白名单。
	if err := tx.Table(table).Select(strings.Join(cols, ", ")).
		Where("id = ?", int64(e.RefID)).Scan(&src).Error; err != nil {
		return err
	}
	if len(src) == 0 {
		return nil // 源行已被物理清掉：交给后面的 RowsAffected==0 → 404，别在这里谎报冲突
	}
	dup := tx.Table(table).Where("deleted_at IS NULL AND id <> ?", int64(e.RefID))
	for _, c := range cols {
		v, ok := src[0][c]
		if !ok {
			return nil // 拿不到键值列（白名单与表结构不一致）：放行比误判成冲突更安全，交由源行状态判定
		}
		dup = dup.Where(c+" = ?", v)
	}
	var n int64
	if err := dup.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("recycle.restore_conflict")
	}
	return nil
}

// keyUserConflict 是 user 的还原撞码判定：用户名与手机号是两把各自独立的键（OR 关系），
// 撞任意一把都会造出两条「同名 / 同手机号」的未删账号——同手机号会让 phoneLogin / 忘记密码
// 用 Limit(2) 命中两行而当场拒登（见 user/phone.go、user/forgot.go）。sys_user 上的
// idx_sys_user_tenant_username 在三种引擎里都是普通索引（非唯一），数据库挡不住，必须在应用层查。
//
// 手机号可空：无手机号的用户之间不该互相误判，所以只在源行手机号非空时才把 phone 纳入 OR 条件。
// 键的隔离范围跟建号查重（user.usernameTaken 走租户 scope）一致：同一 tenant_id 下的未删行。
// 表名来自 recbin.Tables 白名单；列名固定字面量，值全部参数绑定。
func (s *Service) keyUserConflict(ctx context.Context, tx *gorm.DB, e *recbin.Entry, table string) error {
	var src []struct {
		Username string `gorm:"column:username"`
		Phone    string `gorm:"column:phone"`
	}
	if err := tx.Table(table).Select("username, phone").
		Where("id = ?", int64(e.RefID)).Scan(&src).Error; err != nil {
		return err
	}
	if len(src) == 0 {
		return nil // 源行已被物理清掉：交给后面的 RowsAffected==0 → 404，别在这里谎报冲突
	}
	dup := tx.Table(table).
		Where("deleted_at IS NULL AND id <> ? AND tenant_id = ? AND username = ?", int64(e.RefID), int64(e.TenantID), src[0].Username)
	if p := src[0].Phone; p != "" {
		dup = dup.Or("deleted_at IS NULL AND id <> ? AND tenant_id = ? AND phone = ?", int64(e.RefID), int64(e.TenantID), p)
	}
	var n int64
	if err := dup.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("recycle.restore_conflict")
	}
	return nil
}

func (s *Service) keyCustomerConflict(ctx context.Context, tx *gorm.DB, e *recbin.Entry, table string) error {
	var src []struct {
		Type  string `gorm:"column:type"`
		Phone string `gorm:"column:phone"`
		TaxNo string `gorm:"column:tax_no"`
	}
	if err := tx.WithContext(ctx).Table(table).Select("type", "phone", "tax_no").
		Where("id = ?", int64(e.RefID)).Scan(&src).Error; err != nil {
		return err
	}
	if len(src) == 0 {
		return nil
	}
	dup := tx.WithContext(ctx).Table(table).
		Where("deleted_at IS NULL AND id <> ? AND tenant_id = ?", int64(e.RefID), int64(e.TenantID))
	switch {
	case src[0].Type == "person" && src[0].Phone != "":
		dup = dup.Where("phone = ?", src[0].Phone)
	case src[0].Type == "company" && src[0].TaxNo != "":
		dup = dup.Where("tax_no = ?", src[0].TaxNo)
	default:
		return nil
	}
	var n int64
	if err := dup.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("recycle.restore_conflict")
	}
	return nil
}

// Restore 还原：清掉源表的 deleted_at，并移出回收站。
// 源行已不存在时清理回收记录并提交，再返回 404，避免留下永远无法还原的记录。
func (s *Service) Restore(ctx context.Context, id idgen.ID, platform bool) error {
	gone := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		gone, err = s.restore(ctx, tx, id, platform)
		return err
	})
	if err != nil {
		return err
	}
	if gone {
		return errSourceGone()
	}
	return nil
}

func (s *Service) restore(ctx context.Context, tx *gorm.DB, id idgen.ID, platform bool) (bool, error) {
	e, table, err := s.entry(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if err := guardPlatform(e, platform); err != nil {
		return false, err
	}
	exists, err := s.lockSource(ctx, tx, e, table)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	}
	if err := s.keyConflict(ctx, tx, e, table); err != nil {
		return false, err
	}
	where, args := srcScope(e)
	res := tx.WithContext(ctx).Table(table).Where(where, args...).Update("deleted_at", nil)
	if res.Error != nil {
		if isDuplicateKeyErr(res.Error) {
			return false, response.Conflict("recycle.restore_conflict").Wrap(res.Error)
		}
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return true, tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	}
	return false, tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
}

// Purge 彻底删除：真正删掉源表的行，并移出回收站。
func (s *Service) Purge(ctx context.Context, id idgen.ID, platform bool) error {
	gone := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		gone, err = s.purge(ctx, tx, id, platform)
		return err
	})
	if err != nil {
		return err
	}
	if gone {
		return errSourceGone()
	}
	return nil
}

func (s *Service) purge(ctx context.Context, tx *gorm.DB, id idgen.ID, platform bool) (bool, error) {
	e, table, err := s.entry(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if err := guardPlatform(e, platform); err != nil {
		return false, err
	}
	exists, err := s.lockSource(ctx, tx, e, table)
	if err != nil {
		return false, err
	}
	if !exists {
		return true, tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	}
	ref := int64(e.RefID)
	for _, c := range recbin.Children[e.Type] {
		if err := tx.WithContext(ctx).Table(c.Table).Where(c.Column+" = ?", ref).Delete(nil).Error; err != nil {
			return false, err
		}
	}
	where, args := srcScope(e)
	res := tx.WithContext(ctx).Table(table).Where(where, args...).Delete(nil)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return true, tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	}
	return false, tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
}

// BatchResult 是批量还原 / 批量彻底删除的回执。
type BatchResult struct {
	Restored int64             `json:"restored,omitempty"`
	Purged   int64             `json:"purged,omitempty"`
	Conflict int64             `json:"conflict,omitempty"`
	Gone     int64             `json:"gone,omitempty"`
	Items    []BatchItemResult `json:"items,omitempty"`
}

type BatchItemResult struct {
	ID     idgen.ID `json:"id"`
	Result string   `json:"result"`
}

func (r *BatchResult) add(id idgen.ID, result string) {
	r.Items = append(r.Items, BatchItemResult{ID: id, Result: result})
	switch result {
	case "restored":
		r.Restored++
	case "purged":
		r.Purged++
	case "conflict":
		r.Conflict++
	case "gone":
		r.Gone++
	}
}

// RestoreBatch 批量还原；预期冲突按条计数，未预期错误回滚整批。
func (s *Service) RestoreBatch(ctx context.Context, ids []idgen.ID, platform bool) (*BatchResult, error) {
	res := &BatchResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			var gone bool
			// PostgreSQL 的语句错误会中止整个事务，预期冲突须先回滚到保存点才能继续。
			err := tx.Transaction(func(itemTx *gorm.DB) error {
				var err error
				gone, err = s.restore(ctx, itemTx, id, platform)
				return err
			})
			switch {
			case err == nil && gone:
				res.add(id, "gone")
			case err == nil:
				res.add(id, "restored")
			case isConflict(err):
				res.add(id, "conflict")
			case isNotFoundErr(err):
				res.add(id, "gone")
			case isForbiddenErr(err):
				res.add(id, "forbidden")
			default:
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// PurgeBatch 批量彻底删除；未预期错误回滚整批。
func (s *Service) PurgeBatch(ctx context.Context, ids []idgen.ID, platform bool) (*BatchResult, error) {
	res := &BatchResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			var gone bool
			err := tx.Transaction(func(itemTx *gorm.DB) error {
				var err error
				gone, err = s.purge(ctx, itemTx, id, platform)
				return err
			})
			switch {
			case err == nil && gone:
				res.add(id, "gone")
			case err == nil:
				res.add(id, "purged")
			case isNotFoundErr(err):
				res.add(id, "gone")
			case isForbiddenErr(err):
				res.add(id, "forbidden")
			default:
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func isDuplicateKeyErr(err error) bool {
	var mysqlErr *mysqldrv.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForbiddenErr(err error) bool {
	var be *response.Error
	return errors.As(err, &be) && be.Status == 403
}

func isConflict(err error) bool {
	var be *response.Error
	return errors.As(err, &be) && be.Status == 409
}

func isNotFoundErr(err error) bool {
	var be *response.Error
	return errors.As(err, &be) && be.Status == 404
}
