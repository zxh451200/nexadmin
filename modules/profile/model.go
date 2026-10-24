package profile

import (
	"time"

	"nexadmin/pkg/idgen"
)

// userRow 是 sys_user 的最小映射，只包含个人中心用到的列（表由 user 模块维护）。
// 带 TenantID 字段，所以查询/更新会自动限定当前租户；这里不嵌 TenantModel，避免误触其他列。
type userRow struct {
	ID       idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	TenantID idgen.ID
	Username string
	Password string
	Nickname string
	Avatar   string
	Phone    string
	Email    string
	Gender   string
	Province string
	City     string
	District string
	Bio      string `gorm:"column:remark"`
}

func (userRow) TableName() string { return "sys_user" }

// Profile 与前端 Profile 一致。
type Profile struct {
	ID       idgen.ID `json:"id"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	Avatar   string   `json:"avatar"`
	Gender   string   `json:"gender"`
	Phone    string   `json:"phone,omitempty"`
	Email    string   `json:"email,omitempty"`
	Province string   `json:"province,omitempty"`
	City     string   `json:"city,omitempty"`
	District string   `json:"district,omitempty"`
	Bio      string   `json:"bio,omitempty"`
}

// ProfileInput 是 PUT /profile 的请求体（不含 id / username / phone，手机号只能走绑定流程）。
type ProfileInput struct {
	Nickname string `json:"nickname" binding:"required,max=64"`
	Avatar   string `json:"avatar" binding:"max=500"`
	Gender   string `json:"gender" binding:"omitempty,oneof=0 1 2"`
	Email    string `json:"email" binding:"omitempty,email,max=100"`
	Province string `json:"province" binding:"max=12"`
	City     string `json:"city" binding:"max=12"`
	District string `json:"district" binding:"max=12"`
	Bio      string `json:"bio" binding:"max=500"`
}

// ProviderBinding / Bindings 与前端一致。
type ProviderBinding struct {
	Key          string     `json:"key"`
	Name         string     `json:"name"`
	Bound        bool       `json:"bound"`
	ExternalName string     `json:"externalName,omitempty"`
	BoundAt      *time.Time `json:"boundAt,omitempty"`
}

type Bindings struct {
	Phone     string            `json:"phone,omitempty"` // 已打码
	Providers []ProviderBinding `json:"providers"`
}

// LoginSession 与前端 LoginSession 一致。
type LoginSession struct {
	ID           string    `json:"id"`
	Device       string    `json:"device"`
	Browser      string    `json:"browser,omitempty"`
	OS           string    `json:"os,omitempty"`
	IP           string    `json:"ip"`
	LoginAt      time.Time `json:"loginAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
	Current      bool      `json:"current"`
}

func toProfile(u *userRow) *Profile {
	return &Profile{ID: u.ID, Username: u.Username, Nickname: u.Nickname, Avatar: u.Avatar, Gender: u.Gender,
		Phone: u.Phone, Email: u.Email, Province: u.Province, City: u.City, District: u.District, Bio: u.Bio}
}

// maskPhone 138****8000。
func maskPhone(p string) string {
	if len(p) == 11 {
		return p[:3] + "****" + p[7:]
	}
	return p
}
