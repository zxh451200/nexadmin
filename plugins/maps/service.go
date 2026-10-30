package maps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

type setting struct {
	Key   string `gorm:"column:config_key;primaryKey"`
	Value string `gorm:"column:config_value"`
}

func (setting) TableName() string { return "plg_maps_setting" }

// Provider 是地图厂商标识，与前端 provider id 一致。
type Provider string

const (
	ProviderAMap   Provider = "amap"
	ProviderBaidu  Provider = "baidu"
	ProviderGoogle Provider = "google"
)

// ProviderConfig 是单家厂商的配置。服务端专用 Key 永远不下发浏览器。
type ProviderConfig struct {
	Enabled bool
	// 浏览器端用的 Key：高德 JS Key / 百度浏览器端 AK / 谷歌 API Key
	PublicKey string
	// 服务端用的 Key：高德 Web 服务 Key / 百度服务端 AK / 谷歌（与 PublicKey 相同）
	ServerKey string
}

type Config map[Provider]ProviderConfig

type ProviderView struct {
	Enabled      bool   `json:"enabled"`
	PublicKey    string `json:"publicKey"`
	HasServerKey bool   `json:"hasServerKey"`
}

type ConfigView struct {
	AMap   ProviderView `json:"amap"`
	Baidu  ProviderView `json:"baidu"`
	Google ProviderView `json:"google"`
}

type providerReq struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"publicKey" binding:"max=200"`
	ServerKey string `json:"serverKey" binding:"max=200"`
}

type configReq struct {
	AMap   providerReq `json:"amap"`
	Baidu  providerReq `json:"baidu"`
	Google providerReq `json:"google"`
}

var settingKeys = map[Provider][2]string{
	ProviderAMap:   {"maps.amap.enabled", "maps.amap.public_key"},
	ProviderBaidu:  {"maps.baidu.enabled", "maps.baidu.public_key"},
	ProviderGoogle: {"maps.google.enabled", "maps.google.public_key"},
}

func serverKeyName(p Provider) string {
	switch p {
	case ProviderAMap:
		return "maps.amap.server_key"
	case ProviderBaidu:
		return "maps.baidu.server_key"
	case ProviderGoogle:
		return "maps.google.server_key"
	}
	return ""
}

type service struct {
	db     *gorm.DB
	amap   *amapClient
	baidu  *baiduClient
	google *googleClient
}

func newService(db *gorm.DB) *service {
	hc := &http.Client{Timeout: 8 * time.Second}
	return &service{
		db:     db,
		amap:   &amapClient{hc: hc, baseURL: "https://restapi.amap.com"},
		baidu:  &baiduClient{hc: hc, baseURL: "https://api.map.baidu.com"},
		google: &googleClient{hc: hc, baseURL: "https://maps.googleapis.com"},
	}
}

func (s *service) Config(ctx context.Context) (Config, error) {
	var rows []setting
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	get := func(p Provider) ProviderConfig {
		keys := settingKeys[p]
		return ProviderConfig{Enabled: m[keys[0]] == "true", PublicKey: m[keys[1]], ServerKey: m[serverKeyName(p)]}
	}
	return Config{ProviderAMap: get(ProviderAMap), ProviderBaidu: get(ProviderBaidu), ProviderGoogle: get(ProviderGoogle)}, nil
}

func view(c Config) ConfigView {
	pv := func(p Provider) ProviderView {
		cfg := c[p]
		return ProviderView{Enabled: cfg.Enabled, PublicKey: cfg.PublicKey, HasServerKey: cfg.ServerKey != ""}
	}
	return ConfigView{AMap: pv(ProviderAMap), Baidu: pv(ProviderBaidu), Google: pv(ProviderGoogle)}
}

// SaveConfig 保存三家配置；服务端 Key 留空表示沿用旧值（前端永远不回显）。
func (s *service) SaveConfig(ctx context.Context, in configReq) (ConfigView, error) {
	cur, err := s.Config(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	next := Config{}
	for p, req := range map[Provider]providerReq{ProviderAMap: in.AMap, ProviderBaidu: in.Baidu, ProviderGoogle: in.Google} {
		pub := strings.TrimSpace(req.PublicKey)
		server := strings.TrimSpace(req.ServerKey)
		if server == "" {
			server = cur[p].ServerKey
		}
		if p == ProviderGoogle && server == "" {
			server = pub
		}
		next[p] = ProviderConfig{Enabled: req.Enabled, PublicKey: pub, ServerKey: server}
		if req.Enabled && (pub == "" || server == "") {
			return ConfigView{}, badRequest("maps.key_required")
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for p, cfg := range next {
			keys := settingKeys[p]
			for k, v := range map[string]string{
				keys[0]: boolText(cfg.Enabled), keys[1]: cfg.PublicKey, serverKeyName(p): cfg.ServerKey,
			} {
				if err := tx.Save(&setting{Key: k, Value: v}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return ConfigView{}, err
	}
	return view(next), nil
}

// Regeo 逆地理编码：按厂商分派；坐标校验在入口统一做。
func (s *service) Regeo(ctx context.Context, provider Provider, lng, lat float64) (string, error) {
	if math.IsNaN(lng) || math.IsNaN(lat) || lng < -180 || lng > 180 || lat < -90 || lat > 90 {
		return "", badRequest("maps.coords_invalid")
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return "", err
	}
	pc := cfg[provider]
	if !pc.Enabled || pc.ServerKey == "" {
		return "", badRequest("maps.not_enabled")
	}
	switch provider {
	case ProviderAMap:
		return s.amap.Regeo(ctx, pc.ServerKey, lng, lat)
	case ProviderBaidu:
		return s.baidu.Regeo(ctx, pc.ServerKey, lng, lat)
	case ProviderGoogle:
		return s.google.Regeo(ctx, pc.ServerKey, lng, lat)
	}
	return "", badRequest("maps.provider_unknown")
}

// ---- 各厂商 Web 服务客户端；baseURL 可替换，测试用假上游 ----

type amapClient struct {
	hc      *http.Client
	baseURL string
}

func (c *amapClient) Regeo(ctx context.Context, key string, lng, lat float64) (string, error) {
	u := fmt.Sprintf("%s/v3/geocode/regeo?key=%s&location=%.6f,%.6f", strings.TrimSuffix(c.baseURL, "/"), url.QueryEscape(key), lng, lat)
	var out struct {
		Status    string `json:"status"`
		Info      string `json:"info"`
		Regeocode struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"regeocode"`
	}
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return "", err
	}
	if out.Status != "1" {
		msg := out.Info
		if msg == "" {
			msg = "未知错误"
		}
		return "", errors.New("高德地图: " + msg)
	}
	return out.Regeocode.FormattedAddress, nil
}

