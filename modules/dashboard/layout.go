package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 工作台布局（按用户保存的卡片顺序与隐藏项）。卡片由前端发现，服务端不知道有哪些卡片，
// 所以只校验格式与数量，不校验 id 是否存在；前端合并时会丢弃不存在的 id。
const (
	maxLayoutItems = 100
	maxLayoutIDLen = 64
	maxLayoutBody  = 8 << 10 // 8KB
)

var layoutIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9\-_.]*$`)

// Layout 是 GET/PUT /dashboard/layout 的结构，与前端一致。
type Layout struct {
	Order  []string `json:"order"`
	Hidden []string `json:"hidden"`
}

// layoutRow 对应 sys_dashboard_layout：(tenant_id, user_id) 唯一，layout 为 Layout 的 JSON 文本。
type layoutRow struct {
	ID        idgen.ID `gorm:"primaryKey;autoIncrement:false"`
	TenantID  idgen.ID `gorm:"not null"`
	UserID    idgen.ID `gorm:"not null"`
	Layout    string   `gorm:"type:text;not null"`
	UpdatedAt time.Time
}

func (layoutRow) TableName() string { return "sys_dashboard_layout" }

// BeforeCreate 生成雪花 ID 并填入当前租户。
func (r *layoutRow) BeforeCreate(tx *gorm.DB) error {
	if r.ID == 0 {
		id, err := idgen.Next()
		if err != nil {
			return err
		}
		r.ID = id
	}
	if r.TenantID == 0 {
		tid, ok := tenant.From(tx.Statement.Context)
		if !ok {
			return errors.New("dashboard: no tenant in context")
		}
		r.TenantID = tid
	}
	return nil
}

func layoutInvalid(msg string) *response.Error {
	return response.BadRequest("error.bad_request").WithMsg(msg)
}

func validateIDs(name string, ids []string) error {
	if len(ids) > maxLayoutItems {
		return layoutInvalid(name + ": too many items")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if len(id) > maxLayoutIDLen || !layoutIDRe.MatchString(id) {
			return layoutInvalid(name + ": invalid card id")
		}
		if _, dup := seen[id]; dup {
			return layoutInvalid(name + ": duplicate card id")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// Validate 校验并规范化（nil 变成空切片）。
func (l *Layout) Validate() error {
	if l.Order == nil {
		l.Order = []string{}
	}
	if l.Hidden == nil {
		l.Hidden = []string{}
	}
	if err := validateIDs("order", l.Order); err != nil {
		return err
	}
	return validateIDs("hidden", l.Hidden)
}

// GetLayout 读取用户布局；没有记录（或内容损坏）返回空数组。
func (s *service) GetLayout(ctx context.Context, uid idgen.ID) (Layout, error) {
	out := Layout{Order: []string{}, Hidden: []string{}}
	var rows []layoutRow
	if err := s.db.WithContext(ctx).Where("user_id = ?", uid).Limit(1).Find(&rows).Error; err != nil {
		return out, err
	}
	if len(rows) == 0 {
		return out, nil
	}
	var saved Layout
	if json.Unmarshal([]byte(rows[0].Layout), &saved) != nil || saved.Validate() != nil {
		return out, nil
	}
	return saved, nil
}

// PutLayout 保存布局：(tenant_id, user_id) 冲突时覆盖，重复保存幂等。
func (s *service) PutLayout(ctx context.Context, uid idgen.ID, l Layout) error {
	if err := l.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(l)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row := layoutRow{UserID: uid, Layout: string(raw), UpdatedAt: now}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"layout": string(raw), "updated_at": now}),
	}).Create(&row).Error
}

// DeleteLayout 恢复默认（删除记录；没有记录也成功）。
func (s *service) DeleteLayout(ctx context.Context, uid idgen.ID) error {
	return s.db.WithContext(ctx).Where("user_id = ?", uid).Delete(&layoutRow{}).Error
}

// layoutUser 取当前用户 ID；开放平台应用主体或没有用户身份时返回 403。
func layoutUser(c *gin.Context) (idgen.ID, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil || cl.UserID == 0 || cl.Principal == auth.PrincipalApp {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return 0, false
	}
	return cl.UserID, true
}

func (s *service) getLayoutHandler(c *gin.Context) {
	uid, ok := layoutUser(c)
	if !ok {
		return
	}
	l, err := s.GetLayout(c.Request.Context(), uid)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, l)
}

func (s *service) putLayoutHandler(c *gin.Context) {
	uid, ok := layoutUser(c)
	if !ok {
		return
	}
	// ContentLength 可能是 -1 或被谎报：MaxBytesReader 兜底，读到上限就报错。
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxLayoutBody))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			response.Fail(c, response.New(http.StatusRequestEntityTooLarge, "error.payload_too_large"))
			return
		}
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	var in Layout
	if err := json.Unmarshal(raw, &in); err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	if err := in.Validate(); err != nil {
		response.Fail(c, err)
		return
	}
	if err := s.PutLayout(c.Request.Context(), uid, in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, in)
}

func (s *service) deleteLayoutHandler(c *gin.Context) {
	uid, ok := layoutUser(c)
	if !ok {
		return
	}
	if err := s.DeleteLayout(c.Request.Context(), uid); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}
