package openapi

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	coreoa "nexadmin/core/openapi"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

type handler struct {
	db  *gorm.DB
	log *zap.Logger
	snd *sender
	wk  *worker
}

func badReq(msg string) *response.Error { return response.BadRequest("error.bad_request").WithMsg(msg) }

func parseID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil || id == 0 {
		response.Fail(c, badReq("invalid id"))
		return 0, false
	}
	return id, true
}

// ---- 个人访问令牌 ----

const maxTokensPerUser = 50

type tokenDTO struct {
	ID         idgen.ID   `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type tokenCreatedDTO struct {
	ID        idgen.ID   `json:"id"`
	Name      string     `json:"name"`
	Scope     string     `json:"scope"`
	Prefix    string     `json:"prefix"`
	ExpiresAt *time.Time `json:"expiresAt"`
	CreatedAt time.Time  `json:"createdAt"`
	Token     string     `json:"token"` // 明文仅此一次
}

// self 返回当前登录用户（仅真实登录会话；令牌/应用主体一律拒绝，防止用令牌管理令牌）。
func self(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return nil, false
	}
	if cl.Principal != "" {
		response.Fail(c, response.Forbidden("error.forbidden").WithMsg("tokens cannot manage tokens"))
		return nil, false
	}
	return cl, true
}

// GET /profile/tokens
func (h *handler) tokenList(c *gin.Context) {
	cl, ok := self(c)
	if !ok {
		return
	}
	var rows []coreoa.PAT
	if err := h.db.WithContext(c.Request.Context()).Where("user_id = ?", int64(cl.UserID)).
		Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		response.Fail(c, err)
		return
	}
	out := make([]tokenDTO, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, tokenDTO{ID: r.ID, Name: r.Name, Scope: r.Scope, Prefix: r.Prefix, LastUsedAt: r.LastUsedAt, ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt})
	}
	response.OK(c, out)
}

// parseExpiry 解析 expiresAt：空/null = 不过期；支持 RFC3339、"2006-01-02 15:04:05"、"2006-01-02"；必须晚于 now。
func parseExpiry(s *string, now time.Time) (*time.Time, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	v := strings.TrimSpace(*s)
	var t time.Time
	var err error
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err = time.Parse(layout, v); err == nil {
			break
		}
	}
	if err != nil {
		return nil, errors.New("expiresAt must be an RFC3339 time")
	}
	t = t.UTC()
	if !t.After(now) {
		return nil, errors.New("expiresAt must be in the future")
	}
	return &t, nil
}

// POST /profile/tokens {name,scope,expiresAt?}
func (h *handler) tokenCreate(c *gin.Context) {
	cl, ok := self(c)
	if !ok {
		return
	}
	var f struct {
		Name      string  `json:"name"`
		Scope     string  `json:"scope"`
		ExpiresAt *string `json:"expiresAt"`
	}
	if !response.BindJSON(c, &f) {
		return
	}
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > 100 {
		response.Fail(c, badReq("name is required (max 100)"))
		return
	}
	if f.Scope != "read" && f.Scope != "write" {
		response.Fail(c, badReq("scope must be read or write"))
		return
	}
	exp, err := parseExpiry(f.ExpiresAt, time.Now())
	if err != nil {
		response.Fail(c, badReq(err.Error()))
		return
	}
	ctx := c.Request.Context()
	var n int64
	if err := h.db.WithContext(ctx).Model(&coreoa.PAT{}).Where("user_id = ?", int64(cl.UserID)).Count(&n).Error; err != nil {
		response.Fail(c, err)
		return
	}
	if n >= maxTokensPerUser {
		response.Fail(c, response.Conflict("error.conflict").WithMsg("too many tokens, delete unused ones first"))
		return
	}
	plain, hash, prefix := coreoa.NewPATToken()
	rec := coreoa.PAT{UserID: cl.UserID, Name: f.Name, Scope: f.Scope, TokenHash: hash, Prefix: prefix, ExpiresAt: exp}
	if err := h.db.WithContext(ctx).Create(&rec).Error; err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tokenCreatedDTO{ID: rec.ID, Name: rec.Name, Scope: rec.Scope, Prefix: rec.Prefix, ExpiresAt: rec.ExpiresAt, CreatedAt: rec.CreatedAt, Token: plain})
}

// DELETE /profile/tokens/:id（只能删自己的）
func (h *handler) tokenDelete(c *gin.Context) {
	cl, ok := self(c)
	if !ok {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	res := h.db.WithContext(c.Request.Context()).Where("id = ? AND user_id = ?", int64(id), int64(cl.UserID)).Delete(&coreoa.PAT{})
	if res.Error != nil {
		response.Fail(c, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		response.Fail(c, response.NotFound("error.not_found"))
		return
	}
	response.OK(c, nil)
}
