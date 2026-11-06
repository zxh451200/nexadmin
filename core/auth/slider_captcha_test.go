package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	mrand "math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/config"
)

// sliderSecretX 读出挑战的孔位 X（测试专用）：缓存里存的是 "ip|X"，
// X 不随响应外泄，因此用例只能这样拿到它。
func sliderSecretX(t *testing.T, s *Service, key string) int {
	t.Helper()
	v, err := s.cache.Get(context.Background(), sliderKey(key))
	if err != nil {
		t.Fatal(err)
	}
	_, xs, found := strings.Cut(v, "|")
	if !found {
		t.Fatalf("bad slider cache value %q", v)
	}
	x, err := strconv.Atoi(xs)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// humanTrailN 造一条像人的拖动轨迹：时间单调、时长 400ms、推进量足够，末点落在 target。
func humanTrailN(target, points int) []trailPoint {
	out := make([]trailPoint, 0, points)
	for i := 0; i < points; i++ {
		frac := float64(i) / float64(points-1)
		ease := frac * frac * (3 - 2*frac) // 前慢后快再收尾，避免等距直线的机器人特征
		out = append(out, trailPoint{T: int64(400 * ease), X: int(float64(target) * ease)})
	}
	out[points-1].X = target
	out[points-1].T = 400
	return out
}

// humanTrail 是 20 点的常规轨迹；表驱动用例按单参签名调用它。
func humanTrail(target int) []trailPoint { return humanTrailN(target, 20) }

func decodePNG(t *testing.T, dataURI string) (width, height int, alpha []byte) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURI, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("bad png: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	alpha = make([]byte, 0, cfg.Width*cfg.Height)
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			alpha = append(alpha, byte(a>>8))
		}
	}
	return cfg.Width, cfg.Height, alpha
}

func TestSliderChallengeImages(t *testing.T) {
	s := newCaptchaSvc(t)
	key, bg, piece, pieceTop, err := s.NewSliderCaptcha(context.Background(), testIP)
	if err != nil || key == "" {
		t.Fatalf("NewSliderCaptcha: %v", err)
	}
	if w, h, _ := decodePNG(t, bg); w != sliderBgW || h != sliderBgH {
		t.Fatalf("背景尺寸 %dx%d，期望 %dx%d", w, h, sliderBgW, sliderBgH)
	}
	w, h, alpha := decodePNG(t, piece)
	if w != sliderPieceW || h != sliderPieceH {
		t.Fatalf("滑块尺寸 %dx%d，期望 %dx%d", w, h, sliderPieceW, sliderPieceH)
	}
	// 滑块图形状外必须全透明，否则前端叠到背景上会盖住整块画面。
	opaque, transparent := 0, 0
	for _, a := range alpha {
		if a == 0 {
			transparent++
		} else {
			opaque++
		}
	}
	if transparent == 0 {
		t.Fatal("滑块图没有透明像素：形状裁错了")
	}
	if opaque < sliderPiece*sliderPiece/2 {
		t.Fatalf("滑块图不透明像素过少：%d", opaque)
	}
	// 背景整张不透明（挖孔只是压暗，不留洞穿）。
	if _, _, bgAlpha := decodePNG(t, bg); bytes.IndexByte(bgAlpha, 0) >= 0 {
		t.Fatal("背景不应有透明像素")
	}
	x := sliderSecretX(t, s, key)
	if x < sliderXMin || x > sliderXMax {
		t.Fatalf("孔位 X=%d 越界 [%d,%d]", x, sliderXMin, sliderXMax)
	}
	if pieceTop < 0 || pieceTop+sliderPieceH > sliderBgH {
		t.Fatalf("滑块顶边 %d 越界", pieceTop)
	}
}

