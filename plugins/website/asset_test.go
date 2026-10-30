package website

import "testing"

// 静态资源的 ?v= 是内容哈希：文件一改地址就变，浏览器不必手动硬刷新。
// 这里守三件事——长度正确、同进程内稳定、缺失文件不至于让地址变成 "?v="。
func TestAssetVersionIsContentHash(t *testing.T) {
	v := assetVersion("site.css")
	if len(v) != 16 {
		t.Fatalf("site.css 版本号 = %q，期望 16 位十六进制", v)
	}
	if again := assetVersion("site.css"); again != v {
		t.Fatalf("同一进程内两次取值不一致：%q vs %q", v, again)
	}
	if js := assetVersion("site.js"); js == v {
		t.Errorf("site.css 与 site.js 的版本号相同（%q），正常情况内容不同应不同", js)
	}
	if missing := assetVersion("no-such-file.css"); missing != "" {
		t.Errorf("缺失文件应返回空串，得到 %q", missing)
	}
}
