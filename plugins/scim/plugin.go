package scim

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/plugin"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() {
	plugin.Register(plugin.Plugin{ID: "scim", Name: "SCIM 用户同步", Version: "1.0.0", Edition: plugin.Pro, Module: &module{}})
}

type module struct{}

func (*module) Name() string { return "scim" }

func (m *module) Register(r *gin.RouterGroup, d *core.Deps) {
	s := &service{db: d.DB}
	admin := r.Group("/plugins/scim")
	admin.GET("/tokens", core.Perm("scim:list"), s.listTokens)
	admin.POST("/tokens", core.Perm("scim:token"), s.createToken)
	admin.DELETE("/tokens/:id", core.Perm("scim:token"), s.revokeToken)

	public := d.Public.Group("/plugins/scim/v2")
	public.Use(func(c *gin.Context) {
		c.Header("Content-Type", "application/scim+json; charset=utf-8")
		c.Next()
	})
	public.Use(s.bearer())
	public.GET("/ServiceProviderConfig", s.providerConfig)
	public.GET("/ResourceTypes", s.resourceTypes)
	public.GET("/ResourceTypes/:id", s.resourceType)
	public.GET("/Schemas", s.schemas)
	public.GET("/Schemas/:id", s.schema)
	public.GET("/Users", s.listUsers)
	public.POST("/Users", s.createUser)
	public.GET("/Users/:id", s.getUser)
	public.PUT("/Users/:id", s.replaceUser)
	public.PATCH("/Users/:id", s.patchUser)
	public.DELETE("/Users/:id", s.deleteUser)
	public.GET("/Groups", s.listGroups)
	public.POST("/Groups", s.createGroup)
	public.GET("/Groups/:id", s.getGroup)
	public.PUT("/Groups/:id", s.replaceGroup)
	public.PATCH("/Groups/:id", s.patchGroup)
	public.DELETE("/Groups/:id", s.deleteGroup)
}

type token struct {
	ID        idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	TenantID  idgen.ID `gorm:"index;not null"`
	Name      string   `gorm:"size:100;not null"`
	Hash      string   `gorm:"size:64;uniqueIndex;not null"`
	Revoked   bool     `gorm:"not null;default:false"`
	LastUsed  *time.Time
	CreatedAt time.Time
}

func (token) TableName() string { return "plg_scim_token" }

type service struct {
	db *gorm.DB
}

func hashToken(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "nxscim_" + hex.EncodeToString(b), nil
}

func scimError(c *gin.Context, status int, detail string) {
	scimErrorType(c, status, "", detail)
}

func scimErrorType(c *gin.Context, status int, kind, detail string) {
	body := gin.H{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:Error"}, "detail": detail, "status": strconv.Itoa(status)}
	if kind != "" {
		body["scimType"] = kind
	}
	c.Header("Content-Type", "application/scim+json; charset=utf-8")
	c.JSON(status, body)
}

func (s *service) bearer() gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.Header("WWW-Authenticate", "Bearer")
			scimError(c, 401, "authentication required")
			c.Abort()
			return
		}
		var t token
		if err := s.db.WithContext(tenant.Skip(c.Request.Context())).Where("hash = ? AND revoked = ?", hashToken(parts[1]), false).First(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.Header("WWW-Authenticate", "Bearer")
				scimError(c, 401, "invalid credentials")
			} else {
				scimError(c, 500, "credential store unavailable")
			}
			c.Abort()
			return
		}
		ctx := tenant.With(c.Request.Context(), t.TenantID)
		if err := tenant.CheckActive(ctx, t.TenantID); err != nil {
			scimError(c, 403, "tenant unavailable")
			c.Abort()
			return
		}
		now := time.Now().UTC()
		_ = s.db.WithContext(tenant.Skip(ctx)).Model(&token{}).Where("id = ?", t.ID).Update("last_used", now).Error
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