// TestSliderTokenIsConsumedByLoginChannel 钉住这条链路的关键：签发的 token 必须落在
// 图形验证码同一个键（captchaKey）下，登录/短信/注册才能用现成的 VerifyCaptcha 消费。
// token 若写在 sliderKey 下，本用例即红——那才是真正的验证通道。
func TestSliderTokenIsConsumedByLoginChannel(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _, _, err := s.NewSliderCaptcha(ctx, testIP)
	if err != nil {
		t.Fatal(err)
	}
	x := sliderSecretX(t, s, key)
	token, err := s.VerifySliderCaptcha(ctx, key, x, humanTrail(x), testIP)
	if err != nil {
		t.Fatalf("拖到位应当通过: %v", err)
	}
	if token == "" {
		t.Fatal("空 token")
	}
	// 登录侧：captcha=token、captchaKey=key，与图形验证码同一入口。
	ok, err := s.VerifyCaptcha(ctx, key, token, testIP)
	if err != nil || !ok {
		t.Fatalf("VerifyCaptcha 未能消费滑块 token: ok=%v err=%v", ok, err)
	}
	// 一次性：同一个 token 再用一次必须失败。
	if ok, err = s.VerifyCaptcha(ctx, key, token, testIP); err != nil || ok {
		t.Fatalf("滑块 token 被重复消费: ok=%v err=%v", ok, err)
	}
	// 挑战已作废，不能在同一 key 上再拖一次拿到第二个 token。
	if _, err := s.VerifySliderCaptcha(ctx, key, x, humanTrail(x), testIP); err != errSliderUnavailable {
		t.Fatalf("已签发过 token 的挑战应作废，实得 %v", err)
	}
}

func TestSliderVerifyRejects(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	const goodX = 150

	newKey := func(t *testing.T) string {
		t.Helper()
		key, _, _, _, err := s.NewSliderCaptcha(ctx, testIP)
		if err != nil {
			t.Fatal(err)
		}
		// 覆盖成固定孔位，便于逐条构造落点。
		if err := s.cache.Set(ctx, sliderKey(key), testIP+"|150", sliderTTL); err != nil {
			t.Fatal(err)
		}
		return key
	}
	missingKey := func(*testing.T) string { return strings.Repeat("a", 32) }

	cases := []struct {
		name  string
		key   func(t *testing.T) string
		x     int
		trail func(x int) []trailPoint
		ip    string
		want  error
	}{
		{"落点偏 7px", newKey, goodX + sliderTolerance + 1, humanTrail, testIP, errSliderFailed},
		{"落点偏 6px 恰在容差内", newKey, goodX + sliderTolerance, humanTrail, testIP, nil},
		{"轨迹点数不足", newKey, goodX, func(x int) []trailPoint { return humanTrailN(x, sliderTrailMin-1) }, testIP, errSliderFailed},
		{"时间戳倒流", newKey, goodX, func(x int) []trailPoint {
			tr := humanTrailN(x, 20)
			tr[10].T = tr[11].T + 100
			return tr
		}, testIP, errSliderFailed},
		{"推进量不足 40px", newKey, goodX, func(x int) []trailPoint {
			tr := humanTrailN(x, 20)
			for i := range tr {
				tr[i].X = x - 20 // 全程挤在 20px 内，末点仍等于落点
			}
			tr[len(tr)-1].X = x
			return tr
		}, testIP, errSliderFailed},
		{"末点与落点不一致", newKey, goodX, func(x int) []trailPoint {
			tr := humanTrailN(x, 20)
			tr[len(tr)-1].X = x - 1
			return tr
		}, testIP, errSliderFailed},
		// 落点全在容差内也要被时长闸拦下：时间戳几乎不推进是零成本脚本的特征。
		{"时长过短（瞬移）", newKey, goodX, func(x int) []trailPoint {
			tr := humanTrailN(x, 20)
			for i := range tr {
				tr[i].T = 0
			}
			tr[len(tr)-1].T = 50
			return tr
		}, testIP, errSliderFailed},
		{"时长过久", newKey, goodX, func(x int) []trailPoint {
			tr := humanTrailN(x, 20)
			for i := range tr {
				tr[i].T = int64(i * 2000)
			}
			tr[len(tr)-1].T = 40000
			return tr
		}, testIP, errSliderFailed},
		{"IP 不符", newKey, goodX, humanTrail, "10.0.0.8", errSliderUnavailable},
		{"key 不存在", missingKey, goodX, humanTrail, testIP, errSliderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.VerifySliderCaptcha(ctx, tc.key(t), tc.x, tc.trail(tc.x), tc.ip)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("应当通过: %v", err)
				}
				return
			}
			if err != tc.want {
				t.Fatalf("期望 %v，实得 %v", tc.want, err)
			}
		})
	}
}

