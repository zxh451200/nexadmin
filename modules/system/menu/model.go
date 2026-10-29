package menu

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 菜单类型与状态。
const (
	typeDir    = "dir"
	typeMenu   = "menu"
	typeButton = "button"

	statusOn = "1"
)

// Menu 对应 sys_menu（平台级，嵌入 PlatformModel，不带 tenant_id）。
// ParentID = 0 表示顶级；Code 是出厂数据的稳定业务键。
type Menu struct {
	database.PlatformModel
	ParentID  idgen.ID
	Code      string
	Type      string
	Title     string
	Icon      string
	Path      string
	Component string
	Perm      string
	Sort      int
	KeepAlive bool
	Hidden    bool
	External  bool
	Status    string
}

// TableName 指定表名。
func (Menu) TableName() string { return "sys_menu" }

// menuDTO 与前端 SysMenu 一致：parentId 顶级为 null，空字段省略，子节点放 children。
type menuDTO struct {
	ID        idgen.ID   `json:"id"`
	ParentID  *idgen.ID  `json:"parentId"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Icon      string     `json:"icon,omitempty"`
	Path      string     `json:"path,omitempty"`
	Component string     `json:"component,omitempty"`
	Perm      string     `json:"perm,omitempty"`
	Sort      int        `json:"sort"`
	KeepAlive bool       `json:"keepAlive"`
	Hidden    bool       `json:"hidden"`
	External  bool       `json:"external"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Children  []*menuDTO `json:"children,omitempty"`
}

func toDTO(m *Menu) *menuDTO {
	d := &menuDTO{
		ID: m.ID, Type: m.Type, Title: m.Title, Icon: m.Icon, Path: m.Path, Component: m.Component,
		Perm: m.Perm, Sort: m.Sort, KeepAlive: m.KeepAlive, Hidden: m.Hidden, External: m.External,
		Status: m.Status, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.ParentID != 0 {
		p := m.ParentID
		d.ParentID = &p
	}
	return d
}

// menuInput 是新增/修改菜单的请求体（SysMenuInput）。parentId 可为 null 或空串。
type menuInput struct {
	ParentID  string `json:"parentId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Icon      string `json:"icon"`
	Path      string `json:"path"`
	Component string `json:"component"`
	Perm      string `json:"perm"`
	Sort      int    `json:"sort"`
	KeepAlive bool   `json:"keepAlive"`
	Hidden    bool   `json:"hidden"`
	External  bool   `json:"external"`
	Status    string `json:"status"`
}

// sortInput 是 PUT /menus/sort 的请求体：同一父节点下的新顺序。
type sortInput struct {
	ParentID string   `json:"parentId"`
	IDs      []string `json:"ids"`
}

// moveInput 是跨父级移动菜单的请求体。
type moveInput struct {
	ParentID string `json:"parentId"`
	// AfterID 指定插到哪个同级之后：空串=插到最前，省略=沿用旧行为追加到末尾。
	// 拖拽落点是一条插入线，能力上必须能表达「插在第 N 行后面」，否则只能追加到组尾。
	AfterID *string `json:"afterId"`
}
