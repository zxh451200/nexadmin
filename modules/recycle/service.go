package recycle

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/modules/internal/recbin"
	"nexadmin/pkg/idgen"
)

// Service 是回收站的业务层。
type Service struct {
	db  *gorm.DB
	log *zap.Logger
}

// NewService 装配回收站服务（模块注册用；真库用例与二开装配同样走这里）。
func NewService(db *gorm.DB, log *zap.Logger) *Service { return &Service{db: db, log: log} }

// Item 与前端 RecycleItem 一致。
type Item struct {
	ID        idgen.ID  `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	DeletedBy string    `json:"deletedBy"`
	DeletedAt time.Time `json:"deletedAt"`
}

// ListFilter 是回收站列表的筛选：类型 / 关键词 / 删除时间范围（from/to 为 YYYY-MM-DD，含当天）。
type ListFilter struct {
	Type    string
	Keyword string
	From    string
	To      string
	Page    int
	Size    int
}

// List 按删除时间倒序。deleted_by 存的是操作人 ID，批量查昵称翻译成展示名
// （查不到——操作人也被删了——就回退原始值）。
func (s *Service) List(ctx context.Context, f ListFilter) ([]Item, int64, error) {
	tx := s.db.WithContext(ctx).Model(&recbin.Entry{})
	if f.Type != "" {
		tx = tx.Where("type = ?", f.Type)
	}
	if f.Keyword != "" {
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		tx = tx.Where("LOWER(name) LIKE LOWER(?)", "%"+r.Replace(f.Keyword)+"%")
	}
	// 时间范围：from/to 是日期字符串，先校验再换算成 UTC 当天边界，非法值当没有这个筛选。
	if f.From != "" {
		if from, err := time.ParseInLocation("2006-01-02", f.From, time.UTC); err == nil {
			tx = tx.Where("deleted_at >= ?", from)
		}
	}
	if f.To != "" {
		if to, err := time.ParseInLocation("2006-01-02", f.To, time.UTC); err == nil {
			tx = tx.Where("deleted_at < ?", to.AddDate(0, 0, 1))
		}
	}
	tx = tx.Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []recbin.Entry
	if err := tx.Order("deleted_at DESC").Offset((f.Page - 1) * f.Size).Limit(f.Size).Find(&rows).Error; err != nil {
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

func (s *Service) entry(ctx context.Context, id idgen.ID) (*recbin.Entry, string, error) {
	var e recbin.Entry
	if err := s.db.WithContext(ctx).First(&e, "id = ?", int64(id)).Error; err != nil {
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

// keyConflict 在还原前显式检查业务唯一键是否已被别的未删行占用。
//
// role / post / dict 的唯一索引在接软删除时降级成了普通索引（见 recbin.Keys 的说明），
// 数据库不会再抛重复键，不查就会还原出两条同码数据（同码角色 = 同一份权限码被两个角色带着，
// 同码字典项 = Items() 取到随机一条）。错误码沿用降级前的 recycle.restore_conflict，前端与批量回执的
// 计数字段都不用改。user 走 keyUserConflict（两把 OR 关系的键，Keys 表达不了，见 recbin.Keys 说明）；
// notice 不检查：数据库仍按它的索引决定冲突，撞码时由还原 UPDATE 抛重复键兜底。
func (s *Service) keyConflict(ctx context.Context, tx *gorm.DB, e *recbin.Entry, table string) error {
	if e.Type == recbin.TypeUser {
		return s.keyUserConflict(ctx, tx, e, table)
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

// Restore 还原：清掉源表的 deleted_at，并移出回收站。
// 原生 SQL 不经过租户 scope，所以租户级源表显式带 tenant_id（取回收记录自己的租户），
// 平台级源表按 srcScope 去掉这个谓词；撞业务唯一键由 keyConflict 在写之前查出来。
//
// 源行已不存在（RowsAffected==0）时：返回 404，并且仍然清掉这条回收记录。
// 取舍：只返回 404 而保留记录，用户会反复点到同一条永远失败的记录，列表无法自愈；
// 而返回成功会谎报“已还原”。源数据没了，记录本身没有任何还原 / 彻底删除的价值，所以两者都做：
// 在事务里删除记录并提交，事务外再返回 404（不能在事务回调里返回错误，否则清理会被一并回滚）。
func (s *Service) Restore(ctx context.Context, id idgen.ID) error {
	e, table, err := s.entry(ctx, id)
	if err != nil {
		return err
	}
	gone := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.keyConflict(ctx, tx, e, table); err != nil {
			return err
		}
		where, args := srcScope(e)
		// 表名来自 recbin.Tables 白名单；条件值全部走参数绑定。
		res := tx.Table(table).Where(where, args...).Update("deleted_at", nil)
		if res.Error != nil {
			// 仍带唯一索引的源表（user / notice）重复键会从这里抛出
			return response.Conflict("recycle.restore_conflict").Wrap(res.Error)
		}
		gone = res.RowsAffected == 0
		return tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	})
	if err != nil {
		return err
	}
	if gone {
		return errSourceGone()
	}
	return nil
}

// Purge 彻底删除：真正删掉源表的行（只删仍处于软删除状态的），并移出回收站。
// 源行已不存在（RowsAffected==0）时与 Restore 同样处理：清掉回收记录并提交，再返回 404（取舍见 Restore）。
func (s *Service) Purge(ctx context.Context, id idgen.ID) error {
	e, table, err := s.entry(ctx, id)
	if err != nil {
		return err
	}
	gone := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ref := int64(e.RefID)
		// 关联表按 recbin.Children 一并物理清掉：软删除时保留它们（还原才是完整的），
		// 彻底删除时必须清，否则留下指向已不存在之行的关联（公告的发布对象与已读、
		// 角色的菜单/数据范围/用户关联、岗位的用户关联、字典类型下的条目——含条目自己已被软删的）。
		for _, c := range recbin.Children[e.Type] {
			if err := tx.Table(c.Table).Where(c.Column+" = ?", ref).Delete(nil).Error; err != nil {
				return err
			}
		}
		where, args := srcScope(e)
		// 表名来自 recbin.Tables 白名单；条件值全部走参数绑定。
		res := tx.Table(table).Where(where, args...).Delete(nil)
		if res.Error != nil {
			return res.Error
		}
		gone = res.RowsAffected == 0
		return tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	})
	if err != nil {
		return err
	}
	if gone {
		return errSourceGone()
	}
	return nil
}

// BatchResult 是批量还原 / 批量彻底删除的回执。
// 批量动作不因个别条目失败而整体失败：冲突与源行消失如实计数，让界面刷新后自见分晓。
type BatchResult struct {
	Restored int64 `json:"restored,omitempty"`
	Purged   int64 `json:"purged,omitempty"`
	Conflict int64 `json:"conflict,omitempty"`
	Gone     int64 `json:"gone,omitempty"`
}

// RestoreBatch 批量还原。
func (s *Service) RestoreBatch(ctx context.Context, ids []idgen.ID) (*BatchResult, error) {
	res := &BatchResult{}
	for _, id := range ids {
		err := s.Restore(ctx, id)
		switch {
		case err == nil:
			res.Restored++
		case isConflict(err):
			res.Conflict++
		case isNotFoundErr(err):
			res.Gone++
		default:
			return nil, err // 非预期错误（库故障）立刻失败，不静默吞掉
		}
	}
	return res, nil
}

// PurgeBatch 批量彻底删除。
func (s *Service) PurgeBatch(ctx context.Context, ids []idgen.ID) (*BatchResult, error) {
	res := &BatchResult{}
	for _, id := range ids {
		err := s.Purge(ctx, id)
		switch {
		case err == nil:
			res.Purged++
		case isNotFoundErr(err):
			res.Gone++
		default:
			return nil, err
		}
	}
	return res, nil
}

func isConflict(err error) bool {
	var be *response.Error
	return errors.As(err, &be) && be.Status == 409
}

func isNotFoundErr(err error) bool {
	var be *response.Error
	return errors.As(err, &be) && be.Status == 404
}
