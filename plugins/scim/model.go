package scim

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"nexadmin/pkg/idgen"
)

type tokenDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Revoked   bool       `json:"revoked"`
	LastUsed  *time.Time `json:"lastUsed,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type tokenInput struct {
	Name string `json:"name" binding:"required,max=100"`
}

type userResource struct {
	Schemas      []string     `json:"schemas"`
	ID           string       `json:"id,omitempty"`
	ExternalID   string       `json:"externalId,omitempty"`
	UserName     string       `json:"userName"`
	DisplayName  string       `json:"displayName,omitempty"`
	Active       *bool        `json:"active,omitempty"`
	Emails       []contactDTO `json:"emails,omitempty"`
	PhoneNumbers []contactDTO `json:"phoneNumbers,omitempty"`
	Meta         *struct {
		ResourceType string    `json:"resourceType"`
		Created      time.Time `json:"created"`
		LastModified time.Time `json:"lastModified"`
		Version      string    `json:"version,omitempty"`
	} `json:"meta,omitempty"`
}

type patchRequest struct {
	Schemas    []string `json:"schemas"`
	Operations []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	} `json:"Operations"`
}

type userRow struct {
	ID         idgen.ID `gorm:"column:id"`
	TenantID   idgen.ID `gorm:"column:tenant_id"`
	ExternalID string   `gorm:"column:external_id"`
	Username   string   `gorm:"column:username"`
	Nickname   string   `gorm:"column:nickname"`
	Email      string   `gorm:"column:email"`
	Phone      string   `gorm:"column:phone"`
	Status     string   `gorm:"column:status"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func etag(u userRow) string {
	sum := sha256.Sum256([]byte(u.ID.String() + ":" + u.UpdatedAt.UTC().Format(time.RFC3339Nano)))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

type group struct {
	ID          idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	TenantID    idgen.ID `gorm:"index;not null"`
	ExternalID  string   `gorm:"size:255"`
	DisplayName string   `gorm:"size:255;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (group) TableName() string { return "plg_scim_group" }

type groupMember struct {
	GroupID  idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	UserID   idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	TenantID idgen.ID `gorm:"index;not null"`
}

func (groupMember) TableName() string { return "plg_scim_group_member" }

type groupResource struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []struct {
		Value   string `json:"value"`
		Display string `json:"display,omitempty"`
	} `json:"members,omitempty"`
}
