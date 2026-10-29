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
	keyEnabled   = "video.enabled"
	keyAccountID = "video.account_id"
	keyToken     = "video.token"
	keySubdomain = "video.subdomain"
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
	return VideoConfig{
		Enabled:   m[keyEnabled] == "true",
		AccountID: m[keyAccountID],
		Token:     m[keyToken],
		Subdomain: m[keySubdomain],
	}, nil
}

// SaveVideoConfig 保存配置；token 留空表示沿用旧值（前端永远不回显令牌）。
func (s *service) SaveVideoConfig(ctx context.Context, in configReq) (VideoConfigView, error) {
	cur, err := s.VideoConfig(ctx)
	if err != nil {
		return VideoConfigView{}, err
	}
	token := strings.TrimSpace(in.Token)
	if token == "" {
		token = cur.Token
	}
	next := VideoConfig{
		Enabled:   in.Enabled,
		AccountID: strings.TrimSpace(in.AccountID),
		Token:     token,
		Subdomain: strings.TrimSpace(in.Subdomain),
	}
	if next.Enabled && (next.AccountID == "" || next.Token == "" || next.Subdomain == "") {
		return VideoConfigView{}, response.BadRequest("error.bad_request").WithMsg("启用前需要填写 Account ID、API Token 和播放子域")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for k, v := range map[string]string{
			keyEnabled:   boolText(next.Enabled),
			keyAccountID: next.AccountID,
			keyToken:     next.Token,
			keySubdomain: next.Subdomain,
		} {
			row := setting{Key: k, Value: v}
			if err := tx.Save(&row).Error; err != nil {
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
	return VideoConfigView{Enabled: c.Enabled, AccountID: c.AccountID, Subdomain: c.Subdomain, HasToken: c.Token != ""}
}

// CreateVideoUpload 申请一次性直传地址：浏览器把文件直传给厂商，不经过本机存储。
func (s *service) CreateVideoUpload(ctx context.Context, name string) (uploadURL, videoID, playback string, err error) {
	cfg, err := s.VideoConfig(ctx)
	if err != nil {
		return "", "", "", err
	}
	if !cfg.Enabled {
		return "", "", "", response.BadRequest("error.bad_request").WithMsg("视频托管未启用")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", "", response.BadRequest("error.bad_request").WithMsg("缺少文件名")
	}
	uploadURL, videoID, err = s.provider.CreateDirectUpload(ctx, cfg, name)
	if err != nil {
		return "", "", "", err
	}
	return uploadURL, videoID, s.provider.PlaybackURL(cfg, videoID), nil
}

// SaveVideo 记录一条视频台账；播放地址由服务端按 videoId 拼，不信客户端。
func (s *service) SaveVideo(ctx context.Context, username string, req videoSaveReq) (VideoItem, error) {
	if !videoIDPattern.MatchString(req.VideoID) {
		return VideoItem{}, response.BadRequest("error.bad_request").WithMsg("videoId 不合法")
	}
	cfg, err := s.VideoConfig(ctx)
	if err != nil {
		return VideoItem{}, err
	}
	if !cfg.Enabled {
		return VideoItem{}, response.BadRequest("error.bad_request").WithMsg("视频托管未启用")
	}
	row := Video{
		Name: strings.TrimSpace(req.Name), Size: max(req.Size, 0), Provider: s.provider.Name(),
		VideoID: req.VideoID, Playback: s.provider.PlaybackURL(cfg, req.VideoID),
		Status: "ready", UploadedBy: username,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return VideoItem{}, err
	}
	return toVideoItem(row, s.provider.DownloadURL(cfg, row.VideoID)), nil
}

// Videos 按创建时间倒序返回视频台账。
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
		out = append(out, toVideoItem(r, s.provider.DownloadURL(cfg, r.VideoID)))
	}
	return out, total, nil
}

// DeleteVideo 删台账（厂商侧的资产需要在控制台清理；插件不持有其密钥作用域之外的删除权）。
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
