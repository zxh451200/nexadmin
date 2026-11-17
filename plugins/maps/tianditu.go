package maps

// 天地图（国家地理信息公共服务平台）客户端：实名认证后申请 tk，浏览器 JS SDK 与
// 服务端 Web API 用同一个 tk（与谷歌同款，服务端 Key 缺省沿用浏览器 Key）。
// 坐标系 CGCS2000，与 WGS-84 差异在厘米级，前端按 wgs84 落点、无需转换。
// 文档：https://lbs.tianditu.gov.cn/server/geocoding.html 与 .../server/search2.html

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type tiandituClient struct {
	hc      *http.Client
	baseURL string
}

// flexFloat 兼容天地图把坐标回成数字或字符串两种形态（搜索接口 POI 的 lon/lat 是字符串）。
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("maps: 天地图坐标串非法 %q", s)
	}
	*f = flexFloat(v)
	return nil
}

// Regeo 逆地理编码：lat/lon → 完整地址串。/geocoder?postStr={"lon":..,"lat":..,"ver":1}；
// 天地图用 status "0" 表示成功（"1" 是失败），与高德 "1" 成功相反。
func (c *tiandituClient) Regeo(ctx context.Context, key string, lng, lat float64) (string, error) {
	postStr := fmt.Sprintf(`{"lon":%.6f,"lat":%.6f,"ver":1}`, lng, lat)
	u := fmt.Sprintf("%s/geocoder?postStr=%s&tk=%s",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(postStr), queryEscape(key))
	var out struct {
		Status           string `json:"status"`
		Msg              string `json:"msg"`
		FormattedAddress string `json:"formatted_address"`
	}
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return "", err
	}
	if out.Status != "0" {
		return "", upstreamErr("天地图", out.Status, out.Msg, "")
	}
	if out.FormattedAddress == "" {
		return "", upstreamErr("天地图", "", "", "未找到该位置的地址")
	}
	return out.FormattedAddress, nil
}

// Geocode 地址 → 坐标：/geocoder?ds={"keyWord":"…"}，命中回 {"location":{"lon":..,"lat":..}}。
func (c *tiandituClient) Geocode(ctx context.Context, key, address string) (float64, float64, error) {
	ds, _ := json.Marshal(map[string]string{"keyWord": address})
	u := fmt.Sprintf("%s/geocoder?ds=%s&tk=%s",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(string(ds)), queryEscape(key))
	var out struct {
		Status   string `json:"status"`
		Msg      string `json:"msg"`
		Location struct {
			Lng flexFloat `json:"lon"`
			Lat flexFloat `json:"lat"`
		} `json:"location"`
	}
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return 0, 0, err
	}
	if out.Status != "0" {
		return 0, 0, upstreamErr("天地图", out.Status, out.Msg, "未找到该地址")
	}
	return float64(out.Location.Lng), float64(out.Location.Lat), nil
}

// Search POI 关键字检索：/search?postStr={"keyWord":..,"queryType":"1",..}。
// city 参数天地图 V2.0 不支持（没有区域过滤），忽略（与谷歌/OSM 同款处理）。
func (c *tiandituClient) Search(ctx context.Context, key, keywords string) ([]Place, error) {
	postStr, _ := json.Marshal(map[string]any{
		"keyWord": keywords, "level": 12, "map": "vec", "queryType": "1", "start": 0, "count": 10,
	})
	u := fmt.Sprintf("%s/search?postStr=%s&tk=%s",
		strings.TrimSuffix(c.baseURL, "/"), queryEscape(string(postStr)), queryEscape(key))
	var out struct {
		// 请求无效时天地图可能回 {"msg":"…"}（没有 searchResult 包裹）；成功回 searchResult.pois
		Msg          string `json:"msg"`
		SearchResult struct {
			POIs []struct {
				Name    string    `json:"name"`
				Address string    `json:"address"`
				Lon     flexFloat `json:"lon"`
				Lat     flexFloat `json:"lat"`
			} `json:"pois"`
		} `json:"searchResult"`
	}
	if err := getJSON(ctx, c.hc, u, &out); err != nil {
		return nil, err
	}
	pois := out.SearchResult.POIs
	if len(pois) == 0 {
		if out.Msg != "" {
			return nil, upstreamErr("天地图", "", out.Msg, "")
		}
		return []Place{}, nil
	}
	places := make([]Place, 0, len(pois))
	for _, p := range pois {
		places = append(places, Place{Name: p.Name, Address: p.Address, Lng: float64(p.Lon), Lat: float64(p.Lat)})
	}
	return places, nil
}
