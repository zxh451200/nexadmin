package maps

// OpenStreetMap（Nominatim）客户端：开源免费、无 Key。
// 坐标系 wgs84，与 GPS/谷歌一致。Nominatim 是公益服务：
// 使用政策要求请求带可识别的 User-Agent，公共实例限流约 1 req/s——
// 后台选点/搜索这种低频场景足够；重负载请自建 Nominatim（baseURL 可替换）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// nominatimUA 是 Nominatim 使用政策硬要求的身份标识（缺省会被 403）。
const nominatimUA = "NexAdmin Maps Plugin (server-side geocoding proxy)"

type osmClient struct {
	hc      *http.Client
	baseURL string
}

// fetchJSON 与 getJSON 同形，但额外带 User-Agent——Nominatim 没有它直接拒服务。
func (c *osmClient) fetchJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", nominatimUA)
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	// Nominatim 出错时可能回 {"error": "..."}（HTTP 200），也可能回纯文本——统一按结构体尽力解析
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("OpenStreetMap: 服务返回无法解析（HTTP %d）", res.StatusCode)
	}
	return nil
}

// Regeo 逆地理编码：lat/lon → 完整地址串。
func (c *osmClient) Regeo(ctx context.Context, lng, lat float64) (string, error) {
	var out struct {
		Error       string `json:"error"`
		DisplayName string `json:"display_name"`
	}
	u := fmt.Sprintf("%s/reverse?format=jsonv2&lat=%.6f&lon=%.6f&zoom=18&addressdetails=1",
		strings.TrimSuffix(c.baseURL, "/"), lat, lng)
	if err := c.fetchJSON(ctx, u, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", upstreamErr("OpenStreetMap", "", out.Error, "")
	}
	if out.DisplayName == "" {
		return "", upstreamErr("OpenStreetMap", "", "", "未找到该位置的地址")
	}
	return out.DisplayName, nil
}

// osmPlace 是 /search 的一条结果；lat/lon 是字符串（Nominatim 的 JSON 惯例）。
type osmPlace struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
}

func (p osmPlace) toPlace() (Place, error) {
	lat, err1 := strconv.ParseFloat(p.Lat, 64)
	lng, err2 := strconv.ParseFloat(p.Lon, 64)
	if err1 != nil || err2 != nil {
		return Place{}, fmt.Errorf("maps: OSM 坐标串非法 %q/%q", p.Lat, p.Lon)
	}
	// name 为空（纯地址匹配）时拿 display_name 兜底当标题，地址留空避免整串重复
	name := p.Name
	if name == "" {
		name = p.DisplayName
		return Place{Name: name, Lng: lng, Lat: lat}, nil
	}
	return Place{Name: name, Address: p.DisplayName, Lng: lng, Lat: lat}, nil
}

func (c *osmClient) search(ctx context.Context, keywords string, limit int) ([]Place, error) {
	var out []osmPlace
	u := fmt.Sprintf("%s/search?q=%s&format=jsonv2&addressdetails=1&limit=%d",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(keywords), limit)
	if err := c.fetchJSON(ctx, u, &out); err != nil {
		return nil, err
	}
	places := make([]Place, 0, len(out))
	for _, p := range out {
		place, err := p.toPlace()
		if err != nil {
			continue // 单条坐标异常跳过，不拖垮整批
		}
		places = append(places, place)
	}
	return places, nil
}

// Search POI 关键字检索；city 参数 Nominatim 不支持，忽略（与谷歌同款处理）。
func (c *osmClient) Search(ctx context.Context, keywords string) ([]Place, error) {
	return c.search(ctx, keywords, 10)
}

// Geocode 地址 → 坐标：取首条命中。
func (c *osmClient) Geocode(ctx context.Context, address string) (float64, float64, error) {
	places, err := c.search(ctx, address, 1)
	if err != nil {
		return 0, 0, err
	}
	if len(places) == 0 {
		return 0, 0, upstreamErr("OpenStreetMap", "", "", "未找到该地址")
	}
	return places[0].Lng, places[0].Lat, nil
}
