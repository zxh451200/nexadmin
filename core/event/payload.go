package event

import (
	"encoding/json"
	"strings"
	"time"

	"nexadmin/pkg/idgen"
)

// 事件名。
const (
	UserCreated     = "user.created"
	UserUpdated     = "user.updated"
	UserDeleted     = "user.deleted"
	DeptCreated     = "dept.created"
	DeptUpdated     = "dept.updated"
	DeptDeleted     = "dept.deleted"
	RoleCreated     = "role.created"
	RoleUpdated     = "role.updated"
	RoleDeleted     = "role.deleted"
	NoticePublished = "notice.published"
)

// Info 是事件目录里的一项（GET /openapi/events）。
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var catalog = []Info{
	{UserCreated, "用户已创建"},
	{UserUpdated, "用户信息或状态已更新"},
	{UserDeleted, "用户已删除"},
	{DeptCreated, "部门已创建"},
	{DeptUpdated, "部门已更新"},
	{DeptDeleted, "部门已删除"},
	{RoleCreated, "角色已创建"},
	{RoleUpdated, "角色已更新"},
	{RoleDeleted, "角色已删除"},
	{NoticePublished, "公告已发布"},
}

// Catalog 返回事件目录（副本）。
func Catalog() []Info { return append([]Info(nil), catalog...) }

// Known 报告 name 是否在事件目录里。
func Known(name string) bool {
	for _, c := range catalog {
		if c.Name == name {
			return true
		}
	}
	return false
}

// 各事件的载荷：只含 id、必要字段与 tenantId。新增字段前先确认它不是敏感信息。

// UserData 是 user.* 事件载荷（不含密码哈希、手机号、邮箱）。
type UserData struct {
	ID       idgen.ID `json:"id"`
	TenantID idgen.ID `json:"tenantId"`
	Username string   `json:"username,omitempty"`
	Nickname string   `json:"nickname,omitempty"`
	Status   string   `json:"status,omitempty"`
	DeptID   idgen.ID `json:"deptId,omitempty"`
}

// EventTenant 实现 tenantOwner。
func (d UserData) EventTenant() idgen.ID { return d.TenantID }

// DeptData 是 dept.* 事件载荷。
type DeptData struct {
	ID       idgen.ID  `json:"id"`
	TenantID idgen.ID  `json:"tenantId"`
	Name     string    `json:"name,omitempty"`
	ParentID *idgen.ID `json:"parentId,omitempty"`
	Status   string    `json:"status,omitempty"`
}

// EventTenant 实现 tenantOwner。
func (d DeptData) EventTenant() idgen.ID { return d.TenantID }

// RoleData 是 role.* 事件载荷。
type RoleData struct {
	ID       idgen.ID `json:"id"`
	TenantID idgen.ID `json:"tenantId"`
	Name     string   `json:"name,omitempty"`
	Code     string   `json:"code,omitempty"`
	Status   string   `json:"status,omitempty"`
}

// EventTenant 实现 tenantOwner。
func (d RoleData) EventTenant() idgen.ID { return d.TenantID }

// NoticeData 是 notice.published 事件载荷（不含正文）。
type NoticeData struct {
	ID          idgen.ID   `json:"id"`
	TenantID    idgen.ID   `json:"tenantId"`
	Title       string     `json:"title,omitempty"`
	Type        string     `json:"type,omitempty"`
	Level       string     `json:"level,omitempty"`
	Scope       string     `json:"scope,omitempty"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// EventTenant 实现 tenantOwner。
func (d NoticeData) EventTenant() idgen.ID { return d.TenantID }

// sensitiveKeys 命中（不区分大小写、子串匹配）的字段会被 Scrub 删掉。
var sensitiveKeys = []string{"password", "passwd", "secret", "token", "hash", "salt", "credential", "apikey", "api_key", "privatekey", "private_key"}

func isSensitive(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Scrub 把载荷序列化成 JSON，并递归删掉名字像密码/密钥/令牌/哈希的字段，作为发往外部前的最后一道防线。
func Scrub(payload any) (json.RawMessage, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber() // 避免雪花 ID 等大整数被转成 float64 丢精度
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(scrub(v))
}

func scrub(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitive(k) {
				delete(t, k)
				continue
			}
			t[k] = scrub(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = scrub(t[i])
		}
		return t
	}
	return v
}
