package maps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 假上游：三家分别断言 URL 形状（百度是 lat,lng 且带 coordtype=bd09ll）。
func TestRegeoClients(t *testing.T) {
	var gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Path + "?" + r.URL.RawQuery
		switch {
		case strings.HasPrefix(r.URL.Path, "/v3/geocode/regeo"):
			_, _ = w.Write([]byte(`{"status":"1","info":"OK","regeocode":{"formatted_address":"北京市东城区天安门广场"}}`))
		case strings.HasPrefix(r.URL.Path, "/reverse_geocoding/v3"):
			_, _ = w.Write([]byte(`{"status":0,"result":{"formatted_address":"北京市东城区天安门广场"}}`))
		case strings.HasPrefix(r.URL.Path, "/maps/api/geocode/json"):
			_, _ = w.Write([]byte(`{"status":"OK","results":[{"formatted_address":"Tiananmen Square, Beijing"}]}`))
		}
	}))
	defer ts.Close()

	amap := &amapClient{hc: ts.Client(), baseURL: ts.URL}
	if addr, err := amap.Regeo(context.Background(), "k", 116.397428, 39.90923); err != nil || addr == "" {
		t.Fatalf("amap: %v %q", err, addr)
	}
	if !strings.Contains(gotQuery, "location=116.397428,39.909230") {
		t.Fatalf("amap query: %s", gotQuery)
	}

	baidu := &baiduClient{hc: ts.Client(), baseURL: ts.URL}
	if addr, err := baidu.Regeo(context.Background(), "k", 116.404, 39.915); err != nil || addr == "" {
		t.Fatalf("baidu: %v %q", err, addr)
	}
	if !strings.Contains(gotQuery, "location=39.915000,116.404000") || !strings.Contains(gotQuery, "coordtype=bd09ll") {
		t.Fatalf("baidu query (must be lat,lng bd09): %s", gotQuery)
	}

	google := &googleClient{hc: ts.Client(), baseURL: ts.URL}
	if addr, err := google.Regeo(context.Background(), "k", 116.397, 39.909); err != nil || addr == "" {
		t.Fatalf("google: %v %q", err, addr)
	}
	if !strings.Contains(gotQuery, "latlng=39.909000,116.397000") {
		t.Fatalf("google query (lat,lng): %s", gotQuery)
	}
}

// 上游错误要透出厂商自己的原因。
func TestRegeoErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v3/geocode/regeo"):
			_, _ = w.Write([]byte(`{"status":"0","info":"INVALID_USER_KEY"}`))
		case strings.HasPrefix(r.URL.Path, "/reverse_geocoding/v3"):
			_, _ = w.Write([]byte(`{"status":2,"message":"AK参数不存在"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"REQUEST_DENIED","error_message":"API key invalid"}`))
		}
	}))
	defer ts.Close()

	if _, err := (&amapClient{hc: ts.Client(), baseURL: ts.URL}).Regeo(context.Background(), "k", 116, 39); err == nil || !strings.Contains(err.Error(), "INVALID_USER_KEY") {
		t.Fatalf("amap err: %v", err)
	}
	if _, err := (&baiduClient{hc: ts.Client(), baseURL: ts.URL}).Regeo(context.Background(), "k", 116, 39); err == nil || !strings.Contains(err.Error(), "AK参数不存在") {
		t.Fatalf("baidu err: %v", err)
	}
	if _, err := (&googleClient{hc: ts.Client(), baseURL: ts.URL}).Regeo(context.Background(), "k", 116, 39); err == nil || !strings.Contains(err.Error(), "API key invalid") {
		t.Fatalf("google err: %v", err)
	}
}

// 越界坐标在 service 入口就被拒绝，不打上游。
func TestRegeoRangeGuard(t *testing.T) {
	s := &service{amap: &amapClient{hc: http.DefaultClient, baseURL: "http://127.0.0.1:1"}, baidu: &baiduClient{hc: http.DefaultClient}, google: &googleClient{hc: http.DefaultClient}}
	for _, c := range [][2]float64{{181, 0}, {-181, 0}, {0, 91}, {0, -91}} {
		if _, err := s.Regeo(context.Background(), ProviderAMap, c[0], c[1]); err == nil {
			t.Errorf("lng=%v lat=%v should be rejected", c[0], c[1])
		}
	}
}
