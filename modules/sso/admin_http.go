package sso

import (
	"errors"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

type appForm struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirectUris"`
	Logo         string   `json:"logo"`
	HomeURL      string   `json:"homeUrl"`
	Description  string   `json:"description"`
	Enabled      *bool    `json:"enabled"`
	RoleIDs      []string `json:"roleIds"`
	AllowOrgRead *bool    `json:"allowOrgRead"`
}

func badReq(msg string) *response.Error { return response.BadRequest("error.bad_request").WithMsg(msg) }

// validRedirectURI：绝对 http(s) 地址，无 fragment、无用户信息、无通配符。
func validRedirectURI(s string) bool {
	if s == "" || len(s) > 500 || strings.ContainsAny(s, "*# \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Fragment == ""
}

// normalize 校验并规整表单（去重、trim）。
func (f *appForm) normalize() *response.Error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len([]rune(f.Name)) > 100 {
		return badReq("name is required (max 100)")
	}
	seen := map[string]bool{}
	uris := []string{}
	for _, u := range f.RedirectURIs {
		u = strings.TrimSpace(u)
		if !validRedirectURI(u) {
			return badReq("invalid redirect uri: " + u)
		}
		if !seen[u] {
			seen[u] = true
			uris = append(uris, u)
		}
	}
	if len(uris) == 0 || len(uris) > 20 {
		return badReq("1-20 redirect uris are required")
	}
	f.RedirectURIs = uris
	f.HomeURL = strings.TrimSpace(f.HomeURL)
	if f.HomeURL != "" && !validRedirectURI(f.HomeURL) {
		return badReq("invalid homeUrl")
	}
	if len(f.Logo) > 500 || len(f.Description) > 500 {
		return badReq("logo/description too long")
	}
	roles := []string{}
	for _, r := range f.RoleIDs {
		id, err := idgen.Parse(strings.TrimSpace(r))
		if err != nil || id == 0 {
			return badReq("invalid role id")
		}
		roles = append(roles, id.String())
	}
	f.RoleIDs = roles
	return nil
}

func newSecret() (plain, hash string) {
	plain = randToken(32)
	return plain, hashSecret(plain)
}

// List GET /sso/apps?keyword=&page=&pageSize=（未传 pageSize 时返回全部，上限 200）
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	if c.Query("pageSize") == "" {
		size = 200
	}
	q := h.db.WithContext(c.Request.Context()).Model(&App{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		q = q.Where("name LIKE ? OR client_id LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		response.Fail(c, err)
		return
	}
	var rows []App
	if err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]appDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toDTO(&rows[i]))
	}
	response.Page(c, out, total, page, size)
}

// Create POST /sso/apps，响应里带一次性明文 clientSecret。
func (h *handler) create(c *gin.Context) {
	var f appForm
	if !response.BindJSON(c, &f) {
		return
	}
	if e := f.normalize(); e != nil {
		response.Fail(c, e)
		return
	}
	secret, hash := newSecret()
	a := App{Name: f.Name, ClientID: "nxsso_" + strings.ToLower(randToken(12)), SecretHash: hash,
		RedirectURIs: encodeList(f.RedirectURIs), Logo: f.Logo, HomeURL: f.HomeURL, Description: f.Description,
		Enabled: f.Enabled == nil || *f.Enabled, RoleIDs: encodeList(f.RoleIDs), AllowOrgRead: f.AllowOrgRead != nil && *f.AllowOrgRead}
	if err := h.db.WithContext(c.Request.Context()).Create(&a).Error; err != nil {
		response.Fail(c, err)
		return
	}
	d := toDTO(&a)
	d.ClientSecret = secret
	response.OK(c, d)
}

func (h *handler) find(c *gin.Context, id idgen.ID) (*App, bool) {
	var a App
	err := h.db.WithContext(c.Request.Context()).Where("id = ?", int64(id)).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Fail(c, response.NotFound("error.not_found"))
		return nil, false
	}
	if err != nil {
		response.Fail(c, err)
		return nil, false
	}
	return &a, true
}

// Update PUT /sso/apps/:id
func (h *handler) update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f appForm
	if !response.BindJSON(c, &f) {
		return
	}
	if e := f.normalize(); e != nil {
		response.Fail(c, e)
		return
	}
	a, ok := h.find(c, id)
	if !ok {
		return
	}
	a.Name, a.RedirectURIs, a.Logo, a.HomeURL, a.Description, a.RoleIDs = f.Name, encodeList(f.RedirectURIs), f.Logo, f.HomeURL, f.Description, encodeList(f.RoleIDs)
	if f.Enabled != nil {
		a.Enabled = *f.Enabled
	}
	if f.AllowOrgRead != nil {
		a.AllowOrgRead = *f.AllowOrgRead
	}
	if err := h.db.WithContext(c.Request.Context()).Save(a).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toDTO(a))
}

// Delete DELETE /sso/apps/:id（同时清掉该应用的 refresh_token）
func (h *handler) remove(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	a, ok := h.find(c, id)
	if !ok {
		return
	}
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("app_id = ?", int64(a.ID)).Delete(&Refresh{}).Error; err != nil {
			return err
		}
		return tx.Delete(a).Error
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// SetEnabled PUT /sso/apps/:id/enabled {enabled}
func (h *handler) setEnabled(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var b struct {
		Enabled *bool `json:"enabled"`
	}
	if !response.BindJSON(c, &b) {
		return
	}
	if b.Enabled == nil {
		response.Fail(c, badReq("enabled is required"))
		return
	}
	a, ok := h.find(c, id)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(a).Update("enabled", *b.Enabled).Error; err != nil {
		response.Fail(c, err)
		return
	}
	a.Enabled = *b.Enabled
	response.OK(c, toDTO(a))
}

// ResetSecret POST /sso/apps/:id/reset-secret：旧密钥立即失效，明文只返回这一次。
func (h *handler) resetSecret(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	a, ok := h.find(c, id)
	if !ok {
		return
	}
	secret, hash := newSecret()
	if err := h.db.WithContext(c.Request.Context()).Model(a).Update("secret_hash", hash).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"clientSecret": secret})
}
