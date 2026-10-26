package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPass = "correct horse battery"

func noEnv(string) string { return "" }

// scripted 返回依次给出口令的注入式 prompt，并记录调用次数；绝不碰 /dev/tty。
func scripted(calls *int, answers ...string) passPrompt {
	return func(string) (string, error) {
		i := *calls
		*calls++
		if i >= len(answers) {
			return "", errors.New("no more input")
		}
		return answers[i], nil
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	plain := pemOf(t, priv)
	blob, err := encryptKey(plain, testPass, kdfIterations)
	if err != nil {
		t.Fatal(err)
	}
	if !isEncryptedKey(blob) || bytes.Contains(blob, []byte("PRIVATE KEY")) {
		t.Fatal("密文应带魔数且不含明文")
	}
	got, err := decryptKey(blob, testPass)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip err=%v", err)
	}
	if _, err := encryptKey(plain, "short", kdfIterations); err == nil {
		t.Fatal("过短口令应被拒")
	}
	if _, err := encryptKey(plain, testPass, 1000); err == nil {
		t.Fatal("迭代次数过低应被拒")
	}
}

func TestDecryptWrongPassAndTamper(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	blob, _ := encryptKey(pemOf(t, priv), testPass, kdfIterations)
	if _, err := decryptKey(blob, "wrong-passphrase"); !errors.Is(err, errBadPass) {
		t.Fatalf("错误口令 err=%v", err)
	}
	// 篡改头部各区域（盐）与密文末尾，均应被拒。
	for _, idx := range []int{len(keyMagic) + 6, headerLen + 3, len(blob) - 1} {
		bad := append([]byte(nil), blob...)
		bad[idx] ^= 0x01
		if _, err := decryptKey(bad, testPass); err == nil {
			t.Fatalf("篡改偏移 %d 未被发现", idx)
		}
	}
	// 迭代次数被降低 / 抬高到上限之外：格式错误，不进入 KDF。
	low := append([]byte(nil), blob...)
	low[len(keyMagic)+4] = 1
	low[len(keyMagic)+1], low[len(keyMagic)+2], low[len(keyMagic)+3] = 0, 0, 0
	if _, err := decryptKey(low, testPass); !errors.Is(err, errBadFormat) {
		t.Fatalf("低迭代 err=%v", err)
	}
	if _, err := decryptKey(blob[:headerLen], testPass); !errors.Is(err, errBadFormat) {
		t.Fatalf("截断 err=%v", err)
	}
}

func TestEncryptKeyCommand(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	in, out := filepath.Join(dir, "plain.pem"), filepath.Join(dir, "k.enc")
	if err := os.WriteFile(in, pemOf(t, priv), 0o600); err != nil {
		t.Fatal(err)
	}
	var so, se bytes.Buffer
	n := 0
	// 两次输入不一致 → 失败且不写文件
	if err := runEncryptKey([]string{"--in", in, "--out", out}, noEnv, scripted(&n, testPass, "other-pass-xx"), &so, &se); err == nil {
		t.Fatal("两次口令不一致应失败")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("失败时不应留下文件")
	}
	n = 0
	if err := runEncryptKey([]string{"--in", in, "--out", out}, noEnv, scripted(&n, testPass, testPass), &so, &se); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("权限应为 0600: %v %v", fi, err)
	}
	if !strings.Contains(so.String(), "请安全删除明文文件") {
		t.Fatalf("缺少提示: %q", so.String())
	}
	if _, err := os.Stat(in); err != nil {
		t.Fatal("不得自动删除明文文件")
	}
	// 已存在的输出不覆盖
	n = 0
	if err := runEncryptKey([]string{"--in", in, "--out", out}, noEnv, scripted(&n, testPass, testPass), &so, &se); err == nil {
		t.Fatal("不应覆盖已有文件")
	}
	// 环境变量口令（自动化）+ 加载
	env := func(k string) string {
		if k == passEnv {
			return testPass
		}
		return ""
	}
	got, _, enc, err := loadKeyWith(out, env, scripted(new(int)), io.Discard)
	if err != nil || !enc || !got.Equal(priv) {
		t.Fatalf("load err=%v enc=%v", err, enc)
	}
	// decrypt-check 只输出 kid，不含私钥
	so.Reset()
	if err := runDecryptCheck([]string{"--key", out}, env, scripted(new(int)), &so, &se); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(so.String(), keyID(priv.Public().(ed25519.PublicKey))) || strings.Contains(so.String(), "PRIVATE") {
		t.Fatalf("decrypt-check 输出: %q", so.String())
	}
}