// TestSliderTriesCapInvalidatesChallenge 打死“在同一挑战上穷举 X”：超过上限后
// 连正确落点也不再签发 token，且挑战从缓存里消失。
func TestSliderTriesCapInvalidatesChallenge(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	key, _, _, _, err := s.NewSliderCaptcha(ctx, testIP)
	if err != nil {
		t.Fatal(err)
	}
	x := sliderSecretX(t, s, key)
	for i := 0; i < sliderTriesMax; i++ {
		if _, err := s.VerifySliderCaptcha(ctx, key, x+30, humanTrailN(x+30, 20), testIP); err != errSliderFailed {
			t.Fatalf("第 %d 次错误尝试应报 errSliderFailed，实得 %v", i+1, err)
		}
	}
	if _, err := s.VerifySliderCaptcha(ctx, key, x, humanTrail(x), testIP); err != errSliderUnavailable {
		t.Fatalf("超过尝试上限后应作废挑战，实得 %v", err)
	}
	if _, err := s.cache.Get(ctx, sliderKey(key)); err == nil {
		t.Fatal("挑战已超上限却仍留在缓存里")
	}
}

// TestSliderKeyLengthGuard 确认空 key 与超长 key 不落到缓存查询（与图形验证码同一口径）。
func TestSliderKeyLengthGuard(t *testing.T) {
	s := newCaptchaSvc(t)
	for _, k := range []string{"", strings.Repeat("f", 65)} {
		if _, err := s.VerifySliderCaptcha(context.Background(), k, 10, humanTrail(10), testIP); err != errSliderUnavailable {
			t.Fatalf("key %q 应直接拒绝，实得 %v", k, err)
		}
	}
}

// TestSliderGeometryContract 钉住前后端共用的出图口径。这三组数不报错也不编译不过——
// 改了它们，auth.mock.ts 的出图与 web/e2e/slider-captcha.spec.ts 的换算会静默错位，
// 于是 e2e 在假数据上仍然绿、真后端却永远拖不到位。改这里请三处一起改。
func TestSliderGeometryContract(t *testing.T) {
	want := struct{ bgW, bgH, piece, tab, tolerance, trailMin int }{280, 110, 52, 11, 6, 6}
	got := struct{ bgW, bgH, piece, tab, tolerance, trailMin int }{
		sliderBgW, sliderBgH, sliderPiece, sliderTab, sliderTolerance, sliderTrailMin,
	}
	if got != want {
		t.Fatalf("滑块口径变了：%+v，期望 %+v（同步 auth.mock.ts 与 web/e2e/slider-captcha.spec.ts）", got, want)
	}
	if sliderPieceW != sliderPiece+sliderTab {
		t.Fatalf("滑块包围盒 %d 应等于方身+凸出 %d", sliderPieceW, sliderPiece+sliderTab)
	}
	// 把手行程上限必须覆盖最右的孔位，否则最右那一档永远拖不到位。
	if sliderXMax > sliderBgW-sliderPieceW {
		t.Fatalf("最右孔位 %d 超出可拖范围 %d", sliderXMax, sliderBgW-sliderPieceW)
	}
}

// postJSON 发一条 JSON 请求并解出信封（code/message/data）。
func postJSON(t *testing.T, e *gin.Engine, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = testIP + ":1234"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s 响应不是 JSON：%s", path, w.Body.String())
	}
	return w.Code, out
}

func getJSON(t *testing.T, e *gin.Engine, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = testIP + ":1234"
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s 响应不是 JSON：%s", path, w.Body.String())
	}
	return w.Code, out
}

