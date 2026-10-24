package report

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 是报表中心的业务层。
//
// 租户隔离：插件自有表都嵌入 database.TenantModel，生产环境里 database.Open 注册的 scope 会自动追加租户条件；
// 但这里不依赖它——所有对自有表的读写都经过 scoped()，显式带 tenant_id = 当前租户，
// 并且上下文里没有租户时直接拒绝（不会退化成「不带条件」）。动态报表查询的租户条件见 query.go。
type Service struct {
	db  *gorm.DB
	log *zap.Logger
	reg *Registry
	set Settings
	eng *Engine
}

// NewService 创建服务。
func NewService(db *gorm.DB, log *zap.Logger, reg *Registry, set Settings) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, log: log, reg: reg, set: set, eng: newEngine(db, log)}
}

// Viewer 是发起请求的人：用于数据范围、大屏角色授权。
type Viewer struct {
	UserID  idgen.ID
	Super   bool
	Manager bool // 拥有 report:screen（可看全部大屏）
	Scope   *datascope.Scope
}

func tenantOf(ctx context.Context) (int64, error) {
	id, ok := tenant.From(ctx)
	if !ok || id == 0 {
		return 0, response.Forbidden("error.forbidden").WithMsg("缺少租户上下文")
	}
	return int64(id), nil
}

// scoped 返回已带租户条件的查询句柄。
func (s *Service) scoped(ctx context.Context) (*gorm.DB, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return nil, err
	}
	return s.db.WithContext(ctx).Where("tenant_id = ?", tid), nil
}

func (s *Service) env(ctx context.Context, v Viewer) (Env, error) {
	tid, err := tenantOf(ctx)
	if err != nil {
		return Env{}, err
	}
	return Env{TenantID: tid, Scope: v.Scope, Timeout: s.set.Timeout, MaxRows: s.set.MaxRows}, nil
}

// dbErr 把存储层错误收敛为通用 500（细节只进日志）。
func dbErr(err error) error {
	if err == nil {
		return nil
	}
	return response.Internal().Wrap(err)
}

func notFound() *response.Error {
	return response.NotFound("error.not_found").WithMsg("记录不存在")
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func parseIDs(ss []string) ([]idgen.ID, *response.Error) {
	out := make([]idgen.ID, 0, len(ss))
	seen := map[idgen.ID]bool{}
	for _, s := range ss {
		id, err := idgen.Parse(s)
		if err != nil || id == 0 {
			return nil, specErr("ID 无效")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// existingIDs 返回表里（本租户）实际存在的 id 集合。table 只来自本包常量。
func (s *Service) existingIDs(ctx context.Context, table string, ids []idgen.ID) (map[idgen.ID]bool, error) {
	out := map[idgen.ID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	tx, err := s.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var got []idgen.ID
	if err := tx.Table(table).Where("id IN ?", ids).Pluck("id", &got).Error; err != nil {
		return nil, dbErr(err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

func now() time.Time { return time.Now().UTC() }

// pageOf 夹紧分页参数。
func pageOf(page, size int) (offset, limit int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	return (page - 1) * size, size
}
