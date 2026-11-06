package maps

// 三家厂商的地理编码（地址 → 坐标）与 POI 关键字检索。
// 返回坐标均为厂商原生坐标系（amap=gcj02、baidu=bd09、google=wgs84）——
// 前端选点地图与厂商一一对应，结果直接落在同厂商的地图上，无需转换。

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// queryEscape 是 url.QueryEscape 的短别名，拼接厂商查询串时省点视觉噪音。
func queryEscape(s string) string { return url.QueryEscape(s) }

func (c *amapClient) Geocode(ctx context.Context, key, address string) (float64, float64, error) {
	var out struct {
		Status   string `json:"status"`
		Info     string `json:"info"`
		Geocodes []struct {
			Location string `json:"location"` // "lng,lat"
		} `json:"geocodes"`
	}
	u := fmt.Sprintf("%s/v3/geocode/geo?key=%s&address=%s", strings.TrimSuffix(c.baseURL, "/"), queryEscape(key), queryEscape(address))
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return 0, 0, err
	}
	if out.Status != "1" || len(out.Geocodes) == 0 {
		return 0, 0, upstreamErr("高德地图", out.Status, out.Info, "未找到该地址")
	}
	lng, lat, err := parseLngLat(out.Geocodes[0].Location)
	if err != nil {
		return 0, 0, err
	}
	return lng, lat, nil
}

func (c *amapClient) Search(ctx context.Context, key, keywords, city string) ([]Place, error) {
	var out struct {
		Status string `json:"status"`
		Info   string `json:"info"`
		POIs   []struct {
			Name     string `json:"name"`
			Address  string `json:"address"`
			Location string `json:"location"`
		} `json:"pois"`
	}
	u := fmt.Sprintf("%s/v3/place/text?key=%s&keywords=%s&city=%s&offset=10&page=1",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(key), queryEscape(keywords), queryEscape(city))
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return nil, err
	}
	if out.Status != "1" {
		return nil, upstreamErr("高德地图", out.Status, out.Info, "")
	}
	places := make([]Place, 0, len(out.POIs))
	for _, p := range out.POIs {
		lng, lat, err := parseLngLat(p.Location)
		if err != nil {
			continue // 单条坐标异常跳过，不拖垮整批
		}
		places = append(places, Place{Name: p.Name, Address: p.Address, Lng: lng, Lat: lat})
	}
	return places, nil
}

func (c *baiduClient) Geocode(ctx context.Context, key, address string) (float64, float64, error) {
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Result  struct {
			Location struct {
				Lng float64 `json:"lng"`
				Lat float64 `json:"lat"`
			} `json:"location"`
		} `json:"result"`
	}
	u := fmt.Sprintf("%s/geocoding/v3/?ak=%s&output=json&address=%s",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(key), queryEscape(address))
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return 0, 0, err
	}
	if out.Status != 0 {
		return 0, 0, upstreamErr("百度地图", strconv.Itoa(out.Status), out.Message, "")
	}
	return out.Result.Location.Lng, out.Result.Location.Lat, nil
}

func (c *baiduClient) Search(ctx context.Context, key, keywords, city string) ([]Place, error) {
	if strings.TrimSpace(city) == "" {
		city = "全国"
	}
	var out struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
		Results []struct {
			Name     string `json:"name"`
			Address  string `json:"address"`
			Location struct {
				Lng float64 `json:"lng"`
				Lat float64 `json:"lat"`
			} `json:"location"`
		} `json:"results"`
	}
	u := fmt.Sprintf("%s/place/v2/search?ak=%s&output=json&query=%s&region=%s&pageSize=10&page_num=1",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(key), queryEscape(keywords), queryEscape(city))
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return nil, err
	}
	if out.Status != 0 {
		return nil, upstreamErr("百度地图", strconv.Itoa(out.Status), out.Message, "")
	}
	places := make([]Place, 0, len(out.Results))
	for _, r := range out.Results {
		places = append(places, Place{Name: r.Name, Address: r.Address, Lng: r.Location.Lng, Lat: r.Location.Lat})
	}
	return places, nil
}

func (c *googleClient) Geocode(ctx context.Context, key, address string) (float64, float64, error) {
	var out struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
		Results      []struct {
			Geometry struct {
				Location struct {
					Lng float64 `json:"lng"`
					Lat float64 `json:"lat"`
				} `json:"location"`
			} `json:"geometry"`
		} `json:"results"`
	}
	u := fmt.Sprintf("%s/maps/api/geocode/json?address=%s&key=%s",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(address), queryEscape(key))
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return 0, 0, err
	}
	if out.Status != "OK" || len(out.Results) == 0 {
		return 0, 0, upstreamErr("谷歌地图", out.Status, out.ErrorMessage, "未找到该地址")
	}
	g := out.Results[0].Geometry.Location
	return g.Lng, g.Lat, nil
}

func (c *googleClient) Search(ctx context.Context, key, keywords string) ([]Place, error) {
	var out struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
		Results      []struct {
			Name     string `json:"name"`
			Address  string `json:"formatted_address"`
			Geometry struct {
				Location struct {
					Lng float64 `json:"lng"`
					Lat float64 `json:"lat"`
				} `json:"location"`
			} `json:"geometry"`
		} `json:"results"`
	}
	u := fmt.Sprintf("%s/maps/api/place/textsearch/json?query=%s&key=%s",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(keywords), queryEscape(key))
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return nil, err
	}
	if out.Status != "OK" {
		return nil, upstreamErr("谷歌地图", out.Status, out.ErrorMessage, "")
	}
	places := make([]Place, 0, len(out.Results))
	for _, r := range out.Results {
		places = append(places, Place{Name: r.Name, Address: r.Address, Lng: r.Geometry.Location.Lng, Lat: r.Geometry.Location.Lat})
	}
	return places, nil
}

// parseLngLat 解析高德 "lng,lat" 形态的坐标串。
func parseLngLat(s string) (float64, float64, error) {
	lngS, latS, ok := strings.Cut(strings.TrimSpace(s), ",")
	if !ok {
		return 0, 0, fmt.Errorf("maps: 坐标串非法 %q", s)
	}
	lng, err1 := strconv.ParseFloat(strings.TrimSpace(lngS), 64)
	lat, err2 := strconv.ParseFloat(strings.TrimSpace(latS), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("maps: 坐标串非法 %q", s)
	}
	return lng, lat, nil
}

// upstreamErr 把厂商错误收敛成统一形态：厂商名 + 状态 + 明细 + 可选的业务缺省提示。
func upstreamErr(vendor, status, detail, fallback string) error {
	msg := detail
	if msg == "" {
		msg = status
	}
	if msg == "" && fallback != "" {
		msg = fallback
	}
	if msg == "" {
		msg = "服务返回异常"
	}
	return fmt.Errorf("%s: %s", vendor, msg)
}
