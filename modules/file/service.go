package file

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/cache"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Limits 是上传限制。大小单位 MB（可以是小数），0 表示不限制；AllowedTypes 为空表示不限制类型。
type Limits struct {
	MaxImageMB   float64
	MaxVideoMB   float64
	MaxFileMB    float64
	AllowedTypes []string
}

// mergeLimits 以 config.yaml 的限制（base）为底，叠加设置页（系统参数「上传限制」）里设了的项：
// 大小 > 0 才覆盖，类型列表非空才覆盖；设置页没设的项回落到 config.yaml。
func mergeLimits(base Limits, s core.UploadLimits) Limits {
	if s.MaxImageMB > 0 {
		base.MaxImageMB = s.MaxImageMB
	}
	if s.MaxVideoMB > 0 {
		base.MaxVideoMB = s.MaxVideoMB
	}
	if s.MaxFileMB > 0 {
		base.MaxFileMB = s.MaxFileMB
	}
	if len(s.AllowedTypes) > 0 {
		base.AllowedTypes = append([]string(nil), s.AllowedTypes...)
	}
	return base
}

// settingsLimits 返回生效的上传限制：config.yaml 为底，设置页的值优先（设置页保存后经短缓存即生效）。
// 文件模块不 import 系统参数模块，设置页的值由它通过 core.SetUploadLimitsSource 登记。
func settingsLimits(base Limits) LimitsFunc {
	return func(ctx context.Context) Limits {
		if s, ok := core.UploadLimitsFromSettings(ctx); ok {
			return mergeLimits(base, s)
		}
		return base
	}
}

// LimitsFunc 返回当前生效的上传限制。默认读 config 里的 upload.*；
// 系统参数模块可以通过 SetLimitsProvider 换成“后台可设”的实现。
type LimitsFunc func(ctx context.Context) Limits

var limitsProvider LimitsFunc

// SetLimitsProvider 让系统参数模块接管上传限制（后台参数页修改后即时生效）。
func SetLimitsProvider(f LimitsFunc) { limitsProvider = f }

// Service 是文件业务层。
type Service struct {
	cache  cache.Cache
	db     *gorm.DB
	store  *store
	limits LimitsFunc
}

// NewService 创建业务层。
func NewService(db *gorm.DB, st *store, limits LimitsFunc) *Service {
	return &Service{db: db, store: st, limits: limits}
}

func (s *Service) currentLimits(ctx context.Context) Limits {
	if limitsProvider != nil {
		return limitsProvider(ctx)
	}
	return s.limits(ctx)
}

func badRequest(msg string) error { return response.BadRequest("error.bad_request").WithMsg(msg) }

// ---------- 展示转换 ----------

func (s *Service) toItem(f File) FileItem {
	it := FileItem{
		ID: f.ID.String(), Name: f.Name, PosterURL: f.PosterURL, Type: f.MimeType, Category: f.Category,
		Size: f.Size, Visibility: f.Visibility, Uploader: f.Uploader, CreatedAt: f.CreatedAt.UTC(),
	}
	if f.GroupID != 0 {
		it.GroupID = f.GroupID.String()
	}
	switch {
	case f.StoragePath == "":
		it.URL = f.URL
	case f.Visibility == "public":
		it.URL = apiBase + "/files/public/" + f.Token + "/" + url.PathEscape(f.Name)
	default:
		it.URL = s.store.signedURL(f.ID.String())
	}
	return it
}

func (s *Service) toItems(rows []File) []FileItem {
	out := make([]FileItem, 0, len(rows))
	for _, f := range rows {
		out = append(out, s.toItem(f))
	}
	return out
}

// ---------- 分组 ----------

func (s *Service) groupExists(ctx context.Context, id idgen.ID) (bool, error) {
	if id == 0 {
		return true, nil
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&Group{}).Where("id = ?", id).Count(&n).Error
	return n > 0, err
}

