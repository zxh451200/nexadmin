package sso

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 组织同步（只读，免费内置）：下游邮箱/OA 用 client_credentials 令牌（scope org.read）拉通讯录。
// 令牌只在应用勾选了“允许读取组织架构”时签发；每次请求还会实时复核该开关，取消勾选后已签发的令牌立即失效。

const (
	grantClientCredentials = "client_credentials"
	scopeOrgRead           = "org.read"
	orgMaxPage             = 200
	orgDefaultPage         = 100
)

// byClientCredentials 签发机器令牌：无用户、无 id_token、无 refresh_token。
func (p *provider) byClientCredentials(iss string, app *App, r tokenRequest) (*tokenResponse, *oauthErr) {
	if !app.AllowOrgRead {
		return nil, oerr(http.StatusBadRequest, "unauthorized_client", "client is not allowed to use client_credentials")
	}
	scope := scopeOrgRead
	if r.Scope != "" && strings.Join(strings.Fields(r.Scope), " ") != scopeOrgRead {
		return nil, oerr(http.StatusBadRequest, "invalid_scope", "only org.read is available for client_credentials")
	}
	now := p.now()
	at, err := p.keys.sign(jwtClaims(iss, app, scope, now, p.accessTTL), "at+jwt")
	if err != nil {
		return nil, oerr(http.StatusInternalServerError, "server_error", "sign failed")
	}
	return &tokenResponse{AccessToken: at, TokenType: "Bearer", ExpiresIn: int64(p.accessTTL.Seconds()), Scope: scope}, nil
}

func jwtClaims(iss string, app *App, scope string, now time.Time, ttl time.Duration) jwt.MapClaims {
	return jwt.MapClaims{
		"iss": iss, "sub": app.ClientID, "aud": app.ClientID, "client_id": app.ClientID, "scope": scope,
		"gty": grantClientCredentials, "iat": now.Unix(), "exp": now.Add(ttl).Unix(), "jti": randToken(12),
	}
}

// ---- 数据 ----

