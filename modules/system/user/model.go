package user

import (
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 状态与性别取值（字典 sys_status / sys_gender）。
const (
	statusOn  = "1"
	statusOff = "0"
)

// User 对应 sys_user（按租户隔离，软删除）。
// Password 是 bcrypt 哈希；空串表示不可登录（出厂演示账号，需管理员先重置密码）。
type User struct {
	database.TenantModel
	Username    string
	ExternalID  string `gorm:"size:255;index"`
	Password    string
	Nickname    string
	Avatar      string
	Phone       string
	Email       string
	Gender      string
	DeptID      idgen.ID
	Status      string
	Province    string
	City        string
	District    string
	Remark      string
	IsSuper     bool // 超管：可进 /api/v1/admin，可跨租户
	LastLoginAt *time.Time
	DeletedAt   gorm.DeletedAt
}

func (User) TableName() string { return "sys_user" }

// userRole 对应 sys_user_role。
type userRole struct {
	UserID idgen.ID
	RoleID idgen.ID
}

func (userRole) TableName() string { return "sys_user_role" }

// userPost 对应 sys_user_post。
type userPost struct {
	UserID idgen.ID
	PostID idgen.ID
}

func (userPost) TableName() string { return "sys_user_post" }

// regionDTO 是省市区编码，与前端 RegionValue 一致。
type regionDTO struct {
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
}

// userDTO 与前端 User 一致。deptName / roleNames 由服务端关联查询得出。
type userDTO struct {
	ExternalID  string     `json:"-"`
	ID          idgen.ID   `json:"id"`
	Username    string     `json:"username"`
	Nickname    string     `json:"nickname"`
	Avatar      string     `json:"avatar"`
	Phone       string     `json:"phone"`
	Email       string     `json:"email"`
	Gender      string     `json:"gender"`
	DeptID      string     `json:"deptId"` // 未分配部门为空串
	DeptName    string     `json:"deptName"`
	PostIDs     []idgen.ID `json:"postIds"`
	RoleIDs     []idgen.ID `json:"roleIds"`
	RoleNames   []string   `json:"roleNames"`
	Status      string     `json:"status"`
	Region      regionDTO  `json:"region"`
	Remark      string     `json:"remark"`
	Locked      bool       `json:"locked"`
	LockedUntil *time.Time `json:"lockedUntil"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// userInput 是新增/修改用户的请求体（UserInput；deptName / roleNames 由服务端忽略）。
type userInput struct {
	ExternalIDSet bool `json:"-"`
	// NoPassword 为 true 时（SCIM 等由 IdP 供号的路径）不使用 user.default_password，
	// 而是写入一个随机且无人知道的强口令哈希：账号只能经 IdP / 重置流程登录。
	NoPassword bool      `json:"-"`
	ExternalID string    `json:"-"`
	Username   string    `json:"username"`
	Nickname   string    `json:"nickname"`
	Avatar     string    `json:"avatar"`
	Phone      string    `json:"phone"`
	Email      string    `json:"email"`
	Gender     string    `json:"gender"`
	DeptID     string    `json:"deptId"`
	PostIDs    []string  `json:"postIds"`
	RoleIDs    []string  `json:"roleIds"`
	Status     string    `json:"status"`
	Region     regionDTO `json:"region"`
	Remark     string    `json:"remark"`
}

// listQuery 是用户列表/导出的筛选条件。
type listQuery struct {
	Page, PageSize int
	Keyword        string
	Status         string
	DeptID         string
	Gender         string
	CreatedFrom    string // YYYY-MM-DD，闭区间
	CreatedTo      string
}

// actor 是当前操作者，用于数据范围和“不能操作自己”的判断。
type actor struct {
	ID    idgen.ID
	Super bool
	// Provision 标记“外部供号通道”（SCIM）：它沿用 Super 的数据范围，但 guardTarget 对超管账号
	// 与持有内置角色的账号照常拒绝，不享受超管豁免。
	Provision bool
}
