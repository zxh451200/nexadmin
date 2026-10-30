package media

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/response"
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

const (
	keyEnabled  = "video.enabled"
	keyProvider = "video.provider"
	keyKeyA     = "video.key_a"
	keyKeyB     = "video.key_b"
	keyKeyC     = "video.key_c"
)

func (s *service) settings(ctx context.Context) (map[string]string, error) {
	var rows []setting
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}

// VideoConfig 读取视频托管配置；缺省即「未启用」。
func (s *service) VideoConfig(ctx context.Context) (VideoConfig, error) {
	m, err := s.settings(ctx)
	if err != nil {
		return VideoConfig{}, err
	}
	provider := m[keyProvider]
	if provider == "" {
		provider = ProviderCloudflare
	}
	return VideoConfig{
		Enabled: m[keyEnabled] == "true", Provider: provider,
		KeyA: m[keyKeyA], KeyB: m[keyKeyB], KeyC: m[keyKeyC],
	}, nil
}

// SaveVideoConfig 保存配置；KeyB（密钥）留空表示沿用旧值，前端永远不回显。
func (s *service) SaveVideoConfig(ctx context.Context, in configReq) (VideoConfigView, error) {
	cur, err := s.VideoConfig(ctx)
	if err != nil {
		return VideoConfigView{}, err
	}
	provider := strings.TrimSpace(in.Provider)
	if provider == "" {
		provider = ProviderCloudflare
	}
	if _, err := providerFor(provider); err != nil {
		return VideoConfigView{}, badRequest("media.provider_invalid").Wrap(err)
	}
	secret := strings.TrimSpace(in.KeyB)
	if secret == "" {
		secret = cur.KeyB
	}
	next := VideoConfig{Enabled: in.Enabled, Provider: provider, KeyA: strings.TrimSpace(in.KeyA), KeyB: secret, KeyC: strings.TrimSpace(in.KeyC)}
	if next.Enabled {
		impl, err := providerFor(provider)
		if err != nil {
			return VideoConfigView{}, badRequest("media.provider_invalid").Wrap(err)
		}
		if impl.SupportsUpload() {
			if next.KeyA == "" || next.KeyB == "" || next.KeyC == "" {
				return VideoConfigView{}, badRequest("media.upload_credentials_required")
			}
		} else if next.KeyA == "" {
			return VideoConfigView{}, badRequest("media.playback_domain_required")
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for k, v := range map[string]string{
			keyEnabled: boolText(next.Enabled), keyProvider: next.Provider,
			keyKeyA: next.KeyA, keyKeyB: next.KeyB, keyKeyC: next.KeyC,
		} {
			if err := tx.Save(&setting{Key: k, Value: v}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return VideoConfigView{}, err
	}
	return view(next), nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func view(c VideoConfig) VideoConfigView {
	canUpload := false
	if impl, err := providerFor(c.Provider); err == nil {
		canUpload = impl.SupportsUpload()
	}
	return VideoConfigView{
		Enabled: c.Enabled, Provider: c.Provider, KeyA: c.KeyA, KeyC: c.KeyC,
		HasKeyB: c.KeyB != "", CanUpload: canUpload,
	}
}

// CreateVideoUpload 申请直传参数：浏览器把文件直传给厂商，不经过本机存储。
func (s *service) CreateVideoUpload(ctx context.Context, name string) (uploadURL string, headers map[string]string, videoID, playback string, err error) {
	cfg, err := s.VideoConfig(ctx)
	if err != nil {
		return "", nil, "", "", err
	}
	if !cfg.Enabled {
		return "", nil, "", "", badRequest("media.disabled")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, "", "", badRequest("media.name_required")
	}
	provider, err := providerFor(cfg.Provider)
	if err != nil {
		return "", nil, "", "", err
	}
	uploadURL, headers, videoID, err = provider.CreateUpload(ctx, cfg, name)
	if err != nil {
		return "", nil, "", "", err
	}
	return uploadURL, headers, videoID, provider.PlaybackURL(cfg, videoID), nil
}

// SaveVideo 记录一条视频台账；播放/下载地址由服务端按 videoId 推导，不信客户端。
func (s *service) SaveVideo(ctx context.Context, username string, req videoSaveReq) (VideoItem, error) {
	if !videoIDPattern.MatchString(req.VideoID) {
		return VideoItem{}, badRequest("media.video_id_invalid")
	}
	cfg, err := s.VideoConfig(ctx)
	if err != nil {
		return VideoItem{}, err
	}
	if !cfg.Enabled {
		return VideoItem{}, badRequest("media.disabled")
	}
	provider, err := providerFor(cfg.Provider)
	if err != nil {
		return VideoItem{}, err
	}
	row := Video{
		Name: strings.TrimSpace(req.Name), Size: max(req.Size, 0), Provider: provider.Name(),
		VideoID: req.VideoID, Playback: provider.PlaybackURL(cfg, req.VideoID),
		Status: "ready", UploadedBy: username,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return VideoItem{}, err
	}
	return toVideoItem(row, provider.DownloadURL(cfg, row.VideoID)), nil
}

// Videos 按创建时间倒序返回视频台账；播放地址按每条记录自己的厂商推导。
func (s *service) Videos(ctx context.Context, page, pageSize int) ([]VideoItem, int64, error) {
	tx := s.db.WithContext(ctx).Model(&Video{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Video
	if err := tx.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	cfg, err := s.VideoConfig(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]VideoItem, 0, len(rows))
	for _, r := range rows {
		download := ""
		if p, err := providerFor(r.Provider); err == nil {
			download = p.DownloadURL(cfg, r.VideoID)
		}
		out = append(out, toVideoItem(r, download))
	}
	return out, total, nil
}

// DeleteVideo 删台账（厂商侧资产需要在控制台清理）。
func (s *service) DeleteVideo(ctx context.Context, id string) error {
	var row Video
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound("error.not_found")
		}
		return err
	}
	return s.db.WithContext(ctx).Delete(&Video{}, "id = ?", row.ID).Error
}

func toVideoItem(v Video, download string) VideoItem {
	return VideoItem{
		ID: v.ID.String(), Name: v.Name, Size: v.Size, Provider: v.Provider, VideoID: v.VideoID,
		Playback: v.Playback, Download: download, Status: v.Status, Uploader: v.UploadedBy, CreatedAt: v.CreatedAt.UTC(),
	}
}

func badRequest(key string, args ...any) *response.Error {
	return response.BadRequest(key, args...)
}