func TestLoadEncryptedRetriesThree(t *testing.T) {
	dir := t.TempDir()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	blob, _ := encryptKey(pemOf(t, priv), testPass, kdfIterations)
	p := filepath.Join(dir, "k.enc")
	if err := os.WriteFile(p, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	var se bytes.Buffer
	n := 0
	_, _, _, err := loadKeyWith(p, noEnv, scripted(&n, "bad-pass-1", "bad-pass-2", "bad-pass-3", testPass), &se)
	if err == nil || n != 3 {
		t.Fatalf("应在 3 次后放弃: err=%v n=%d", err, n)
	}
	if strings.Contains(err.Error(), "GCM") || strings.Contains(se.String(), "bad-pass") {
		t.Fatal("不应泄露细节或口令")
	}
	n = 0
	got, _, _, err := loadKeyWith(p, noEnv, scripted(&n, "bad-pass-1", testPass), &se)
	if err != nil || n != 2 || !got.Equal(priv) {
		t.Fatalf("第二次应成功: err=%v n=%d", err, n)
	}
	// 环境变量口令错误：不重试、不提示
	n = 0
	bad := func(string) string { return "wrong-env-pass" }
	if _, _, _, err := loadKeyWith(p, bad, scripted(&n, testPass), &se); err == nil || n != 0 {
		t.Fatalf("env 错误口令应直接失败: err=%v n=%d", err, n)
	}
}

func mustNets(t *testing.T, s ...string) []netip.Prefix {
	t.Helper()
	n, err := parseNets(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParseNetsAndMatch(t *testing.T) {
	nets := mustNets(t, "10.0.0.0/8, 203.0.113.5", "2001:db8::/32")
	cases := map[string]bool{
		"10.9.8.7": true, "203.0.113.5": true, "203.0.113.6": false, "11.0.0.1": false,
		"2001:db8::1": true, "2001:db9::1": false, "::ffff:10.1.1.1": true,
	}
	for ip, want := range cases {
		if got := inNets(nets, netip.MustParseAddr(ip).Unmap()); got != want {
			t.Errorf("%s => %v, want %v", ip, got, want)
		}
	}
	for _, bad := range []string{"10.0.0.0/33", "abc", "1.2.3"} {
		if _, err := parseNets([]string{bad}); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
}

func guardCall(allow, trusted []netip.Prefix, remote string, xff ...string) (int, string, string) {
	var logged strings.Builder
	h := ipGuard(allow, trusted, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("app"))
	}), func(f string, a ...any) { logged.WriteString(strings.TrimSpace(fmt.Sprintf(f, a...))) },
		func() time.Time { return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for _, x := range xff {
		req.Header.Add("X-Forwarded-For", x)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code, w.Body.String(), logged.String()
}

func TestIPGuardDefaultLoopback(t *testing.T) {
	if c, _, _ := guardCall(loopbackNets, nil, "127.0.0.1:5000"); c != 200 {
		t.Fatalf("回环应放行: %d", c)
	}
	if c, _, _ := guardCall(loopbackNets, nil, "[::1]:5000"); c != 200 {
		t.Fatalf("::1 应放行: %d", c)
	}
	c, body, log := guardCall(loopbackNets, nil, "198.51.100.9:5000")
	if c != 403 || body != "" {
		t.Fatalf("应 403 且 body 为空: %d %q", c, body)
	}
	if !strings.Contains(log, "198.51.100.9") || !strings.Contains(log, "2026-10-17T00:00:00Z") {
		t.Fatalf("日志应含 IP 与时间: %q", log)
	}
}

func TestIPGuardXFFOnlyFromTrustedProxy(t *testing.T) {
	allow := mustNets(t, "203.0.113.0/24")
	trusted := mustNets(t, "10.0.0.0/8")
	// 非受信对端伪造 XFF：忽略 XFF，按对端判定 → 拒绝
	if c, _, _ := guardCall(allow, trusted, "198.51.100.9:1", "203.0.113.7"); c != 403 {
		t.Fatalf("非代理的 XFF 不应被采信: %d", c)
	}
	// 没配 trusted-proxy：永远不看 XFF
	if c, _, _ := guardCall(allow, nil, "10.0.0.2:1", "203.0.113.7"); c != 403 {
		t.Fatalf("未配置代理时不应采信 XFF: %d", c)
	}
	// 受信代理 + 最右侧非代理 IP 在白名单 → 放行
	if c, _, _ := guardCall(allow, trusted, "10.0.0.2:1", "198.51.100.9, 203.0.113.7, 10.0.0.3"); c != 200 {
		t.Fatalf("应取最右侧非代理 IP 203.0.113.7: %d", c)
	}
	// 客户端自填的左侧白名单 IP 不能骗过：最右侧非代理是 198.51.100.9 → 拒绝
	if c, _, _ := guardCall(allow, trusted, "10.0.0.2:1", "203.0.113.7, 198.51.100.9"); c != 403 {
		t.Fatalf("左侧伪造不应生效: %d", c)
	}
	// 多个 XFF 头合并；无法解析的项 → 拒绝
	if c, _, _ := guardCall(allow, trusted, "10.0.0.2:1", "203.0.113.7", "10.0.0.3"); c != 200 {
		t.Fatalf("多头应合并: %d", c)
	}
	if c, _, _ := guardCall(allow, trusted, "10.0.0.2:1", "203.0.113.7, garbage"); c != 403 {
		t.Fatalf("无法解析应拒绝: %d", c)
	}
}

func TestCheckListenRequiresAllow(t *testing.T) {
	for _, a := range []string{"127.0.0.1:8787", "[::1]:8787", "localhost:8787"} {
		if err := checkListen(a, false); err != nil {
			t.Errorf("%s 不应被拒: %v", a, err)
		}
	}
	for _, a := range []string{"0.0.0.0:8787", ":8787", "192.168.1.5:8787", "[::]:8787"} {
		if err := checkListen(a, false); err == nil {
			t.Errorf("%s 无白名单应被拒", a)
		}
		if err := checkListen(a, true); err != nil {
			t.Errorf("%s 有白名单应放行: %v", a, err)
		}
	}
	if err := checkListen("bad", false); err == nil {
		t.Error("格式错误应报错")
	}
}
