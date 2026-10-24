package report

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

// 读取内置表只用这两个名字常量，且只读 id 列；读不到一律按「无权限」处理，绝不 panic、不放行。
const (
	roleTable     = "sys_role"
	userRoleTable = "sys_user_role"
)

// ScreenForm 是创建/更新大屏的请求体。RoleIDs 为空表示只有管理者（超管 / report:screen）可见。
type ScreenForm struct {
	Name    string       `json:"name"`
	Remark  string       `json:"remark"`
	Config  ScreenConfig `json:"config"`
	RoleIDs []string     `json:"roleIds"`
}

// ScreenView 是大屏的返回形状。
type ScreenView struct {
	ID        idgen.ID     `json:"id"`
	Name      string       `json:"name"`
	Remark    string       `json:"remark"`
	Config    ScreenConfig `json:"config"`
	RoleIDs   []idgen.ID   `json:"roleIds,omitempty"`
	Charts    []ChartView  `json:"charts,omitempty"`
	CreatedAt string       `json:"createdAt"`
	UpdatedAt string       `json:"updatedAt"`
}

// RoleOption 是授权用的角色下拉项。
type RoleOption struct {
	ID   idgen.ID `json:"id"`
	Name string   `json:"name"`
}

func screenView(r Screen) ScreenView {
	var cfg ScreenConfig
	_ = decodeStrict(r.Config, &cfg)
	if cfg.Items == nil {
		cfg.Items = []ScreenItem{}
	}
	return ScreenView{ID: r.ID, Name: r.Name, Remark: r.Remark, Config: cfg,
		CreatedAt: r.CreatedAt.UTC().Format(timeFmt), UpdatedAt: r.UpdatedAt.UTC().Format(timeFmt)}
}

// Roles 列出本租户启用的角色（只读 id、name）。
func (s *Service) Roles(ctx context.Context) ([]RoleOption, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var out []RoleOption
	err = s.db.WithContext(ctx).Table(roleTable).Select("id, name").
		Where("tenant_id = ? AND status = ?", tid, "1").Order("sort, id").Scan(&out).Error
	if err != nil {
		return nil, dbErr(err)
	}
	if out == nil {
		out = []RoleOption{}
	}
	return out, nil
}

func (s *Service) checkScreen(ctx context.Context, f *ScreenForm) (string, []idgen.ID, error) {
	if badText(f.Name, 100, true) || badText(f.Remark, 255, false) || badText(f.Config.Title, 100, false) {
		return "", nil, specErr("名称不能为空且不超过 100 字")
	}
	if r := f.Config.RefreshSec; r != 0 && (r < 5 || r > 3600) {
		return "", nil, specErr("刷新间隔需为 0（不刷新）或 5 到 3600 秒")
	}
	if e := validateScreenItems(f.Config.Items); e != nil {
		return "", nil, e
	}
	ids := make([]string, len(f.Config.Items))
	for i, it := range f.Config.Items {
		ids[i] = it.ChartID
	}
	parsed, e := parseIDs(ids)
	if e != nil {
		return "", nil, e
	}
	have, err := s.existingIDs(ctx, "plg_report_chart", parsed)
	if err != nil {
		return "", nil, err
	}
	for i := range f.Config.Items {
		id, _ := idgen.Parse(f.Config.Items[i].ChartID)
		if !have[id] {
			return "", nil, specErr("引用的图表不存在")
		}
		f.Config.Items[i].ChartID = id.String()
	}
	roles, e := parseIDs(f.RoleIDs)
	if e != nil {
		return "", nil, e
	}
	if len(roles) > 0 {
		tid, err := tenantOf(ctx)
		if err != nil {
			return "", nil, err
		}
		var got []idgen.ID
		if err := s.db.WithContext(ctx).Table(roleTable).Where("tenant_id = ? AND id IN ?", tid, roles).Pluck("id", &got).Error; err != nil {
			return "", nil, dbErr(err)
		}
		if len(got) != len(roles) {
			return "", nil, specErr("角色不存在")
		}
	}
	raw, err := encodeJSON(f.Config)
	return raw, roles, dbErr(err)
}

// SaveScreen 创建（id=0）或更新大屏，并整体替换其角色授权。
func (s *Service) SaveScreen(ctx context.Context, id idgen.ID, f ScreenForm) (*ScreenView, error) {
	raw, roles, err := s.checkScreen(ctx, &f)
	if err != nil {
		return nil, err
	}
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	var row Screen
	if id != 0 {
		if row, err = s.loadScreen(ctx, id); err != nil {
			return nil, err
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id == 0 {
			row = Screen{Name: f.Name, Remark: f.Remark, Config: raw}
			row.TenantID = idgen.ID(tid)
			if cl := auth.ClaimsFromContext(ctx); cl != nil {
				row.CreatedBy = cl.UserID
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			upd := map[string]any{"name": f.Name, "remark": f.Remark, "config": raw, "updated_at": now()}
			if err := tx.Model(&Screen{}).Where("tenant_id = ? AND id = ?", tid, id).Updates(upd).Error; err != nil {
				return err
			}
			row.Name, row.Remark, row.Config = f.Name, f.Remark, raw
		}
		if err := tx.Where("tenant_id = ? AND screen_id = ?", tid, row.ID).Delete(&ScreenRole{}).Error; err != nil {
			return err
		}
		for _, r := range roles {
			sr := ScreenRole{ScreenID: row.ID, RoleID: r}
			sr.TenantID = idgen.ID(tid)
			if err := tx.Create(&sr).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, dbErr(err)
	}
	v := screenView(row)
	v.RoleIDs = roles
	return &v, nil
}

func (s *Service) loadScreen(ctx context.Context, id idgen.ID) (Screen, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return Screen{}, err
	}
	var row Screen
	if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Screen{}, notFound()
		}
		return Screen{}, dbErr(err)
	}
	return row, nil
}

func (s *Service) screenRoleIDs(ctx context.Context, screenID idgen.ID) ([]idgen.ID, error) {
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, err
	}
	ids := []idgen.ID{}
	if err := tx.Model(&ScreenRole{}).Where("screen_id = ?", screenID).Pluck("role_id", &ids).Error; err != nil {
		return nil, dbErr(err)
	}
	return ids, nil
}

// GetScreenManage 返回大屏定义与角色授权（管理端）。
func (s *Service) GetScreenManage(ctx context.Context, id idgen.ID) (*ScreenView, error) {
	row, err := s.loadScreen(ctx, id)
	if err != nil {
		return nil, err
	}
	v := screenView(row)
	if v.RoleIDs, err = s.screenRoleIDs(ctx, id); err != nil {
		return nil, err
	}
	return &v, nil
}

// DeleteScreen 删除大屏及其授权。
func (s *Service) DeleteScreen(ctx context.Context, id idgen.ID) error {
	if _, err := s.loadScreen(ctx, id); err != nil {
		return err
	}
	tid, _ := tenantOf(ctx)
	return dbErr(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND screen_id = ?", tid, id).Delete(&ScreenRole{}).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND id = ?", tid, id).Delete(&Screen{}).Error
	}))
}
