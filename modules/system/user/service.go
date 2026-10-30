package user

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/database"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type service struct {
	db     *gorm.DB
	cache  cache.Cache
	notify *notify.Service // 可为空：账号通知邮件（设置页开关，默认关）
}

func bad(key string, args ...any) *response.Error { return response.BadRequest(key, args...) }

func notFound() *response.Error { return response.NotFound("error.not_found") }

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ensureUserQuota 在新增 adding 个用户前检查套餐用户数上限（超出返回 403 + 明确 i18n key）。
// 没有额度限制（0 = 不限、默认租户、平台级操作）时不加锁也不做统计查询；有限制时必须在 tx 事务内调用：
// 先对租户行 SELECT ... FOR UPDATE，再统计（含停用账号，不含已删除），使同一租户的并发创建被串行化、不会超出 maxUsers。
func ensureUserQuota(ctx context.Context, tx *gorm.DB, adding int) error {
	info, err := tenant.QuotaInfo(ctx)
	if err != nil || info == nil || info.MaxUsers <= 0 {
		return err
	}
	if err := database.LockTenantRow(ctx, tx); err != nil {
		return err
	}
	var n int64
	if err := tx.WithContext(ctx).Model(&User{}).Count(&n).Error; err != nil {
		return err
	}
	return tenant.CheckUserQuota(ctx, n, adding)
}

func tenantOf(ctx context.Context) idgen.ID {
	id, _ := tenant.From(ctx)
	return id
}

// filtered 组装列表/导出的筛选条件（含当前操作者的数据范围）。返回的查询可重复使用（先 Count 再 Find）。
func (s *service) filtered(ctx context.Context, q listQuery, me actor) (*gorm.DB, error) {
	tx := s.db.WithContext(ctx).Model(&User{})
	if kw := strings.ToLower(strings.TrimSpace(q.Keyword)); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		tx = tx.Where("(LOWER(username) LIKE ? OR LOWER(nickname) LIKE ? OR LOWER(phone) LIKE ? OR LOWER(email) LIKE ?)", like, like, like, like)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.Gender != "" {
		tx = tx.Where("gender = ?", q.Gender)
	}
	if q.DeptID != "" {
		id, err := idgen.Parse(q.DeptID)
		if err != nil {
			return nil, bad("user.dept_id_invalid")
		}
		tx = tx.Where("dept_id = ?", int64(id))
	}
	if q.CreatedFrom != "" {
		t, err := time.Parse("2006-01-02", q.CreatedFrom)
		if err != nil {
			return nil, bad("user.created_at_invalid")
		}
		tx = tx.Where("created_at >= ?", t)
	}
	if q.CreatedTo != "" {
		t, err := time.Parse("2006-01-02", q.CreatedTo)
		if err != nil {
			return nil, bad("user.created_at_invalid")
		}
		tx = tx.Where("created_at < ?", t.AddDate(0, 0, 1))
	}
	cond, args, limited, err := s.scopeCond(ctx, me)
	if err != nil {
		return nil, err
	}
	if limited {
		tx = tx.Where(cond, args...)
	}
	return tx.Session(&gorm.Session{}), nil
}

func (s *service) list(ctx context.Context, q listQuery, me actor) ([]*userDTO, int64, error) {
	tx, err := s.filtered(ctx, q, me)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []User
	if err := tx.Order("created_at DESC, id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out, err := s.toDTOs(ctx, rows)
	return out, total, err
}

// exportLimit 是一次导出的行数上限，防止拖垮服务。
const exportLimit = 10000

func (s *service) export(ctx context.Context, q listQuery, me actor) ([]*userDTO, error) {
	tx, err := s.filtered(ctx, q, me)
	if err != nil {
		return nil, err
	}
	var rows []User
	if err := tx.Order("created_at DESC, id DESC").Limit(exportLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.toDTOs(ctx, rows)
}

func (s *service) find(ctx context.Context, id idgen.ID) (*User, error) {
	var u User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	return &u, nil
}

func (s *service) get(ctx context.Context, id idgen.ID) (*userDTO, error) {
	u, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	out, err := s.toDTOs(ctx, []User{*u})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// toDTOs 批量补全部门名、角色、岗位和锁定状态，避免逐行查询。
func (s *service) toDTOs(ctx context.Context, rows []User) ([]*userDTO, error) {
	out := make([]*userDTO, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int64, len(rows))
	var deptIDs []int64
	for i := range rows {
		ids[i] = int64(rows[i].ID)
		if rows[i].DeptID != 0 {
			deptIDs = append(deptIDs, int64(rows[i].DeptID))
		}
	}
	db := s.db.WithContext(ctx)

	var roleRows []struct {
		UserID idgen.ID
		RoleID idgen.ID
		Name   string
	}
	if err := db.Table("sys_user_role AS ur").Select("ur.user_id AS user_id, ur.role_id AS role_id, r.name AS name").
		Joins("JOIN sys_role r ON r.id = ur.role_id").Where("ur.user_id IN ?", ids).
		Order("r.sort ASC, r.id ASC").Scan(&roleRows).Error; err != nil {
		return nil, err
	}
	var postRows []userPost
	if err := db.Where("user_id IN ?", ids).Find(&postRows).Error; err != nil {
		return nil, err
	}
	// 部门表属于部门模块；该模块停用或还没建表时只是拿不到名称，不影响用户列表。
	deptName := map[idgen.ID]string{}
	if len(deptIDs) > 0 {
		var depts []struct {
			ID   idgen.ID
			Name string
		}
		if err := db.Table("sys_dept").Select("id, name").Where("id IN ?", deptIDs).Scan(&depts).Error; err == nil {
			for _, d := range depts {
				deptName[d.ID] = d.Name
			}
		}
	}

	roleIDs, roleNames := map[idgen.ID][]idgen.ID{}, map[idgen.ID][]string{}
	for _, r := range roleRows {
		roleIDs[r.UserID] = append(roleIDs[r.UserID], r.RoleID)
		roleNames[r.UserID] = append(roleNames[r.UserID], r.Name)
	}
	postIDs := map[idgen.ID][]idgen.ID{}
	for _, p := range postRows {
		postIDs[p.UserID] = append(postIDs[p.UserID], p.PostID)
	}

	for i := range rows {
		u := &rows[i]
		d := &userDTO{
			ExternalID: u.ExternalID,
			ID:         u.ID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar, Phone: u.Phone, Email: u.Email,
			Gender: u.Gender, DeptName: deptName[u.DeptID], PostIDs: postIDs[u.ID], RoleIDs: roleIDs[u.ID],
			RoleNames: roleNames[u.ID], Status: u.Status, Remark: u.Remark, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
			Region: regionDTO{Province: u.Province, City: u.City, District: u.District},
		}
		if u.DeptID != 0 {
			d.DeptID = u.DeptID.String()
		}
		if d.PostIDs == nil {
			d.PostIDs = []idgen.ID{}
		}
		if d.RoleIDs == nil {
			d.RoleIDs, d.RoleNames = []idgen.ID{}, []string{}
		}
		until, err := lockedUntil(ctx, s.cache, u.Username)
		if err != nil {
			return nil, err
		}
		if !until.IsZero() {
			d.Locked, d.LockedUntil = true, &until
		}
		out[i] = d
	}
	return out, nil
}