// ListGroups 返回分组及各组文件数。
func (s *Service) ListGroups(ctx context.Context) ([]GroupItem, error) {
	var groups []Group
	if err := s.db.WithContext(ctx).Order("created_at ASC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	var counts []struct {
		GroupID idgen.ID
		N       int64
	}
	if err := s.db.WithContext(ctx).Model(&File{}).Select("group_id, COUNT(*) AS n").Group("group_id").Scan(&counts).Error; err != nil {
		return nil, err
	}
	byGroup := map[idgen.ID]int64{}
	for _, c := range counts {
		byGroup[c.GroupID] = c.N
	}
	out := make([]GroupItem, 0, len(groups))
	for _, g := range groups {
		out = append(out, GroupItem{ID: g.ID.String(), Name: g.Name, Count: byGroup[g.ID]})
	}
	return out, nil
}

// CreateGroup 新建分组。
func (s *Service) CreateGroup(ctx context.Context, name string) (GroupItem, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return GroupItem{}, badRequest("分组名称不能为空")
	}
	g := Group{Name: name}
	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
		return GroupItem{}, err
	}
	return GroupItem{ID: g.ID.String(), Name: g.Name}, nil
}

// RenameGroup 重命名分组。
func (s *Service) RenameGroup(ctx context.Context, id idgen.ID, name string) (GroupItem, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return GroupItem{}, badRequest("分组名称不能为空")
	}
	var g Group
	if err := s.db.WithContext(ctx).First(&g, "id = ?", id).Error; err != nil {
		return GroupItem{}, notFoundOr(err)
	}
	if err := s.db.WithContext(ctx).Model(&g).Update("name", name).Error; err != nil {
		return GroupItem{}, err
	}
	return GroupItem{ID: g.ID.String(), Name: name}, nil
}

// DeleteGroup 删除分组，组内文件回到“未分组”。
func (s *Service) DeleteGroup(ctx context.Context, id idgen.ID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&File{}).Where("group_id = ?", id).Update("group_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(&Group{}, "id = ?", id).Error
	})
}

func notFoundOr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return response.NotFound("error.not_found")
	}
	return err
}

// ---------- 文件 ----------

// List 分页查询文件。
func (s *Service) List(ctx context.Context, q listQuery, page, pageSize int) ([]FileItem, int64, error) {
	tx := s.db.WithContext(ctx).Model(&File{})
	switch q.GroupID {
	case "":
	case "none":
		tx = tx.Where("group_id = ?", 0)
	default:
		gid, err := idgen.Parse(q.GroupID)
		if err != nil {
			return nil, 0, badRequest("分组参数不正确")
		}
		tx = tx.Where("group_id = ?", gid)
	}
	if q.Category != "" {
		tx = tx.Where("category = ?", q.Category)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		kw = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw)
		like := "%" + kw + "%"
		tx = tx.Where("(name LIKE ? OR uploader LIKE ?)", like, like)
	}
	if t, err := time.Parse("2006-01-02", q.From); err == nil {
		tx = tx.Where("created_at >= ?", t)
	}
	if t, err := time.Parse("2006-01-02", q.To); err == nil {
		tx = tx.Where("created_at < ?", t.AddDate(0, 0, 1))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []File
	if err := tx.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return s.toItems(rows), total, nil
}

func normVisibility(v string) string {
	if v == "public" {
		return "public"
	}
	return "private"
}

