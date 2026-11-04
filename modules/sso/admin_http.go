package sso

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
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
		return response.BadRequest("sso.name_required")
	}
	seen := map[string]bool{}
	uris := []string{}
	for _, u := range f.RedirectURIs {
		u = strings.TrimSpace(u)
		if !validRedirectURI(u) {
			return response.BadRequest("sso.redirect_uri_invalid", u)
		}
		if !seen[u] {
			seen[u] = true
			uris = append(uris, u)
		}
	}
	if len(uris) == 0 || len(uris) > 20 {
		return response.BadRequest("sso.redirect_uri_required")
	}
	f.RedirectURIs = uris
	f.HomeURL = strings.TrimSpace(f.HomeURL)
	if f.HomeURL != "" && !validRedirectURI(f.HomeURL) {
		return response.BadRequest("sso.home_url_invalid")
	}
	// 按字符判：列宽 sys_sso_app.description 是 varchar(500)（MySQL 按字符计），
	// 报错文案也写着「最多 500 个字符」。用 len() 量字节会把中文描述的实际容量压到 ~166 字。
	if utf8.RuneCountInString(f.Logo) > 500 || utf8.RuneCountInString(f.Description) > 500 {
		return response.BadRequest("sso.logo_description_too_long")
	}
	roles := []string{}
	for _, r := range f.RoleIDs {
		id, err := idgen.Parse(strings.TrimSpace(r))
		if err != nil || id == 0 {
			return response.BadRequest("sso.role_invalid")
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

// appsQuery 是列表与导出共用的查询：同一套关键字过滤，租户条件由 core/database 的租户插件按 ctx 追加。
// 只写这一遍，导出不会和列表口径不一致。
func (h *handler) appsQuery(c *gin.Context) *gorm.DB {
	q := h.db.WithContext(c.Request.Context()).Model(&App{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		q = q.Where("name LIKE ? OR client_id LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	return q
}

// maxExportApps 一次导出的行数上限：接入应用是「一个租户几十条」的量级，
// 上限只兜底防异常；真要更大规模应改走导出中心作业，而不是抬这个数。
const maxExportApps = 500

// List GET /sso/apps?keyword=&page=&pageSize=（未传 pageSize 时返回全部，上限 200）
func (h *handler) list(c *gin.Context) {
	page, size := response.ParsePage(c)
	if c.Query("pageSize") == "" {
		size = 200
	}
	q := h.appsQuery(c)
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

// export GET /sso/apps/export?keyword= —— 不分页返回当前租户在当前关键字下的全部应用（与 list 同一套条件）。
// 权限复用读权限 sso:list；CSV 由前端 ExportCsvButton 生成，表头跟着界面语言走。
func (h *handler) exportApps(c *gin.Context) {
	var rows []App
	if err := h.appsQuery(c).Order("created_at DESC, id DESC").Limit(maxExportApps).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]appDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toDTO(&rows[i]))
	}
	response.OK(c, out)
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

// removeMany DELETE /sso/apps?ids=1,2,3 —— 批量删除应用，并清掉它们各自的 refresh_token。
// 权限与单条删一致（sso:edit），不新增权限码。
// 租户条件显式写进 SQL（同 modules/sendlog）：批量删除是破坏性操作，除了 GORM 租户插件那层保护
// 再钉一遍——越权删在只测列表的用例里看不出来，显式条件也让单测能钉住真实的 SQL 形状。
func (h *handler) removeMany(c *gin.Context) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err) // 空 ids / 非法 ids 一律拒绝：退化成「不带条件全删」是灾难
		return
	}
	tid, ok := tenant.From(c.Request.Context())
	if !ok {
		response.Fail(c, database.ErrNoTenant) // 认不出租户就一条都不动，不会落到「删全站」
		return
	}
	raw := make([]int64, len(ids))
	for i, id := range ids {
		raw[i] = int64(id)
	}
	err = h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND app_id IN ?", int64(tid), raw).Delete(&Refresh{}).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND id IN ?", int64(tid), raw).Delete(&App{}).Error
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
		response.Fail(c, response.BadRequest("sso.enabled_required"))
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
