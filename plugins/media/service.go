package media

import (
	"context"
	"net/url"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 与文件模块一致：公开文件走 /files/public/<token>/<name>，可直接被 <img>/fetch 使用。
const apiBase = "/api/v1"

// optimizedWhere 只挑「公开、已落盘、JPEG/PNG、还没有 WebP 副本」的原图。
// 私有图片只在后台出现，不值得为它多存一份；GIF/SVG 不能转，直接排除。
const optimizedSource = `variant_of = 0 AND category = 'image' AND visibility = 'public' AND storage_path <> ''
	AND mime_type IN ('image/jpeg', 'image/png')`

const hasWebp = `EXISTS (SELECT 1 FROM sys_file v WHERE v.variant_of = sys_file.id AND v.mime_type = 'image/webp')`

type Stats struct {
	Total   int64 `json:"total"`
	Done    int64 `json:"done"`
	Pending int64 `json:"pending"`
}

type Image struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Size      int64     `json:"size"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

type fileRow struct {
	ID        idgen.ID  `gorm:"column:id"`
	Name      string    `gorm:"column:name"`
	MimeType  string    `gorm:"column:mime_type"`
	Size      int64     `gorm:"column:size"`
	Token     string    `gorm:"column:token"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

type service struct{ db *gorm.DB }

func newService(db *gorm.DB) *service { return &service{db: db} }

// scoped 给裸表查询补租户条件：GORM 的自动隔离只对模型生效，这里直接查 sys_file。
func (s *service) scoped(ctx context.Context) *gorm.DB {
	tx := s.db.WithContext(ctx).Table("sys_file")
	if tid, ok := tenant.From(ctx); ok && !tenant.Skipped(ctx) {
		tx = tx.Where("tenant_id = ?", tid)
	}
	return tx
}

func (s *service) Stats(ctx context.Context) (Stats, error) {
	var total, done int64
	if err := s.scoped(ctx).Where(optimizedSource).Count(&total).Error; err != nil {
		return Stats{}, err
	}
	if err := s.scoped(ctx).Where(optimizedSource).Where(hasWebp).Count(&done).Error; err != nil {
		return Stats{}, err
	}
	return Stats{Total: total, Done: done, Pending: total - done}, nil
}

// Pending 返回还没有 WebP 副本的原图，按创建时间倒序分页。
func (s *service) Pending(ctx context.Context, page, pageSize int) ([]Image, int64, error) {
	tx := s.scoped(ctx).Where(optimizedSource).Where("NOT " + hasWebp)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []fileRow
	if err := tx.Select("id, name, mime_type, size, token, created_at").
		Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]Image, 0, len(rows))
	for _, r := range rows {
		out = append(out, Image{
			ID: r.ID.String(), Name: r.Name, Type: r.MimeType, Size: r.Size,
			URL: apiBase + "/files/public/" + r.Token + "/" + url.PathEscape(r.Name), CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return out, total, nil
}