// externalURLOK 只放行常见安全协议，避免 javascript: 之类进入库里。
func externalURLOK(u string) bool {
	l := strings.ToLower(u)
	for _, p := range []string{"http://", "https://", "/", "data:image/", "data:video/"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return u == ""
}

// uploadIDFromURL 从 tus 上传地址（.../files/upload/<id>）里取出文件 ID。
func uploadIDFromURL(u string) (idgen.ID, bool) {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if !strings.Contains(u, "/files/upload/") {
		return 0, false
	}
	id, err := idgen.Parse(path.Base(u))
	return id, err == nil
}

// Register 登记文件：tus 已上传的文件更新分组/可见性/名称；外部地址则新增一行。
func (s *Service) Register(ctx context.Context, username string, req registerReq) ([]FileItem, error) {
	if ok, err := s.groupExists(ctx, req.GroupID); err != nil {
		return nil, err
	} else if !ok {
		return nil, badRequest("分组不存在")
	}
	vis := normVisibility(req.Visibility)
	var out []File
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, rf := range req.Files {
			name := cleanName(rf.Name)
			if id, ok := uploadIDFromURL(rf.URL); ok {
				var f File
				err := tx.First(&f, "id = ? AND storage_path <> ''", id).Error
				if err == nil {
					if f.Uploader != username {
						return response.Forbidden("error.forbidden")
					}
					patch := map[string]any{"name": name, "group_id": req.GroupID, "visibility": vis}
					if rf.PosterURL != "" && externalURLOK(rf.PosterURL) {
						patch["poster_url"] = rf.PosterURL
					}
					if err := tx.Model(&f).Updates(patch).Error; err != nil {
						return err
					}
					if err := tx.First(&f, "id = ?", id).Error; err != nil {
						return err
					}
					out = append(out, f)
					continue
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			} else if strings.Contains(rf.URL, "/files/upload/") {
				return badRequest("上传文件不存在")
			}
			if strings.TrimSpace(rf.URL) == "" || !externalURLOK(rf.URL) ||
				(rf.PosterURL != "" && !externalURLOK(rf.PosterURL)) {
				return badRequest("文件地址不合法")
			}
			f := File{
				GroupID: req.GroupID, Name: name, URL: rf.URL, PosterURL: rf.PosterURL, MimeType: rf.Type,
				Category: classify(rf.Type, name), Size: max(rf.Size, 0), Visibility: vis, Token: randomToken(), Uploader: username,
			}
			if err := tx.Create(&f).Error; err != nil {
				return err
			}
			out = append(out, f)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.toItems(out), nil
}

func cleanName(n string) string {
	n = strings.TrimSpace(path.Base(strings.ReplaceAll(n, `\`, "/")))
	if n == "" || n == "." || n == "/" {
		return "unnamed"
	}
	return n
}

// Update 修改名称 / 可见性 / 分组。
func (s *Service) Update(ctx context.Context, id idgen.ID, req updateReq) (FileItem, error) {
	var f File
	if err := s.db.WithContext(ctx).First(&f, "id = ?", id).Error; err != nil {
		return FileItem{}, notFoundOr(err)
	}
	patch := map[string]any{}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return FileItem{}, badRequest("文件名不能为空")
		}
		patch["name"] = cleanName(*req.Name)
	}
	if req.Visibility != nil {
		if *req.Visibility != "public" && *req.Visibility != "private" {
			return FileItem{}, badRequest("可见性只能是 public 或 private")
		}
		patch["visibility"] = *req.Visibility
	}
	if req.GroupID != nil {
		if ok, err := s.groupExists(ctx, *req.GroupID); err != nil {
			return FileItem{}, err
		} else if !ok {
			return FileItem{}, badRequest("分组不存在")
		}
		patch["group_id"] = *req.GroupID
	}
	if len(patch) > 0 {
		if err := s.db.WithContext(ctx).Model(&f).Updates(patch).Error; err != nil {
			return FileItem{}, err
		}
		if err := s.db.WithContext(ctx).First(&f, "id = ?", id).Error; err != nil {
			return FileItem{}, err
		}
	}
	return s.toItem(f), nil
}

// Move 批量移动到分组（0 = 未分组）。
func (s *Service) Move(ctx context.Context, ids []idgen.ID, groupID idgen.ID) error {
	if ok, err := s.groupExists(ctx, groupID); err != nil {
		return err
	} else if !ok {
		return badRequest("分组不存在")
	}
	return s.db.WithContext(ctx).Model(&File{}).Where("id IN ?", ids).Update("group_id", groupID).Error
}

// Delete 批量删除文件记录，并尽力删除存储里的对象（删除失败只影响垃圾文件，不回滚）。
func (s *Service) Delete(ctx context.Context, ids []idgen.ID) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []File
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&File{}).Error; err != nil {
		return err
	}
	var freed int64
	for _, f := range rows {
		if f.StoragePath == "" {
			continue
		}
		freed += f.Size
		_ = s.store.blobs.Delete(ctx, f.StoragePath)
	}
	if tid, ok := tenant.From(ctx); ok && freed > 0 {
		tenant.StorageMeter.Add(tid, -freed)
	}
	return nil
}
