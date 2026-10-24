package role

import (
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 状态与数据范围取值。
const (
	statusOn  = "1"
	statusOff = "0"

	scopeAll          = "all"          // 全部
	scopeCustom       = "custom"       // 自定义部门
	scopeDept         = "dept"         // 本部门
	scopeDeptAndChild = "deptAndChild" // 本部门及以下
	scopeSelf         = "self"         // 仅本人
)

// Role 对应 sys_role（按租户隔离）。
type Role struct {
	database.TenantModel
	Name      string
	Code      string
	Sort      int
	Status    string
	DataScope string
	Builtin   bool // 内置角色不可删除、不可停用
	// Assignable：是否允许被下级管理员分配给用户。超管不受限；内置超管角色恒为不可分配。
	Assignable bool
	Remark     string
}

func (Role) TableName() string { return "sys_role" }

// roleMenu 对应 sys_role_menu：角色拥有的菜单/按钮。
type roleMenu struct {
	RoleID idgen.ID
	MenuID idgen.ID
}

func (roleMenu) TableName() string { return "sys_role_menu" }

// roleDept 对应 sys_role_dept：数据范围为“自定义”时可见的部门。
type roleDept struct {
	RoleID idgen.ID
	DeptID idgen.ID
}

func (roleDept) TableName() string { return "sys_role_dept" }

// roleDTO 与前端 Role 一致。
type roleDTO struct {
	ID         idgen.ID   `json:"id"`
	Name       string     `json:"name"`
	Code       string     `json:"code"`
	Sort       int        `json:"sort"`
	Status     string     `json:"status"`
	DataScope  string     `json:"dataScope"`
	DeptIDs    []idgen.ID `json:"deptIds"`
	Remark     string     `json:"remark"`
	Builtin    bool       `json:"builtin,omitempty"`
	Assignable bool       `json:"assignable"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func toDTO(r *Role, deptIDs []idgen.ID) *roleDTO {
	if deptIDs == nil {
		deptIDs = []idgen.ID{}
	}
	return &roleDTO{
		ID: r.ID, Name: r.Name, Code: r.Code, Sort: r.Sort, Status: r.Status, DataScope: r.DataScope,
		DeptIDs: deptIDs, Remark: r.Remark, Builtin: r.Builtin, Assignable: r.Assignable, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// roleInput 是新增/修改角色的请求体（RoleInput）。
type roleInput struct {
	Name      string   `json:"name"`
	Code      string   `json:"code"`
	Sort      int      `json:"sort"`
	Status    string   `json:"status"`
	DataScope string   `json:"dataScope"`
	DeptIDs   []string `json:"deptIds"`
	Remark    string   `json:"remark"`
	// Assignable 缺省：新建为 true，修改时不变。
	Assignable *bool `json:"assignable"`
}

// option 是 GET /roles/options 的元素。
type option struct {
	ID   idgen.ID `json:"id"`
	Name string   `json:"name"`
}