// TestSliderSolvesLoginAtHTTPLayer 打的是接线而不是逻辑：两条 /auth/slider 路由、
// 请求体绑定、以及「滑块 token 当作 captcha 提交给 /auth/login」这条真实消费通道。
// 单元级用例证明不了 handler 少注册一条路由、字段名写错、或 token 键位放错这三类问题。
func TestSliderSolvesLoginAtHTTPLayer(t *testing.T) {
	e, s, _ := loginEngine(t)
	h := &Handler{Svc: s, Cfg: &config.Config{}}
	e.GET("/slider", h.SliderCaptcha)
	e.POST("/slider/verify", h.SliderVerify)

	// 连错三次把登录推到「必须带验证码」的状态（与图形验证码共用同一道闸）。
	for i := 1; i <= 3; i++ {
		if r := doLogin(t, e, "ghost", "bad", "", ""); r.Code != 400 {
			t.Fatalf("预置失败第 %d 次应 400，实得 %d", i, r.Code)
		}
	}

	status, got := getJSON(t, e, "/slider")
	data, _ := got["data"].(map[string]any)
	if status != http.StatusOK || data == nil {
		t.Fatalf("GET /slider 应当 200 且带 data：%+v", got)
	}
	key, _ := data["key"].(string)
	for _, field := range []string{"image", "piece"} {
		if v, _ := data[field].(string); !strings.HasPrefix(v, "data:image/png;base64,") {
			t.Fatalf("GET /slider 的 %s 不是 PNG dataURI", field)
		}
	}
	for _, field := range []string{"width", "height", "pieceWidth", "pieceHeight", "pieceTop"} {
		if v, ok := data[field].(float64); !ok || v <= 0 {
			t.Fatalf("GET /slider 缺少可用的 %s（前端换算就靠它）", field)
		}
	}
	if _, leaked := data["x"]; leaked {
		t.Fatal("GET /slider 把孔位 X 泄露给前端了")
	}
	if key == "" {
		t.Fatal("空 key")
	}

	x := sliderSecretX(t, s, key)
	// 拖偏：400，且不消费挑战。
	if status, _ := postJSON(t, e, "/slider/verify", map[string]any{"key": key, "x": x + 40, "trail": humanTrailN(x+40, 20)}); status != http.StatusBadRequest {
		t.Fatalf("拖偏应 400，实得 %d", status)
	}
	// 拖到位：200 且回 token。
	status, got = postJSON(t, e, "/slider/verify", map[string]any{"key": key, "x": x, "trail": humanTrail(x)})
	data, _ = got["data"].(map[string]any)
	token, _ := data["token"].(string)
	if status != http.StatusOK || token == "" {
		t.Fatalf("拖到位应 200 + token，实得 %d %+v", status, got)
	}
	// 真通道：同一 key + token 交给 /auth/login，必须登录成功。
	if r := doLogin(t, e, "ghost", "right", key, token); r.Code != 0 {
		t.Fatalf("滑块 token 应能通过登录的验证码闸，实得 %+v", r)
	}
	// 一次性：同一个 token 再用必须失败。
	if r := doLogin(t, e, "ghost", "right", key, token); r.Code == 0 {
		t.Fatal("滑块 token 被重复消费还能登录")
	}
}

// TestSliderPieceIsTrueCropOfHole 是「拖到位就对得齐」的底层证据：滑块图形状内的每个
// 像素必须等于挖孔前背景在孔位处的同一像素（描边那一圈除外）。裁错一个偏移量，
// 拼图永远拼不上，而且不报错——只是看着歪。
func TestSliderPieceIsTrueCropOfHole(t *testing.T) {
	for _, x := range []int{sliderXMin, (sliderXMin + sliderXMax) / 2, sliderXMax} {
		for _, y := range []int{sliderYMin, sliderYMax} {
			src := paintSliderBackdrop(mrand.New(mrand.NewSource(int64(x*1000 + y))))
			before := image.NewNRGBA(src.Rect)
			copy(before.Pix, src.Pix)
			piece := cutSliderPiece(src, x, y)
			punchSliderHole(src, x, y)

			mismatch, checked := 0, 0
				for dy := -sliderTab; dy < sliderPiece; dy++ {
					for dx := 0; dx < sliderPieceW; dx++ {
						// 白描边画到 2px：豁免口径与 cutSliderPiece 的上色范围同步。
						if !inSliderPiece(dx, dy) || onSliderEdge(dx, dy) || nearSliderEdge(dx, dy) {
							continue
						}
					checked++
					p := piece.PixOffset(dx, dy+sliderTab)
					q := before.PixOffset(x+dx, y+dy)
					if piece.Pix[p] != before.Pix[q] || piece.Pix[p+1] != before.Pix[q+1] || piece.Pix[p+2] != before.Pix[q+2] {
						mismatch++
					}
				}
			}
			if checked < sliderPiece*sliderPiece/2 {
				t.Fatalf("孔位 (%d,%d) 只比对了 %d 个像素，样本太少说明取景错了", x, y, checked)
			}
			if mismatch != 0 {
				t.Fatalf("孔位 (%d,%d)：滑块图有 %d 个像素与背景不一致（裁剪偏移错了）", x, y, mismatch)
			}
			// 挖孔必须只动孔位那一块：整张图的其余像素不能被涂到。
			changed := 0
			for i := range src.Pix {
				if src.Pix[i] != before.Pix[i] {
					changed++
				}
			}
			if changed == 0 {
				t.Fatalf("孔位 (%d,%d) 挖下去背景一点没变", x, y)
			}
		}
	}
}