type orgUser struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	DeptID    string    `json:"deptId"`
	PostIDs   []string  `json:"postIds"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
	Deleted   bool      `json:"deleted,omitempty"` // 软删除用户：下游据此同步删除
}

type orgDept struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId"`
	Sort      int       `json:"sort"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// orgStore 是组织同步读取的数据接口（单测用内存实现替换）。
type orgStore interface {
	orgUsers(ctx context.Context, tenantID idgen.ID, since *time.Time, page, size int) ([]orgUser, int64, error)
	orgDepts(ctx context.Context, tenantID idgen.ID) ([]orgDept, error)
}

type orgGorm struct{ db *gorm.DB }

type orgUserRow struct {
	ID        idgen.ID
	Username  string
	Nickname  string
	Email     string
	Phone     string
	DeptID    *idgen.ID
	Status    string
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// orgUsers 数据范围说明（已核对）：调用方是应用主体（client_credentials），没有“人”的角色与部门，
// 因此不适用 sys_role.data_scope；它的边界由两层固定：① 租户——只读该应用所属租户（tid 来自应用，而非请求参数）；
// ② 授权——应用必须启用且被勾选 org.read。在这两个边界内对通讯录按“全部范围”读取是合理的
// （邮箱/OA 同步本来就要全量），但超管账号不输出，且不含密码、头像等敏感列（见下方 Select 白名单）。
func (s *orgGorm) orgUsers(ctx context.Context, tid idgen.ID, since *time.Time, page, size int) ([]orgUser, int64, error) {
	db := s.db.WithContext(tenant.Skip(ctx))
	// 软删除不会改 updated_at，所以增量条件同时看 deleted_at；超管账号不进通讯录。
	q := db.Table("sys_user").Where("tenant_id = ? AND is_super = ?", int64(tid), false)
	if since != nil {
		q = q.Where("(updated_at > ? OR deleted_at > ?)", *since, *since)
	}
	q = q.Session(&gorm.Session{})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []orgUserRow
	if err := q.Select("id, username, nickname, email, phone, dept_id, status, updated_at, deleted_at").
		Order("id ASC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]orgUser, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	idx := map[idgen.ID]int{}
	for _, r := range rows {
		u := orgUser{ID: r.ID.String(), Username: r.Username, Name: r.Nickname, Email: r.Email, Phone: r.Phone,
			PostIDs: []string{}, Status: r.Status, UpdatedAt: r.UpdatedAt}
		if r.DeptID != nil && *r.DeptID != 0 {
			u.DeptID = r.DeptID.String()
		}
		if r.DeletedAt != nil {
			u.Deleted = true
			if r.DeletedAt.After(u.UpdatedAt) {
				u.UpdatedAt = *r.DeletedAt
			}
		}
		idx[r.ID] = len(out)
		ids = append(ids, int64(r.ID))
		out = append(out, u)
	}
	if len(ids) > 0 {
		var ups []struct{ UserID, PostID idgen.ID }
		if err := db.Table("sys_user_post").Select("user_id, post_id").Where("user_id IN ?", ids).Scan(&ups).Error; err == nil {
			for _, up := range ups {
				if i, ok := idx[up.UserID]; ok {
					out[i].PostIDs = append(out[i].PostIDs, up.PostID.String())
				}
			}
		}
	}
	return out, total, nil
}

func (s *orgGorm) orgDepts(ctx context.Context, tid idgen.ID) ([]orgDept, error) {
	var rows []struct {
		ID        idgen.ID
		Name      string
		ParentID  *idgen.ID
		Sort      int
		Status    string
		UpdatedAt time.Time
	}
	err := s.db.WithContext(tenant.Skip(ctx)).Table("sys_dept").Select("id, name, parent_id, sort, status, updated_at").
		Where("tenant_id = ?", int64(tid)).Order("sort ASC, id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]orgDept, 0, len(rows))
	for _, r := range rows {
		d := orgDept{ID: r.ID.String(), Name: r.Name, Sort: r.Sort, Status: r.Status, UpdatedAt: r.UpdatedAt}
		if r.ParentID != nil && *r.ParentID != 0 {
			d.ParentID = r.ParentID.String()
		}
		out = append(out, d)
	}
	return out, nil
}

// ---- HTTP ----

// orgApp 校验 Bearer 令牌并返回对应应用：必须是本 IdP 签发的 client_credentials 令牌、含 org.read，
// 且应用仍然启用并勾选了 org.read。失败时已写响应。
func (h *handler) orgApp(c *gin.Context) (*App, bool) {
	noStore(c)
	if !h.ready(c) {
		return nil, false
	}
	tok := bearer(c)
	if tok == "" {
		bearerFail(c)
		return nil, false
	}
	cl, typ, err := h.p.keys.parse(tok, jwt.WithExpirationRequired())
	cid, _ := cl["client_id"].(string)
	gty, _ := cl["gty"].(string)
	scope, _ := cl["scope"].(string)
	if err != nil || typ != "at+jwt" || cid == "" || gty != grantClientCredentials || !slices.Contains(strings.Fields(scope), scopeOrgRead) {
		bearerFail(c)
		return nil, false
	}
	app, aerr := h.p.st.appByClientID(c.Request.Context(), cid)
	if aerr != nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "internal error"))
		return nil, false
	}
	if app == nil || !app.Enabled || !app.AllowOrgRead {
		bearerFail(c)
		return nil, false
	}
	return app, true
}

func parseSince(s string) (*time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t, true
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
		t := time.Unix(n, 0)
		return &t, true
	}
	return nil, false
}

// orgUsersHandler GET /oauth/org/users?updated_since=&page=&page_size=（单页最多 200）
func (h *handler) orgUsersHandler(c *gin.Context) {
	app, ok := h.orgApp(c)
	if !ok {
		return
	}
	since, ok := parseSince(c.Query("updated_since"))
	if !ok {
		h.writeErr(c, oerr(http.StatusBadRequest, "invalid_request", "updated_since must be RFC3339 or unix seconds"))
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", c.Query("pageSize")))
	if size < 1 {
		size = orgDefaultPage
	}
	if size > orgMaxPage {
		size = orgMaxPage
	}
	list, total, err := h.org.orgUsers(c.Request.Context(), app.TenantID, since, page, size)
	if err != nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "internal error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total, "page": page, "pageSize": size})
}

// orgDeptsHandler GET /oauth/org/depts
func (h *handler) orgDeptsHandler(c *gin.Context) {
	app, ok := h.orgApp(c)
	if !ok {
		return
	}
	list, err := h.org.orgDepts(c.Request.Context(), app.TenantID)
	if err != nil {
		h.writeErr(c, oerr(http.StatusInternalServerError, "server_error", "internal error"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": len(list)})
}
