package pay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/response"
)

// 微信支付渠道补齐的测试：平台证书自动下载与轮换、退款结果查询、渠道账单、
// 代付/商家转账、回调取证。手法与 providers_test.go 一致——httptest 起一个 APIv3
// 桩网关，渠道配置里 gateway 指向它，凭据用本地生成的商户 RSA 密钥与自签「平台证书」。

const wechatTestAPIKey = "0123456789abcdef0123456789abcdef" // 32 位测试 APIv3 密钥

// ---- 桩网关 ----

type wechatGWReply struct {
	status int
	body   string
}

type wechatGWCert struct {
	pem string
	// serial 是 /v3/certificates 应答里 advertise 的 serial_no；留空则用证书自身序列号
	// （正常路径）。测试填一个不一致的值，用来证明「证书列表被动过手脚」会被拒。
	serial     string
	expireTime time.Time
}

// wechatGW 是微信支付 APIv3 桩网关：
//   - GET /v3/certificates：按「当前证书列表」应答，每张证书的 PEM 用 APIv3 密钥
//     AES-256-GCM 加密；测试可以中途加证书来模拟平台证书轮换。
//   - GET /v3/billdownload/file：返回放好的账单字节（gzip，只给 Content-Type 不给
//     Content-Encoding，与微信一致：渠道摘要算的是压缩原文）。
//   - 其他路径：按注册的前缀应答，并统一带平台证书签名头（含非 2xx）。
type wechatGW struct {
	t       *testing.T
	apiKey  []byte
	srv     *httptest.Server
	cfg     map[string]string
	mu      sync.Mutex
	replies map[string]wechatGWReply
	certs   []wechatGWCert
	bill    []byte

	platKey *rsa.PrivateKey // 当前签应答/回调的平台私钥
	serial  string          // 对应的证书序列号（大写 hex）

	certCalls int
	requests  []string
	bodies    map[string]string
	billHTTP  int
}

func newWechatGW(t *testing.T, mch string) *wechatGW {
	t.Helper()
	wechatCertCacheReset()
	t.Cleanup(wechatCertCacheReset)
	_, priv, _ := testRSA(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &wechatGW{
		t:       t,
		apiKey:  []byte(wechatTestAPIKey),
		replies: map[string]wechatGWReply{},
		bodies:  map[string]string{},
		platKey: key,
		serial:  "PUB_KEY_ID_UNUSED",
	}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	g.cfg = map[string]string{
		"appId": "wx0000000000000001", "mchId": mch, "serialNo": "MCHSERIAL0001",
		"privateKey": priv, "apiV3Key": wechatTestAPIKey, "gateway": g.srv.URL,
	}
	return g
}

// usePlatform 装上当前的平台证书：桩网关之后都用它的私钥签名，并把这张证书列进列表。
func (g *wechatGW) usePlatform(certPEM, serial string, key *rsa.PrivateKey) {
	g.platKey, g.serial = key, serial
	g.mu.Lock()
	defer g.mu.Unlock()
	g.certs = append(g.certs, wechatGWCert{pem: certPEM, expireTime: time.Now().AddDate(1, 0, 0)})
}

// addCert 只把证书加进列表、不改签名用的私钥：用于「微信开始下发新序列号」的轮换场景。
func (g *wechatGW) addCert(certPEM, serial string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.certs = append(g.certs, wechatGWCert{pem: certPEM, serial: serial, expireTime: time.Now().AddDate(1, 0, 0)})
}

func (g *wechatGW) replaceCerts(certs ...wechatGWCert) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.certs = certs
}

func (g *wechatGW) setReply(methodPath, body string) {
	g.setReplyStatus(methodPath, http.StatusOK, body)
}

func (g *wechatGW) setReplyStatus(methodPath string, status int, body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.replies[methodPath] = wechatGWReply{status: status, body: body}
}

func (g *wechatGW) setBill(file []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bill = file
}

func (g *wechatGW) setBillHTTP(status int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.billHTTP = status
}

func (g *wechatGW) certCallsN() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.certCalls
}

func (g *wechatGW) body(methodPath string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.bodies[methodPath]
}

func (g *wechatGW) askedFor(substr string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.requests {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return false
}

func (g *wechatGW) requestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.requests)
}