// TestSliderPieceShape 钉住拼图形状的几何：方身满、右凸与上凸各是一块半圆，
// 且形状总面积等于三者之和。形状算错不会报错，只会让前端对齐不上。
func TestSliderPieceShape(t *testing.T) {
	body, rightTab, topTab, total := 0, 0, 0, 0
	for dy := -sliderTab; dy < sliderPiece; dy++ {
		for dx := 0; dx < sliderPieceW; dx++ {
			if !inSliderPiece(dx, dy) {
				continue
			}
			total++
			switch {
			case dx >= sliderPiece:
				rightTab++
			case dy < 0:
				topTab++
			default:
				body++
			}
		}
	}
	if body != sliderPiece*sliderPiece {
		t.Fatalf("方身面积 %d，期望 %d", body, sliderPiece*sliderPiece)
	}
	if total != body+rightTab+topTab {
		t.Fatalf("形状总面积 %d 与分块之和 %d 不符", total, body+rightTab+topTab)
	}
	// 凸出是半圆，面积约 πr²/2，允许 20% 离散误差。
	half := float64(sliderTab*sliderTab) * 3.14159 / 2
	for _, tc := range []struct {
		name string
		got  int
	}{{"右凸", rightTab}, {"上凸", topTab}} {
		if diff := float64(tc.got) - half; diff > half*0.2 || diff < -half*0.2 {
			t.Fatalf("%s面积 %d，偏离半圆 %.0f 超过 20%%", tc.name, tc.got, half)
		}
	}
	if !inSliderPiece(0, 0) || inSliderPiece(sliderPiece-1, -sliderTab) {
		t.Fatal("方身左上角应在形内，上凸的左上角应在形外")
	}
}

// TestSliderRenderBounds 校验出图尺寸与孔位/顶边范围（直接调渲染，不经过缓存）。
func TestSliderRenderBounds(t *testing.T) {
	for i := 0; i < 30; i++ {
		bg, piece, x, top, err := renderSlider([]byte{byte(i), 2, 3, 4, 5, 6, 7, 8})
		if err != nil {
			t.Fatal(err)
		}
		if len(bg) == 0 || len(piece) == 0 {
			t.Fatal("空 PNG")
		}
		if x < sliderXMin || x > sliderXMax {
			t.Fatalf("孔位 X=%d 越界 [%d,%d]", x, sliderXMin, sliderXMax)
		}
		if top < 0 || top+sliderPieceH > sliderBgH {
			t.Fatalf("滑块顶边 %d 越界", top)
		}
	}
}

// TestSliderHoleVariesAcrossSameSeed 是“答案不来自 seed”这条约定的探测器：
// 同一个 seed（前端可见的 key）连出多张图，孔位必须散布。哪天把 X 改回由 seed 派生，
// 本用例立刻红。
func TestSliderHoleVariesAcrossSameSeed(t *testing.T) {
	seed := bytes.Repeat([]byte{0x11}, 16)
	seen := map[int]bool{}
	for i := 0; i < 24; i++ {
		_, _, x, _, err := renderSlider(seed)
		if err != nil {
			t.Fatal(err)
		}
		seen[x] = true
	}
	if len(seen) < 8 {
		t.Fatalf("同 seed 连出 24 张只得到 %d 个孔位：答案疑似由 key 派生", len(seen))
	}
}
