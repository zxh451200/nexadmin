package openapi

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/database"
	coreoa "nexadmin/core/openapi"
	"nexadmin/core/response"
	"nexadmin/core/secret"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type appDTO struct {
	ID          idgen.ID   `json:"id"`
	Name        string     `json:"name"`
	AppKey      string     `json:"appKey"`
	Description string     `json:"description"`
	RoleIDs     []string   `json:"roleIds"`
	Enabled     bool       `json:"enabled"`
	IPAllow     []string   `json:"ipAllow"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	AppSecret   string     `json:"appSecret,omitempty"` // 仅创建/重置时返回一次
}

func toAppDTO(a *coreoa.App) appDTO {
	return appDTO{ID: a.ID, Name: a.Name, AppKey: a.AppKey, Description: a.Description, RoleIDs: a.RoleList(), Enabled: a.Enabled,
		IPAllow: a.IPList(), CreatedAt: a.CreatedAt, LastUsedAt: a.LastUsedAt}
}

type appForm struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RoleIDs     []string `json:"roleIds"`
	IPAllow     []string `json:"ipAllow"`
	Enabled     *bool    `json:"enabled"`
}

func (f *appForm) normalize() *response.Error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > 100 {
		return response.BadRequest("openapi.name_required")
	}
	if utf8.RuneCountInString(f.Description) > 500 {
		return response.BadRequest("openapi.description_too_long")
	}
	seen := map[string]bool{}
	roles := []string{}
	for _, r := range f.RoleIDs {
		id, err := idgen.Parse(strings.TrimSpace(r))
		if err != nil || id == 0 {
			return response.BadRequest("openapi.role_invalid")
		}
		if !seen[id.String()] {
			seen[id.String()] = true
			roles = append(roles, id.String())
		}
	}
	if len(roles) > 50 {
		return response.BadRequest("openapi.role_limit")
	}
	f.RoleIDs = roles
	ips := []string{}
	seenIP := map[string]bool{}
	for _, s := range f.IPAllow {
		if strings.TrimSpace(s) == "" {
			continue
		}
		norm, ok := coreoa.ParseAllowEntry(s)
		if !ok {
			return response.BadRequest("openapi.ip_invalid", s)
		}
		if !seenIP[norm] {
			seenIP[norm] = true
			ips = append(ips, norm)
		}
	}
	if len(ips) > 50 {
		return response.BadRequest("openapi.ip_limit")
	}
	f.IPAllow = ips
	return nil
}

// checkRoles：角色必须属于当前租户且不是内置角色（内置角色等于超管，服务账号不能绑定）。
func (h *handler) checkRoles(c *gin.Context, roles []string) *response.Error {
	if len(roles) == 0 {
		return nil
	}
	ctx := c.Request.Context()
	tid, ok := tenant.From(ctx)
	if !ok {
		return response.BadRequest("tenant.context_required")
	}
	ids := make([]int64, 0, len(roles))
	for _, r := range roles {
		n, _ := strconv.ParseInt(r, 10, 64)
		ids = append(ids, n)
	}
	var n int64
	err := h.db.WithContext(ctx).Table("sys_role").Where("id IN ? AND tenant_id = ? AND builtin = ?", ids, int64(tid), false).Count(&n).Error
	if err != nil {
		return response.Internal().Wrap(err)
	}
	if int(n) != len(ids) {
		return response.BadRequest("openapi.roles_invalid")
	}
	return nil
}

func newAppCredentials() (key, secretPlain string) {
	return "nxapp_" + strings.ToLower(coreoa.RandString(16)), "nxs_" + coreoa.RandString(40)
}

// GET /openapi/apps?keyword=&page=&pageSize=（未传 pageSize 时返回全部，上限 200）
func (h *handler) appList(c *gin.Context) {
	page, size := response.ParsePage(c)
	if c.Query("pageSize") == "" {
		size = 200
	}
	q := h.db.WithContext(c.Request.Context()).Model(&coreoa.App{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		q = q.Where("name LIKE ? OR app_key LIKE ?", "%"+kw+"%", "%"+kw+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		response.Fail(c, err)
		return
	}
	var rows []coreoa.App
	if err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]appDTO, 0, len(rows))
	for i := range rows {
		out = append(out, toAppDTO(&rows[i]))
	}
	response.Page(c, out, total, page, size)
}

// POST /openapi/apps
func (h *handler) appCreate(c *gin.Context) {
	var f appForm
	if !response.BindJSON(c, &f) {
		return
	}
	if e := f.normalize(); e != nil {
		response.Fail(c, e)
		return
	}
	if e := h.checkRoles(c, f.RoleIDs); e != nil {
		response.Fail(c, e)
		return
	}
	key, plain := newAppCredentials()
	enc, err := secret.Encrypt(plain)
	if err != nil {
		response.Fail(c, err)
		return
	}
	a := coreoa.App{Name: f.Name, AppKey: key, SecretEnc: enc, Description: f.Description, RoleIDs: coreoa.EncodeList(f.RoleIDs),
		IPAllow: coreoa.EncodeList(f.IPAllow), Enabled: f.Enabled == nil || *f.Enabled}
	if err := h.db.WithContext(c.Request.Context()).Create(&a).Error; err != nil {
		response.Fail(c, err)
		return
	}
	d := toAppDTO(&a)
	d.AppSecret = plain
	response.OK(c, d)
}

func (h *handler) findApp(c *gin.Context, id idgen.ID) (*coreoa.App, bool) {
	var a coreoa.App
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

// PUT /openapi/apps/:id（不改密钥；enabled 不传则保持）
func (h *handler) appUpdate(c *gin.Context) {
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
	if e := h.checkRoles(c, f.RoleIDs); e != nil {
		response.Fail(c, e)
		return
	}
	a, ok := h.findApp(c, id)
	if !ok {
		return
	}
	a.Name, a.Description, a.RoleIDs, a.IPAllow = f.Name, f.Description, coreoa.EncodeList(f.RoleIDs), coreoa.EncodeList(f.IPAllow)
	if f.Enabled != nil {
		a.Enabled = *f.Enabled
	}
	if err := h.db.WithContext(c.Request.Context()).Save(a).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, toAppDTO(a))
}

// DELETE /openapi/apps/:id
func (h *handler) appRemove(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	a, ok := h.findApp(c, id)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(a).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// idValues 把批量 id 转成 SQL 参数：GORM 的 `IN ?` 要 []int64，而 idgen.ID 只是 int64 的别名类型。
func idValues(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// DELETE /openapi/apps?ids=1,2,3 —— 批量删除开放应用，权限码复用单条删除的 openapi:edit。
//
// 单条删除（appRemove）除了「按 id 读出本租户的应用（租户条件由 core/database 的租户插件追加）再删行」
// 之外没有别的清理动作，批量版因此也只删行：
//   - 密钥不需要额外吊销：签名调用每请求都现查 App（core/openapi 的 authn.app → Store.AppByKey），
//     缓存里只有 nonce 与 last_used_at 的节流键，行没了签名立刻验不过；
//   - 投递记录（sys_webhook_delivery）只挂 Webhook，与 App 无关，不存在悬挂引用。
//
// 租户条件再显式写进 WHERE（同 modules/sso、modules/sendlog 的批量删除）：破坏性操作除了插件那层保护
// 再钉一遍，越权/别租户的 id 不会被删、也就不会计入实删条数，单测还能钉住真实 SQL 形状。
func (h *handler) appRemoveMany(c *gin.Context) {
	ids, err := response.ParseIDsQuery(c)
	if err != nil {
		response.Fail(c, err) // 空 ids / 任一项非法一律 400：绝不退化成「不带条件动整张表」
		return
	}
	tid, ok := tenant.From(c.Request.Context())
	if !ok {
		response.Fail(c, database.ErrNoTenant) // 认不出租户就一条都不动，不会落到「删全站」
		return
	}
	res := h.db.WithContext(c.Request.Context()).
		Where("tenant_id = ? AND id IN ?", int64(tid), idValues(ids)).
		Delete(&coreoa.App{})
	if res.Error != nil {
		response.Fail(c, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		response.Fail(c, response.NotFound("error.not_found")) // 与单条删除同口径：选中的都不在就 404
		return
	}
	response.OK(c, map[string]int64{"deleted": res.RowsAffected})
}

// PUT /openapi/apps/:id/enabled {enabled}
func (h *handler) appSetEnabled(c *gin.Context) {
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
		response.Fail(c, response.BadRequest("openapi.enabled_required"))
		return
	}
	a, ok := h.findApp(c, id)
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(a).Update("enabled", *b.Enabled).Error; err != nil {
		response.Fail(c, err)
		return
	}
	a.Enabled = *b.Enabled
	response.OK(c, toAppDTO(a))
}

// POST /openapi/apps/:id/reset-secret：旧密钥立即失效，新明文只返回这一次。
func (h *handler) appResetSecret(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	a, ok := h.findApp(c, id)
	if !ok {
		return
	}
	plain := "nxs_" + coreoa.RandString(40)
	enc, err := secret.Encrypt(plain)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(a).Update("secret_enc", enc).Error; err != nil {
		response.Fail(c, err)
		return
	}
	d := toAppDTO(a)
	d.AppSecret = plain
	response.OK(c, d)
}
