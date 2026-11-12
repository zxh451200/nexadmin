package file

import (
	"context"
	"errors"
	"math"
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

// LimitsFunc 返回当前生效的上传限制：config.yaml 的 upload.* 为底，系统参数「上传限制」覆盖
// （settingsLimits 经 core.UploadLimitsFromSettings 取设置页的值）。装配时由 NewService 注入，
// 只这一条通道——早先另有一个包级 SetLimitsProvider 注册点，没人调用，留着会让人以为有第二条链。
type LimitsFunc func(ctx context.Context) Limits

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
	return s.limits(ctx)
}

// ---------- 展示转换 ----------

func (s *Service) toItem(f File) FileItem {
	it := FileItem{
		ID: f.ID.String(), Name: f.Name, PosterURL: f.PosterURL, Type: f.MimeType, Category: f.Category,
		Size: f.Size, Visibility: f.Visibility, Uploader: f.Uploader, CreatedAt: f.CreatedAt.UTC(),
		Width: f.Width, Height: f.Height,
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
	if err := s.db.WithContext(ctx).Model(&File{}).Where("variant_of = 0").Select("group_id, COUNT(*) AS n").Group("group_id").Scan(&counts).Error; err != nil {
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
		return GroupItem{}, response.BadRequest("file.group_name_required")
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
		return GroupItem{}, response.BadRequest("file.group_name_required")
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
	tx := s.db.WithContext(ctx).Model(&File{}).Where("variant_of = 0")
	// 分组多选：CSV 里混着 "none"（未分组）与分组 id；非法项忽略，命中的并集返回
	var onlyNone, hasGroup bool
	var groupIDs []int64
	for _, part := range strings.Split(q.GroupID, ",") {
		switch part = strings.TrimSpace(part); part {
		case "":
		case "none":
			onlyNone = true
		default:
			if gid, err := idgen.Parse(part); err == nil && gid > 0 {
				hasGroup = true
				groupIDs = append(groupIDs, int64(gid))
			}
		}
	}
	switch {
	case onlyNone && !hasGroup:
		tx = tx.Where("group_id = ?", 0)
	case hasGroup && !onlyNone:
		tx = tx.Where("group_id IN ?", groupIDs)
	case hasGroup && onlyNone:
		tx = tx.Where("group_id IN ? OR group_id = ?", groupIDs, 0)
	}
	if cats := response.ParseList(q.Category, response.MaxFilterValues); len(cats) > 0 {
		tx = tx.Where("category IN ?", cats)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		kw = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw)
		like := "%" + kw + "%"
		tx = tx.Where("(LOWER(name) LIKE LOWER(?) OR LOWER(uploader) LIKE LOWER(?))", like, like)
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
	// 排序白名单：列名只来自这里，请求里的字符串永远进不了 SQL；id 兜底保证分页稳定
	var order string
	switch q.Sort {
	case "name":
		order = "name"
	case "size":
		order = "size"
	default:
		order = "created_at"
	}
	dir := "DESC"
	if !q.Desc {
		dir = "ASC"
	}
	var rows []File
	if err := tx.Order(order + " " + dir + ", id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
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
		return nil, response.BadRequest("file.group_not_found")
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
					// 副本可见性跟着原文件走：原文件转私有后，副本绝不能还公开着。
					if err := tx.Model(&File{}).Where("variant_of = ?", f.ID).Update("visibility", vis).Error; err != nil {
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
				return response.BadRequest("file.upload_not_found")
			}
			if strings.TrimSpace(rf.URL) == "" || !externalURLOK(rf.URL) ||
				(rf.PosterURL != "" && !externalURLOK(rf.PosterURL)) {
				return response.BadRequest("file.url_invalid")
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

// ResolveUploadURL 把 tus 上传地址（.../files/upload/<id>）解析成真实下载地址。
// 只放行公开文件：私有文件的签名地址等于绕过鉴权外泄，一律 404。
func (s *Service) ResolveUploadURL(ctx context.Context, id idgen.ID) (string, error) {
	var f File
	if err := s.db.WithContext(ctx).First(&f, "id = ? AND storage_path <> '' AND visibility = 'public'", id).Error; err != nil {
		return "", notFoundOr(err)
	}
	return apiBase + "/files/public/" + f.Token + "/" + url.PathEscape(f.Name), nil
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
			return FileItem{}, response.BadRequest("file.name_required")
		}
		patch["name"] = cleanName(*req.Name)
	}
	if req.Visibility != nil {
		if *req.Visibility != "public" && *req.Visibility != "private" {
			return FileItem{}, response.BadRequest("file.visibility_invalid")
		}
		patch["visibility"] = *req.Visibility
	}
	if req.GroupID != nil {
		if ok, err := s.groupExists(ctx, *req.GroupID); err != nil {
			return FileItem{}, err
		} else if !ok {
			return FileItem{}, response.BadRequest("file.group_not_found")
		}
		patch["group_id"] = *req.GroupID
	}
	if len(patch) > 0 {
		if err := s.db.WithContext(ctx).Model(&f).Updates(patch).Error; err != nil {
			return FileItem{}, err
		}
		if req.Visibility != nil {
			// 副本可见性跟着原文件走，避免原图转私有后副本仍可公开访问。
			if err := s.db.WithContext(ctx).Model(&File{}).Where("variant_of = ?", f.ID).Update("visibility", *req.Visibility).Error; err != nil {
				return FileItem{}, err
			}
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
		return response.BadRequest("file.group_not_found")
	}
	return s.db.WithContext(ctx).Model(&File{}).Where("id IN ?", ids).Update("group_id", groupID).Error
}

// 打包下载的一次性上限。两条都是「这条请求最多值多少」：数量决定 SQL 与 zip 条目数，
// 总字节决定响应体积。zip 是逐条流式写出的，文件内容不会整包驻留内存，但每条读缓冲、
// 以及「响应还没写完就被拖住的这条连接」都是实打实的占用——上限同时是内存和带宽天花板。
// 上限一旦超过，报错也要在写第一个字节之前给出（见 Handler.DownloadZip）。
const (
	// MaxZipFiles 单次打包的文件数上限，与批量删除同口径：response.MaxBatchIDs = 500 条点名。
	MaxZipFiles = response.MaxBatchIDs
	// MaxZipBytes 单次打包的**解压前**原始字节上限（按台账 size 累加，1 GiB）。
	// 取 1 GiB 是因为前端这请求的超时是 120s：1 GiB 已经要求 ~9 MB/s 的稳定带宽才能收完，
	// 再放大只会让慢客户端把连接占得更久，而不是让用户多拿到什么。
	MaxZipBytes = int64(1) << 30
)

// zipMB 把字节换成文案里的 MB 数（保留两位，%g 打出一长串小数没意义）。
func zipMB(b int64) float64 { return math.Round(float64(b)/(1<<20)*100) / 100 }

// FilesForDownload 取一批可打包下载的文件（含 storage_path 的才进 zip；外链没有字节可打包）。
//
// 读路径与本包 List / Delete 完全一致：只走 s.db（core/database 挂了租户插件，
// 对带 tenant_id 的模型自动追加条件）+ variant_of = 0，所以打包下载看不到列表之外的行——
// 派生副本不单独成条目，别的租户的 id 一条也查不出来。ctx 必须是带租户的请求 ctx。
func (s *Service) FilesForDownload(ctx context.Context, ids []idgen.ID) ([]File, error) {
	if len(ids) == 0 {
		return nil, response.BadRequest("file.ids_invalid")
	}
	// 数量先于 SQL 判定：一条 800 个占位符的 IN (...) 本身就是被打死的对象，
	// 不能为了算条数先把库打一遍。
	if len(ids) > MaxZipFiles {
		return nil, response.BadRequest("file.zip_too_many", len(ids), MaxZipFiles)
	}
	var rows []File
	err := s.db.WithContext(ctx).Where("id IN ? AND variant_of = 0", ids).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	// 总大小按台账 size 累加（解压前的原始字节），不碰存储：打包前就能拒绝。
	var total int64
	packable := 0
	for _, f := range rows {
		if f.StoragePath == "" {
			continue // 外链：库里只有 URL，既没有字节可累加，也没有字节可打包
		}
		packable++
		total += f.Size
	}
	if packable == 0 {
		// 点名了但一个都拿不到（跨租户 id、已删除、全是外链）：给空 zip 等于什么都没发生。
		return nil, response.BadRequest("file.zip_empty")
	}
	if total > MaxZipBytes {
		return nil, response.BadRequest("file.zip_too_large", zipMB(total), zipMB(MaxZipBytes))
	}
	return rows, nil
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
	// 派生副本跟着原文件一起走，避免留下没有入口的孤儿对象。
	var variants []File
	if err := s.db.WithContext(ctx).Where("variant_of IN ?", ids).Find(&variants).Error; err != nil {
		return err
	}
	gone := make([]idgen.ID, 0, len(rows)+len(variants))
	for _, f := range rows {
		gone = append(gone, f.ID)
	}
	for _, f := range variants {
		gone = append(gone, f.ID)
	}
	if err := s.db.WithContext(ctx).Where("id IN ?", gone).Delete(&File{}).Error; err != nil {
		return err
	}
	var freed int64
	for _, f := range append(rows, variants...) {
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