func (g *wechatGW) serve(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	g.mu.Lock()
	g.requests = append(g.requests, key+"?"+r.URL.RawQuery)
	g.bodies[key] = string(raw)

	if r.URL.Path == "/v3/certificates" {
		g.certCalls++
		out := g.certListJSONLocked()
		g.mu.Unlock()
		g.respond(w, http.StatusOK, out)
		return
	}
	if r.URL.Path == "/v3/billdownload/file" {
		file, status := g.bill, g.billHTTP
		g.mu.Unlock()
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"code":"FAIL","message":"账单文件下载失败"}`))
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(file)
		return
	}
	rep, found := wechatGWReply{}, false
	for prefix, v := range g.replies {
		if strings.HasPrefix(key, prefix) {
			rep, found = v, true
			break
		}
	}
	g.mu.Unlock()
	if !found {
		g.respond(w, http.StatusNotFound, `{"code":"RESOURCE_NOT_FOUND","message":"桩网关没有这条路由"}`)
		return
	}
	g.respond(w, rep.status, rep.body)
}

func (g *wechatGW) respond(w http.ResponseWriter, status int, body string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "0123456789abcdef"
	sig := rsaSignSHA256(g.platKey, ts+"\n"+nonce+"\n"+body+"\n")
	h := w.Header()
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", nonce)
	h.Set("Wechatpay-Signature", sig)
	h.Set("Wechatpay-Serial", g.serial)
	h.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func (g *wechatGW) certListJSONLocked() string {
	items := make([]map[string]any, 0, len(g.certs))
	for i, c := range g.certs {
		nonce := fmt.Sprintf("certnonce%02dx", i) // 12 字节，AES-GCM 要求的 nonce 长度
		serial := c.serial
		if serial == "" {
			serial = wechatSerialHexOfPEM(g.t, c.pem) // 正常路径：报的就是证书自身序列号
		}
		items = append(items, map[string]any{
			"serial_no":      serial,
			"effective_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
			"expire_time":    c.expireTime.Format(time.RFC3339),
			"encrypt_certificate": map[string]any{
				"algorithm":       "AEAD_AES_256_GCM",
				"nonce":           nonce,
				"associated_data": "certificate",
				"ciphertext":      wechatGCMSeal(g.apiKey, "certificate", nonce, c.pem),
			},
		})
	}
	out, err := json.Marshal(map[string]any{"certificates": items})
	if err != nil {
		g.t.Fatal(err)
	}
	return string(out)
}

// ---- 测试小工具 ----

func wechatGCMSeal(key []byte, aad, nonce, plain string) string {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte(nonce), []byte(plain), []byte(aad)))
}

// wechatTestPlatformCert 自签一张平台证书，返回 PEM、大写十六进制序列号与私钥。
func wechatTestPlatformCert(t *testing.T, serial int64, notAfter time.Time) (string, string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{Organization: []string{"WeChatPay Test Platform"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return certPEM, strings.ToUpper(big.NewInt(serial).Text(16)), key
}

func wechatSerialHexOfPEM(t *testing.T, certPEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("测试证书 PEM 解析失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return wechatCertSerialHex(cert)
}

// wechatNotify 组一条已签名回调：resource 明文用 APIv3 密钥加密，整包用平台私钥签名。
// eventID 留空模拟报文没有 id；skew 用来做过期时间戳。
func wechatNotify(t *testing.T, key *rsa.PrivateKey, serial, eventID, eventType, plain, apiKey string, skew time.Duration) CallbackInput {
	t.Helper()
	nonce := "notifynonce1" // 12 字节，AES-256-GCM 要求的 nonce 长度
	msg := map[string]any{
		"event_type": eventType,
		"resource": map[string]any{
			"associated_data": "resource",
			"nonce":           nonce,
			"ciphertext":      wechatGCMSeal([]byte(apiKey), "resource", nonce, plain),
		},
	}
	if eventID != "" {
		msg["id"] = eventID
	}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	ts := strconv.FormatInt(time.Now().Add(skew).Unix(), 10)
	rnd := "0123456789abcdef"
	return CallbackInput{
		Channel: "wechat",
		Body:    body,
		Headers: map[string]string{
			"Wechatpay-Timestamp": ts,
			"Wechatpay-Nonce":     rnd,
			"Wechatpay-Signature": rsaSignSHA256(key, ts+"\n"+rnd+"\n"+string(body)+"\n"),
			"Wechatpay-Serial":    serial,
		},
	}
}

const wechatPaidPlain = `{"out_trade_no":"PAY-1","transaction_id":"4200000123202611011234567890",` +
	`"trade_state":"SUCCESS","amount":{"total":1234}}`

func wechatGzip(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveBill 把账单按微信的形状挂上桩：/v3/bill/tradebill 给 download_url+SHA1 摘要，
// 摘要算的是压缩原文；tamperHash=true 时故意给一个不符的摘要。
func serveBill(g *wechatGW, csv string, tamperHash bool) {
	g.t.Helper()
	file := wechatGzip(g.t, csv)
	g.setBill(file)
	sum := fmt.Sprintf("%x", sha1.Sum(file))
	if tamperHash {
		sum = strings.Repeat("0", 40)
	}
	body := fmt.Sprintf(`{"hash_type":"SHA1","hash_value":%q,"download_url":%q}`,
		sum, g.srv.URL+"/v3/billdownload/file?token=TOKEN1")
	g.setReply("GET /v3/bill/tradebill", body)
}

func wechatBillRequest() BillRequest { return BillRequest{BillDate: "2026-10-31"} }

// ---- A. 平台证书自动下载、缓存与轮换 ----

func TestWechatPlatformCertAutoDownloadAndRotate(t *testing.T) {
	g := newWechatGW(t, "1900000101")
	certA, serialA, keyA := wechatTestPlatformCert(t, 0x11AA, time.Now().AddDate(0, 0, 30))
	g.usePlatform(certA, serialA, keyA)

	// 配置里没有手工平台公钥 → 必须走自动下载，并用下载到的证书验签。
	res, err := (wechatProvider{}).VerifyCallback(
		wechatNotify(t, keyA, serialA, "EV-1001", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg)
	if err != nil {
		t.Fatalf("自动下载的平台证书没能验签: %v", err)
	}
	if res.PaymentNo != "PAY-1" || !res.Paid || res.AmountMinor != 1234 || res.NotifyID != "EV-1001" {
		t.Fatalf("unexpected result %+v", res)
	}
	if got := g.certCallsN(); got != 1 {
		t.Fatalf("平台证书应只下载一次，实际 %d 次", got)
	}

	// 缓存命中：同序列号的第二条回调不再打 /v3/certificates。
	if _, err := (wechatProvider{}).VerifyCallback(
		wechatNotify(t, keyA, serialA, "EV-1002", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg); err != nil {
		t.Fatalf("缓存里的证书拒绝了合法回调: %v", err)
	}
	if got := g.certCallsN(); got != 1 {
		t.Fatalf("缓存命中却重新下载了，实际 %d 次", got)
	}

	// 轮换：微信开始用新证书 B 签名（序列号不在缓存里）→ 未命中触发恰好一次重下载 → 命中。
	certB, serialB, keyB := wechatTestPlatformCert(t, 0x22BB, time.Now().AddDate(1, 0, 0))
	g.platKey, g.serial = keyB, serialB
	g.addCert(certB, serialB)
	if _, err := (wechatProvider{}).VerifyCallback(
		wechatNotify(t, keyB, serialB, "EV-1003", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg); err != nil {
		t.Fatalf("轮换后的新序列号未命中应重下载并成功: %v", err)
	}
	if got := g.certCallsN(); got != 2 {
		t.Fatalf("序列号未命中应触发恰好一次重下载，实际 %d 次", got)
	}

	// 应答头没有 Wechatpay-Serial：自动模式下无法选证书，必须拒绝（不存在不验签路径）。
	in := wechatNotify(t, keyB, serialB, "EV-1004", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0)
	delete(in.Headers, "Wechatpay-Serial")
	if _, err := (wechatProvider{}).VerifyCallback(in, g.cfg); err == nil {
		t.Fatal("缺少 Wechatpay-Serial 的回调被放过了")
	} else if !strings.Contains(err.Error(), "Wechatpay-Serial") {
		t.Fatalf("错误应说明缺序列号: %v", err)
	}

	// 请求路径（非回调）也走同一套证书选择：伪造应答必须被拒。
	foreign, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g.setReply("GET /v3/pay/transactions/out-trade-no", `{"trade_state":"SUCCESS","transaction_id":"4200000009"}`)
	if _, err := (wechatProvider{}).Query(context.Background(), "PAY-1", g.cfg); err != nil {
		t.Fatalf("应答验签用自动下载的证书应通过: %v", err)
	}
	g.platKey = foreign
	if _, err := (wechatProvider{}).Query(context.Background(), "PAY-1", g.cfg); err == nil {
		t.Fatal("外来私钥签名的网关应答被接受了（删掉应答验签这条必红）")
	}
}

func TestWechatCertDownloadConfigAndTamperErrors(t *testing.T) {
	t.Run("缺 apiV3Key 报配置错误且不联网", func(t *testing.T) {
		g := newWechatGW(t, "1900000102")
		cert, serial, key := wechatTestPlatformCert(t, 0x33CC, time.Now().AddDate(0, 0, 30))
		g.usePlatform(cert, serial, key)
		delete(g.cfg, "apiV3Key")
		_, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-1", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg)
		if err == nil {
			t.Fatal("没有 APIv3 密钥却验签通过了")
		}
		if !strings.Contains(err.Error(), "apiV3Key") {
			t.Fatalf("错误应指向 apiV3Key 配置: %v", err)
		}
		if g.certCallsN() != 0 || g.requestCount() != 0 {
			t.Fatalf("凭据不全时不该打网关，实际下载 %d 次、请求 %d 次", g.certCallsN(), g.requestCount())
		}
	})

	t.Run("APIv3 密钥不是同一把 → GCM 解密失败，不降级", func(t *testing.T) {
		g := newWechatGW(t, "1900000103")
		cert, serial, key := wechatTestPlatformCert(t, 0x44DD, time.Now().AddDate(0, 0, 30))
		g.usePlatform(cert, serial, key)
		g.cfg["apiV3Key"] = "ffffffffffffffffffffffffffffffff" // 32 位，但不是同一把
		_, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-2", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg)
		if err == nil {
			t.Fatal("用别的 APIv3 密钥解出来的证书列表被接受了")
		}
		if !strings.Contains(err.Error(), "解密失败") {
			t.Fatalf("错误应说明证书解密失败: %v", err)
		}
		if g.certCallsN() != 1 {
			t.Fatalf("应尝试下载一次证书，实际 %d 次", g.certCallsN())
		}
	})

	t.Run("缺 mchId 时明确报配置错误", func(t *testing.T) {
		g := newWechatGW(t, "1900000107")
		cert, serial, key := wechatTestPlatformCert(t, 0x77CC, time.Now().AddDate(0, 0, 30))
		g.usePlatform(cert, serial, key)
		g.cfg["mchId"] = "  "
		_, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg)
		if err == nil || !strings.Contains(err.Error(), "mchId") {
			t.Fatalf("错误应指向 mchId 配置: %v", err)
		}
	})

	t.Run("列表 serial_no 与证书自身不一致 → 拒绝", func(t *testing.T) {
		g := newWechatGW(t, "1900000104")
		cert, serial, key := wechatTestPlatformCert(t, 0x55EE, time.Now().AddDate(0, 0, 30))
		g.usePlatform(cert, serial, key)
		g.replaceCerts(wechatGWCert{pem: cert, serial: "DEADBEEF", expireTime: time.Now().AddDate(1, 0, 0)})
		_, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-3", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg)
		if err == nil {
			t.Fatal("serial_no 与证书自身序列号不一致的列表被接受了")
		}
		if !strings.Contains(err.Error(), "不一致") {
			t.Fatalf("错误应指出证书序列号不一致: %v", err)
		}
	})

	t.Run("刚过期的证书仍然可用（在架回调还是用它签的）", func(t *testing.T) {
		g := newWechatGW(t, "1900000105")
		cert, serial, key := wechatTestPlatformCert(t, 0x66FF, time.Now().Add(-time.Hour))
		g.usePlatform(cert, serial, key)
		if _, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-4", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), g.cfg); err != nil {
			t.Fatalf("刚过期的平台证书不该直接被拒: %v", err)
		}
	})
}

// ---- A(兜底). 手工平台公钥仍然优先 ----

func TestWechatManualPlatformKeyFallback(t *testing.T) {
	g := newWechatGW(t, "1900000106")
	cert, serial, key := wechatTestPlatformCert(t, 0x77AA, time.Now().AddDate(0, 0, 30))
	g.usePlatform(cert, serial, key)

	manual := func() map[string]string {
		cfg := map[string]string{}
		for k, v := range g.cfg {
			cfg[k] = v
		}
		cfg["platformPublicKey"] = cert
		cfg["platformPublicKeyId"] = serial
		return cfg
	}

	t.Run("手工公钥生效且不下载证书", func(t *testing.T) {
		res, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-5", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), manual())
		if err != nil {
			t.Fatalf("手工公钥验签失败: %v", err)
		}
		if res.NotifyID != "EV-5" {
			t.Fatalf("回调取证号没落进 NotifyID: %+v", res)
		}
		if g.certCallsN() != 0 {
			t.Fatalf("手工公钥优先，不该下载证书，实际 %d 次", g.certCallsN())
		}
	})

	t.Run("手工公钥 ID 与报文序列号不一致要报出来", func(t *testing.T) {
		cfg := manual()
		cfg["platformPublicKeyId"] = "PUB_KEY_ID_0000000001"
		if _, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-6", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), cfg); err == nil {
			t.Fatal("公钥 ID 不一致的回调被放过了")
		}
	})

	t.Run("手工公钥坏了报配置错误", func(t *testing.T) {
		cfg := manual()
		cfg["platformPublicKey"] = "不是 PEM 也不是 Base64"
		_, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), cfg)
		if err == nil || !strings.Contains(err.Error(), "不可用") {
			t.Fatalf("手工公钥非法要报配置错误: %v", err)
		}
	})

	// 负向控制：删掉「rsaVerifySHA256」这一句，本用例必红。
	t.Run("外来私钥签着同一个序列号必须被拒", func(t *testing.T) {
		foreign, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, foreign, serial, "EV-7", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0), manual()); err == nil {
			t.Fatal("外来私钥签名的回调验签通过了")
		}
	})

	t.Run("超出 5 分钟硬窗要拒", func(t *testing.T) {
		if _, err := (wechatProvider{}).VerifyCallback(
			wechatNotify(t, key, serial, "EV-8", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, -6*time.Minute),
			manual()); err == nil {
			t.Fatal("过期 6 分钟的回调被放过了")
		}
	})

	t.Run("金额被改一个字符就过不了验签", func(t *testing.T) {
		in := wechatNotify(t, key, serial, "EV-9", "TRANSACTION.SUCCESS", wechatPaidPlain, wechatTestAPIKey, 0)
		in.Body = []byte(strings.Replace(string(in.Body), wechatGCMSeal([]byte(wechatTestAPIKey), "resource",
			"notifynonce1", wechatPaidPlain), wechatGCMSeal([]byte(wechatTestAPIKey), "resource",
			"notifynonce1", strings.Replace(wechatPaidPlain, `"total":1234`, `"total":1`, 1)), 1))
		if _, err := (wechatProvider{}).VerifyCallback(in, manual()); err == nil {
			t.Fatal("改过密文金额的回调还能过验签")
		}
	})
}

// ---- B. 能力矩阵 ----

func TestWechatCapabilities(t *testing.T) {
	caps := ProviderCaps(wechatProvider{})
	if len(caps.Currencies) != 1 || caps.Currencies[0] != "CNY" {
		t.Fatalf("微信只收 CNY，实际 %v", caps.Currencies)
	}
	if got := strings.Join(caps.Methods, ","); got != "native,jsapi,h5,app" {
		t.Fatalf("methods = %s", got)
	}
	if !caps.RefundQuery || !caps.Bill || !caps.Payout {
		t.Fatalf("本轮补齐的能力必须如实声明为支持: %+v", caps)
	}
	// Auth=false 是刻意的：微信支付没有通用开放的资金预授权接口。
	if caps.Auth {
		t.Fatal("微信不应声明支持资金预授权")
	}
	var asAny any = wechatProvider{}
	if _, ok := asAny.(AuthProvider); ok {
		t.Fatal("微信实现了 AuthProvider，服务层的 errCapabilityUnsupported 路径会被绕过")
	}
	if caps.supportsCurrency("USD") || !caps.supportsCurrency("cny") || !caps.supportsCurrency("") {
		t.Fatal("币种能力判定与「只 CNY」不一致")
	}
	if !caps.supportsMethod("native") || caps.supportsMethod("micropay") {
		t.Fatal("下单方式能力判定不对")
	}
}

// ---- C. 退款结果查询 ----

func TestWechatQueryRefundBranches(t *testing.T) {
	g := newWechatGW(t, "1900000201")
	cert, serial, key := wechatTestPlatformCert(t, 0x81AA, time.Now().AddDate(0, 0, 30))
	g.usePlatform(cert, serial, key)

	cases := []struct {
		name       string
		status     int
		body       string
		wantFound  bool
		wantOK     bool
		wantPend   bool
		wantReason string
		wantID     string
	}{
		{
			name: "SUCCESS 收敛为成功", wantFound: true, wantOK: true, wantID: "50000123",
			body: `{"refund_id":"50000123","out_refund_no":"IDK-1","status":"SUCCESS"}`, wantReason: "SUCCESS",
		},
		{
			name: "PROCESSING 仍是 pending", wantFound: true, wantPend: true, wantID: "50000124",
			body: `{"refund_id":"50000124","out_refund_no":"IDK-1","status":"PROCESSING"}`, wantReason: "PROCESSING",
		},
		{
			name: "ABNORMAL 渠道侧异常", wantFound: true, wantID: "50000125",
			body: `{"refund_id":"50000125","out_refund_no":"IDK-1","status":"ABNORMAL"}`, wantReason: "ABNORMAL",
		},
		{
			name: "CLOSED 渠道侧关闭", wantFound: true,
			body: `{"refund_id":"","out_refund_no":"IDK-1","status":"CLOSED"}`, wantReason: "CLOSED",
		},
		{
			name: "查无此退款", status: http.StatusNotFound, wantReason: "查无",
			body: `{"code":"RESOURCE_NOT_FOUND","message":"退款单不存在"}`,
		},
		{
			name: "没见过的状态不下结论", wantFound: true, wantPend: true,
			body: `{"refund_id":"50000126","out_refund_no":"IDK-1","status":"SOMETHING_NEW"}`, wantReason: "SOMETHING_NEW",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := c.status
			if status == 0 {
				status = http.StatusOK
			}
			g.setReplyStatus("GET /v3/refund/domestic/refunds", status, c.body)
			got, err := (wechatProvider{}).QueryRefund(context.Background(), RefundQueryRequest{
				RefundNo: "R-1", PaymentNo: "PAY-1", IdempotencyKey: "IDK-1",
			}, g.cfg)
			if err != nil {
				t.Fatalf("%s 不该是错误路径: %v", c.name, err)
			}
			if got.Found != c.wantFound || got.OK != c.wantOK || got.Pending != c.wantPend {
				t.Fatalf("unexpected %+v", got)
			}
			if !strings.Contains(got.Reason, c.wantReason) {
				t.Fatalf("Reason 要带渠道原始结论 %q，实际 %q", c.wantReason, got.Reason)
			}
			if c.wantID != "" && got.ChannelRefundID != c.wantID {
				t.Fatalf("ChannelRefundID = %q", got.ChannelRefundID)
			}
			// 必须用发起退款时同一个 out_refund_no，否则微信认不出是同一笔。
			if !g.askedFor("/v3/refund/domestic/refunds/IDK-1") {
				t.Fatalf("查询没走 out_refund_no=IDK-1")
			}
		})
	}

	t.Run("IdempotencyKey 缺失退回 RefundNo", func(t *testing.T) {
		g.setReply("GET /v3/refund/domestic/refunds", `{"status":"SUCCESS"}`)
		if _, err := (wechatProvider{}).QueryRefund(context.Background(), RefundQueryRequest{RefundNo: "R-9"}, g.cfg); err != nil {
			t.Fatal(err)
		}
		if !g.askedFor("/v3/refund/domestic/refunds/R-9") {
			t.Fatal("缺 IdempotencyKey 时应用 RefundNo 兜底")
		}
	})

	t.Run("两个单号都空是配置错误，不打网关", func(t *testing.T) {
		before := g.requestCount()
		if _, err := (wechatProvider{}).QueryRefund(context.Background(), RefundQueryRequest{}, g.cfg); err == nil {
			t.Fatal("没有退款单号也能查询")
		}
		if g.requestCount() != before {
			t.Fatal("非法入参不该产生网关调用")
		}
	})

	t.Run("渠道 5xx 是错误而不是查无", func(t *testing.T) {
		g.setReplyStatus("GET /v3/refund/domestic/refunds", http.StatusInternalServerError,
			`{"code":"SYSTEM_ERROR","message":"系统繁忙"}`)
		_, err := (wechatProvider{}).QueryRefund(context.Background(), RefundQueryRequest{IdempotencyKey: "IDK-1"}, g.cfg)
		if err == nil {
			t.Fatal("系统错误被吞掉了")
		}
		var re *response.Error
		if !errors.As(err, &re) || re.Status != http.StatusBadGateway {
			t.Fatalf("渠道故障应是 PAY_CHANNEL_ERROR(502): %v", err)
		}
		if !strings.Contains(err.Error(), "SYSTEM_ERROR") {
			t.Fatalf("渠道原始 code 要能排障: %v", err)
		}
	})
}

// ---- D. 账单下载与解析 ----

// 表头（每列都带微信防 Excel 科学计数法的 ` 前缀）。
const wechatBillHeader = "`商户订单号,`微信支付订单号,`交易状态,`订单金额,`手续费金额,`商户退款单号,`微信退款单号,`退款金额"

func wechatBillCSV() string {
	return "\ufeff#微信支付-商户交易账单\n" +
		"#----------------------" + strings.Repeat("-", 40) + "\n" +
		"#导出时间：2026-11-01 10:00:00\n" +
		wechatBillHeader + "\n" +
		// 千分位金额必须带引号，才能不被当成两列。
		"`PAY-1,`4200000001,`SUCCESS,`\"1,234.56\",`1.23,,,\n" +
		// 交易行同时带退款列：微信交易账单的形状（退款附在原单行上）。
		"`PAY-2,`4200000002,`SUCCESS,`12.00,`0.12,`RF-2,`5000000002,`2.00\n" +
		// 明确没成功的交易不进对账。
		"`PAY-3,`4200000003,`CLOSED,`8.00,`0.00,,,\n" +
		"#汇总,,,`1,254.56,`1.35,,,\n"
}

func TestWechatFetchBill(t *testing.T) {
	g := newWechatGW(t, "1900000301")
	cert, serial, key := wechatTestPlatformCert(t, 0x91AA, time.Now().AddDate(0, 0, 30))
	g.usePlatform(cert, serial, key)
	csv := wechatBillCSV()

	t.Run("正常账单：按表头列名定位、元→分、Digest 是账单文本的 sha256", func(t *testing.T) {
		serveBill(g, csv, false)
		bill, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err != nil {
			t.Fatalf("正常账单报错: %v", err)
		}
		want := []BillRow{
			{Kind: BillKindTrade, PaymentNo: "PAY-1", AmountMinor: 123456, FeeMinor: 123, ChannelTxnID: "4200000001"},
			{Kind: BillKindTrade, PaymentNo: "PAY-2", AmountMinor: 1200, FeeMinor: 12, ChannelTxnID: "4200000002"},
			{Kind: BillKindRefund, PaymentNo: "PAY-2", RefundNo: "RF-2", AmountMinor: 200, FeeMinor: 12, ChannelTxnID: "5000000002"},
		}
		if bill.RowCount != len(bill.Rows) || bill.RowCount != len(want) {
			t.Fatalf("行数 = %d，期望 %d，rows=%+v", bill.RowCount, len(want), bill.Rows)
		}
		for i := range want {
			if bill.Rows[i] != want[i] {
				t.Fatalf("第 %d 行 = %+v，期望 %+v", i, bill.Rows[i], want[i])
			}
		}
		sum := sha256.Sum256([]byte(strings.TrimPrefix(csv, "\ufeff")))
		if bill.Digest != hex.EncodeToString(sum[:]) {
			t.Fatalf("Digest 应是解析用账单文本的 sha256，实际 %s", bill.Digest)
		}
		if strings.Contains(fmt.Sprintf("%v", bill.Rows), "PAY-3") {
			t.Fatal("CLOSED 的交易不该进账单行")
		}
		if !g.askedFor("/v3/bill/tradebill?bill_date=2026-10-31&bill_type=ALL") {
			t.Fatalf("账单请求参数不对")
		}
	})

	t.Run("列顺序变了也不受影响（按下标取值的写法在这里必红）", func(t *testing.T) {
		shuffled := "#账单\n" +
			"`手续费金额,`商户订单号,`退款金额,`交易状态,`订单金额,`商户退款单号\n" +
			"`0.99,`PAY-9,,`SUCCESS,`9.90,\n"
		serveBill(g, shuffled, false)
		bill, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(bill.Rows) != 1 || bill.Rows[0] != (BillRow{Kind: BillKindTrade, PaymentNo: "PAY-9", AmountMinor: 990, FeeMinor: 99}) {
			t.Fatalf("列名定位失效: %+v", bill.Rows)
		}
	})

	t.Run("hash_value 不符必须丢弃（删掉摘要校验这条用例必红）", func(t *testing.T) {
		serveBill(g, csv, true)
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil {
			t.Fatal("摘要不符的账单被接受了")
		}
		if !errors.Is(err, errWechatBillHash) {
			t.Fatalf("错误应包着 errWechatBillHash: %v", err)
		}
		if errors.Is(err, errWechatBillNoRows) || errors.Is(err, errWechatBillDownload) {
			t.Fatalf("摘要不符不该混进下载失败/0 行: %v", err)
		}
		var re *response.Error
		if !errors.As(err, &re) || re.Status != http.StatusBadGateway {
			t.Fatalf("账单摘要失败应是 PAY_CHANNEL_ERROR(502): %v", err)
		}
	})

	t.Run("未知摘要算法同样拒绝（不校验等于没校验）", func(t *testing.T) {
		file := wechatGzip(t, csv)
		g.setBill(file)
		g.setReply("GET /v3/bill/tradebill", fmt.Sprintf(
			`{"hash_type":"SM3","hash_value":%q,"download_url":%q}`,
			fmt.Sprintf("%x", sha1.Sum(file)), g.srv.URL+"/v3/billdownload/file?token=T1"))
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil {
			t.Fatal("SM3 摘要（本渠道没实现）被当成校验通过")
		}
		if !errors.Is(err, errWechatBillHash) {
			t.Fatalf("应报摘要类错误: %v", err)
		}
	})

	t.Run("没有 hash_value 就不下载", func(t *testing.T) {
		g.setReply("GET /v3/bill/tradebill", `{"hash_type":"SHA1","download_url":"`+g.srv.URL+`/v3/billdownload/file?token=T1"}`)
		before := g.requestCount()
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil || !errors.Is(err, errWechatBillHash) {
			t.Fatalf("缺摘要的账单必须拒绝: %v", err)
		}
		if g.requestCount() != before+1 {
			t.Fatalf("只该多一次 bill/tradebill 调用，不该去下载文件: %v", g.requestCount())
		}
	})

	t.Run("解析出 0 行与下载失败是两条不同路径", func(t *testing.T) {
		serveBill(g, "#----------------------\n#合计,,,\n", false)
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil {
			t.Fatal("当天没有明细行的账单被当成成功")
		}
		if !errors.Is(err, errWechatBillNoRows) || errors.Is(err, errWechatBillDownload) {
			t.Fatalf("空账单应是 0 行路径，不该混进下载失败: %v", err)
		}

		serveBill(g, csv, false)
		g.setBillHTTP(http.StatusInternalServerError)
		_, err = (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		g.setBillHTTP(http.StatusOK)
		if err == nil {
			t.Fatal("下载失败被当成成功")
		}
		if !errors.Is(err, errWechatBillDownload) || errors.Is(err, errWechatBillNoRows) {
			t.Fatalf("下载失败应是下载类错误: %v", err)
		}
	})

	t.Run("表头认不出订单号列是格式错误", func(t *testing.T) {
		serveBill(g, "`微信支付流水,`订单金额\n`4200000001,`1.00\n", false)
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil {
			t.Fatal("没有 商户订单号 列的表头被当成可用账单")
		}
		if !errors.Is(err, errWechatBillFormat) {
			t.Fatalf("应报账单格式类错误: %v", err)
		}
	})

	t.Run("有订单号但没有金额列，落到 0 行而不是格式错误", func(t *testing.T) {
		serveBill(g, "`商户订单号,`交易状态\n`PAY-1,`SUCCESS\n", false)
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil {
			t.Fatal("读不出金额的账单被当成成功")
		}
		if !errors.Is(err, errWechatBillNoRows) {
			t.Fatalf("读不出金额应是 0 行路径: %v", err)
		}
	})

	t.Run("账单接口报错原样带出微信 code", func(t *testing.T) {
		g.setReplyStatus("GET /v3/bill/tradebill", http.StatusBadRequest,
			`{"code":"NO_BILL_EXIST","message":"当日没有账单"}`)
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil || !strings.Contains(err.Error(), "NO_BILL_EXIST") {
			t.Fatalf("渠道原始错误码要能排障: %v", err)
		}
	})

	t.Run("download_url 指向别的机器就是不可信应答", func(t *testing.T) {
		g.setReply("GET /v3/bill/tradebill",
			`{"hash_type":"SHA1","hash_value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","download_url":"https://evil.example.com/x"}`)
		_, err := (wechatProvider{}).FetchBill(context.Background(), wechatBillRequest(), g.cfg)
		if err == nil || !errors.Is(err, errWechatBillDownload) {
			t.Fatalf("愿意去下载任意 URL 的账单接口（SSRF）没被拦: %v", err)
		}
	})

	t.Run("账期日非法是配置错误，不打网关", func(t *testing.T) {
		before := g.requestCount()
		if _, err := (wechatProvider{}).FetchBill(context.Background(), BillRequest{BillDate: "2026-10-31x"}, g.cfg); err == nil {
			t.Fatal("非法账期日也去下载账单了")
		}
		if g.requestCount() != before {
			t.Fatal("非法入参不该产生网关调用")
		}
	})
}

// ---- E. 代付 / 商家转账 ----

func TestWechatTransfer(t *testing.T) {
	g := newWechatGW(t, "1900000401")
	cert, serial, key := wechatTestPlatformCert(t, 0xA1AA, time.Now().AddDate(0, 0, 30))
	g.usePlatform(cert, serial, key)

	t.Run("受理成功只算 pending", func(t *testing.T) {
		g.setReply("POST /v3/transfer/batches",
			`{"out_batch_no":"PO-1","batch_id":"103000007190","create_time":"2026-11-01T10:00:00+08:00"}`)
		res, err := (wechatProvider{}).Transfer(context.Background(), TransferRequest{
			BatchNo: "PO-1", ItemNo: "PO-1-01", AmountMinor: 5000, Currency: "CNY",
			ReceiverType: "personal", ReceiverID: "oABCD00000000000000000000000", Remark: "结算打款",
		}, g.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Accepted || !res.Pending || res.ChannelBatchNo != "103000007190" {
			t.Fatalf("受理应答只能是「已受理待到账」: %+v", res)
		}
		var sent map[string]any
		if err := json.Unmarshal([]byte(g.body("POST /v3/transfer/batches")), &sent); err != nil {
			t.Fatal(err)
		}
		if sent["out_batch_no"] != "PO-1" || sent["total_amount"] != float64(5000) || sent["total_num"] != float64(1) {
			t.Fatalf("批次字段不对: %v", sent)
		}
		details, _ := sent["transfer_detail_list"].([]any)
		if len(details) != 1 {
			t.Fatalf("明细应当只有一笔: %v", sent)
		}
		d := details[0].(map[string]any)
		if d["out_detail_no"] != "PO-1-01" || d["transfer_amount"] != float64(5000) || d["openid"] == "" {
			t.Fatalf("明细幂等号/金额/openid 不对: %v", d)
		}
	})

	t.Run("未签约转账产品原样回传渠道错误，不假成功", func(t *testing.T) {
		g.setReplyStatus("POST /v3/transfer/batches", http.StatusForbidden,
			`{"code":"NO_AUTH","message":"商户未开通转账产品权限"}`)
		res, err := (wechatProvider{}).Transfer(context.Background(), TransferRequest{
			BatchNo: "PO-2", ItemNo: "PO-2-01", AmountMinor: 5000, ReceiverID: "oABCD", Currency: "CNY",
		}, g.cfg)
		if err == nil {
			t.Fatalf("渠道拒绝被当成成功返回了: %+v", res)
		}
		var re *response.Error
		if !errors.As(err, &re) || re.Status != http.StatusBadGateway {
			t.Fatalf("渠道拒绝应是 PAY_CHANNEL_ERROR(502): %v", err)
		}
		if !strings.Contains(err.Error(), "NO_AUTH") {
			t.Fatalf("渠道原始错误码要能排障: %v", err)
		}
	})

	t.Run("入参不全在打网关之前就报错", func(t *testing.T) {
		before := g.requestCount()
		cases := []struct {
			name string
			in   TransferRequest
		}{
			{"缺 openid", TransferRequest{BatchNo: "PO-3", ItemNo: "PO-3-01", AmountMinor: 100, Currency: "CNY"}},
			{"外币", TransferRequest{BatchNo: "PO-3", ItemNo: "PO-3-01", AmountMinor: 100, ReceiverID: "oABCD", Currency: "USD"}},
			{"零金额", TransferRequest{BatchNo: "PO-3", ItemNo: "PO-3-01", AmountMinor: 0, ReceiverID: "oABCD", Currency: "CNY"}},
			{"缺批次号", TransferRequest{ItemNo: "PO-3-01", AmountMinor: 100, ReceiverID: "oABCD", Currency: "CNY"}},
			{
				// 校验收款人姓名要用平台证书公钥 RSA 加密，本渠道没实现：宁可拒绝也不明文送/悄悄丢。
				"收款人姓名", TransferRequest{BatchNo: "PO-3", ItemNo: "PO-3-01", AmountMinor: 100, ReceiverID: "oABCD", ReceiverName: "张三", Currency: "CNY"},
			},
		}
		for _, c := range cases {
			if _, err := (wechatProvider{}).Transfer(context.Background(), c.in, g.cfg); err == nil {
				t.Fatalf("%s 的转账被接受了", c.name)
			}
		}
		if g.requestCount() != before {
			t.Fatalf("入参不全不该打网关: 多了 %d 次调用", g.requestCount()-before)
		}
	})

	t.Run("按批次号查询，明细才算最终结论", func(t *testing.T) {
		g.setReply("GET /v3/transfer/batches/out-batch-no",
			`{"out_batch_no":"PO-1","batch_id":"103000007190","batch_status":"FINISHED",`+
				`"transfer_detail_list":[{"out_detail_no":"PO-1-01","detail_id":"d1","detail_status":"SUCCESS"}]}`)
		res, err := (wechatProvider{}).QueryTransfer(context.Background(), "PO-1", g.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Accepted || res.Pending || res.ChannelDetailNo != "d1" || res.State != "FINISHED" {
			t.Fatalf("到账结论不对: %+v", res)
		}
		if !g.askedFor("/v3/transfer/batches/out-batch-no/PO-1?need_query_detail=true") {
			t.Fatal("查询没带 need_query_detail=true")
		}

		g.setReply("GET /v3/transfer/batches/out-batch-no",
			`{"batch_status":"FINISHED","transfer_detail_list":[`+
				`{"out_detail_no":"PO-1-01","detail_status":"FAIL","fail_reason":"用户账号已销户"}]}`)
		res, err = (wechatProvider{}).QueryTransfer(context.Background(), "PO-1", g.cfg)
		if err != nil || res.Accepted || res.Pending || !strings.Contains(res.Reason, "销户") {
			t.Fatalf("批次结束但明细失败要报失败: %+v err=%v", res, err)
		}

		g.setReply("GET /v3/transfer/batches/out-batch-no", `{"batch_status":"PROCESSING"}`)
		res, err = (wechatProvider{}).QueryTransfer(context.Background(), "PO-1", g.cfg)
		if err != nil || !res.Pending || !res.Accepted || res.State != "PROCESSING" {
			t.Fatalf("处理中应是「已受理未到账」: %+v err=%v", res, err)
		}

		g.setReply("GET /v3/transfer/batches/out-batch-no", `{"batch_status":"FINISHED"}`)
		res, err = (wechatProvider{}).QueryTransfer(context.Background(), "PO-1", g.cfg)
		if err != nil || !res.Pending || res.Accepted {
			t.Fatalf("批次结束但没有明细时不下结论: %+v err=%v", res, err)
		}

		g.setReplyStatus("GET /v3/transfer/batches/out-batch-no", http.StatusNotFound,
			`{"code":"RESOURCE_NOT_FOUND","message":"批次不存在"}`)
		res, err = (wechatProvider{}).QueryTransfer(context.Background(), "PO-9", g.cfg)
		if err != nil || res.Accepted || res.Pending || res.State != "NOT_FOUND" {
			t.Fatalf("查无批次既不能算失败也不能算受理: %+v err=%v", res, err)
		}
		if !strings.Contains(res.Reason, "不代表失败") {
			t.Fatalf("查无语义要写清楚: %q", res.Reason)
		}
	})

	t.Run("缺批次号是配置错误，不打网关", func(t *testing.T) {
		before := g.requestCount()
		if _, err := (wechatProvider{}).QueryTransfer(context.Background(), "  ", g.cfg); err == nil {
			t.Fatal("没有批次号也能查询")
		}
		if g.requestCount() != before {
			t.Fatal("非法入参不该产生网关调用")
		}
	})
}

// ---- F. 回调取证与事件分派 ----

func TestWechatCallbackNotifyIDAndEvents(t *testing.T) {
	g := newWechatGW(t, "1900000501")
	cert, serial, key := wechatTestPlatformCert(t, 0xB1AA, time.Now().AddDate(0, 0, 30))
	g.usePlatform(cert, serial, key)

	res, err := (wechatProvider{}).VerifyCallback(
		wechatNotify(t, key, serial, "EV-REFUND-1", "REFUND.SUCCESS",
			`{"out_trade_no":"PAY-1","out_refund_no":"RF-1","refund_id":"50000999","refund_status":"SUCCESS","amount":{"refund":200}}`,
			wechatTestAPIKey, 0), g.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.NotifyID != "EV-REFUND-1" {
		t.Fatalf("退款回调的取证号没落进 NotifyID: %+v", res)
	}
	if res.RefundNo != "RF-1" || !res.RefundOK || res.GatewayStatus != "SUCCESS" || res.AmountMinor != 200 {
		t.Fatalf("退款回调结论不对: %+v", res)
	}

	// 退款异常事件：即使 refund_status 是 SUCCESS 也不能算退款成功。
	res, err = (wechatProvider{}).VerifyCallback(
		wechatNotify(t, key, serial, "EV-REFUND-2", "REFUND.ABNORMAL",
			`{"out_trade_no":"PAY-1","out_refund_no":"RF-2","refund_status":"SUCCESS"}`, wechatTestAPIKey, 0), g.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.RefundOK || res.NotifyID != "EV-REFUND-2" {
		t.Fatalf("REFUND.ABNORMAL 不该给 RefundOK: %+v", res)
	}

	// 报文没有 id 时也要有可检索指纹（列宽 128，只取证不判重）。
	res, err = (wechatProvider{}).VerifyCallback(
		wechatNotify(t, key, serial, "", "CUSTOMER.COMPLAIN", `{}`, wechatTestAPIKey, 0), g.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.NotifyID, "serial="+serial) || !strings.Contains(res.NotifyID, "ts=") ||
		!strings.Contains(res.NotifyID, "nonce=") {
		t.Fatalf("缺事件 id 时应拼 serial+ts+nonce 指纹: %+v", res)
	}
	if len(res.NotifyID) > 128 {
		t.Fatalf("NotifyID 超出列宽: %d 字符", len(res.NotifyID))
	}
	if res.PaymentNo != "" || res.Paid || res.RefundNo != "" {
		t.Fatalf("无关事件不该改资金结论: %+v", res)
	}
}