type baiduClient struct {
	hc      *http.Client
	baseURL string
}

// 百度要求 BD-09 坐标，且 location 参数是「纬度,经度」——与其它厂商相反。
func (c *baiduClient) Regeo(ctx context.Context, key string, lng, lat float64) (string, error) {
	u := fmt.Sprintf("%s/reverse_geocoding/v3/?ak=%s&output=json&coordtype=bd09ll&location=%.6f,%.6f",
		strings.TrimSuffix(c.baseURL, "/"), url.QueryEscape(key), lat, lng)
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Result  struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"result"`
	}
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return "", err
	}
	if out.Status != 0 {
		msg := out.Message
		if msg == "" {
			msg = fmt.Sprintf("status=%d", out.Status)
		}
		return "", errors.New("百度地图: " + msg)
	}
	return out.Result.FormattedAddress, nil
}

type googleClient struct {
	hc      *http.Client
	baseURL string
}

func (c *googleClient) Regeo(ctx context.Context, key string, lng, lat float64) (string, error) {
	u := fmt.Sprintf("%s/maps/api/geocode/json?latlng=%.6f,%.6f&key=%s", strings.TrimSuffix(c.baseURL, "/"), lat, lng, url.QueryEscape(key))
	var out struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
		Results      []struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"results"`
	}
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return "", err
	}
	if out.Status != "OK" || len(out.Results) == 0 {
		msg := out.ErrorMessage
		if msg == "" {
			msg = out.Status
		}
		return "", errors.New("谷歌地图: " + msg)
	}
	return out.Results[0].FormattedAddress, nil
}

func getJSON(ctx context.Context, hc *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("地图服务返回无法解析（HTTP %d）", res.StatusCode)
	}
	return nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
