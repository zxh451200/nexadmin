// licensor 是发行方自用的授权签发后台（不属于 nexadmin 模板，不发给客户）。
//
// 用法:
//
//	# 一次性：把明文私钥加密成密文文件（口令交互输入两次，或用 LICENSOR_PASSPHRASE）
//	licensor encrypt-key --in plain.pem --out private.enc
//	licensor decrypt-check --key private.enc        # 只验证口令并打印 kid，不输出私钥
//
//	# 日常：用密文启动（口令来自 LICENSOR_PASSPHRASE 或 /dev/tty 提示，最多重试 3 次）
//	licensor --key private.enc --token <口令> [--addr 127.0.0.1:8787] [--data data]
//	LICENSOR_TOKEN=<口令> licensor --key private.enc
//
//	# 监听非回环地址时必须设置 IP 白名单（否则拒绝启动）
//	licensor --key private.enc --addr 0.0.0.0:8787 --allow-ip 10.0.0.0/8,203.0.113.5 [--trusted-proxy 10.0.0.2]
//
// 然后浏览器打开 http://127.0.0.1:8787 ，输入口令即可。
//
// 说明:
//   - --key 可以是密文文件（魔数识别；PBKDF2-SHA256 ≥600000 次 + AES-256-GCM，解密后只在内存），
//     也可以是 PKCS#8 PEM 的 ed25519 私钥明文（nexadmin plugin keygen 生成；仍接受，但启动会打醒目警告建议加密）。
//     私钥只在启动时读取到内存，不会回显、不会上传、不会写入任何文件。权限不是 0600 时启动会警告。
//     口令输入依赖 /dev/tty 与 stty（Linux/macOS）；Windows 不支持交互输入，请用 LICENSOR_PASSPHRASE。
//   - --allow-ip（逗号分隔的 IP/CIDR，可重复；或环境变量 LICENSOR_ALLOW_IP，命令行优先）：默认只允许回环地址，
//     一旦设置则只允许所列地址（要保留本机请自己加上 127.0.0.1）。按 TCP RemoteAddr 判定，不信任 X-Forwarded-For；
//     仅当连接来自 --trusted-proxy（CIDR 列表）时，才取 X-Forwarded-For 最右侧非代理 IP。
//     不在白名单返回空 body 的 403，只记 IP 和时间。
//   - 完全独立：只用标准库，不 import 也不依赖 nexadmin 框架源码。授权文件格式见 format.go 顶部规范；
//     与框架一致性靠 testdata/golden.lic 黄金样本保证（框架侧有同一份样本）。
//   - 签发记录写入 <data>/issued.json（时间、插件、客户、到期、域名、kid、序列号、备注），不含私钥和授权全文。
//   - 页面上可以吊销授权（按序列号；旧授权没有序列号不能单独吊销）：记录写入 <data>/revoked.json（含 seq），
//     「导出吊销清单」（GET /api/revoked.json）用当前私钥签名，得到的 license-revoked.json 放进客户项目的
//     config/license-revoked.json（随升级包分发）才会生效。源码交付下买家可以删除清单，这是辅助手段，不是强保护。
//   - 默认只监听 127.0.0.1；口令放在 X-Licensor-Token / Authorization: Bearer 请求头或 licensor_token cookie 里，
//     常量时间比较。要远程访问请放在带 TLS 的反向代理后面。
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		var run func([]string, func(string) string, passPrompt, io.Writer, io.Writer) error
		switch os.Args[1] {
		case "encrypt-key":
			run = runEncryptKey
		case "decrypt-check":
			run = runDecryptCheck
		}
		if run != nil {
			if err := run(os.Args[2:], os.Getenv, ttyPrompt, os.Stdout, os.Stderr); err != nil {
				if !errors.Is(err, flag.ErrHelp) {
					fmt.Fprintln(os.Stderr, "错误:", err)
				}
				os.Exit(1)
			}
			return
		}
	}

	key := flag.String("key", "", "ed25519 私钥文件（密文，或 PKCS#8 PEM 明文）")
	addr := flag.String("addr", "127.0.0.1:8787", "监听地址（默认只监听本机）")
	token := flag.String("token", os.Getenv("LICENSOR_TOKEN"), "访问口令（或环境变量 LICENSOR_TOKEN）")
	dataDir := flag.String("data", "data", "数据目录（issued.json 签发记录、revoked.json 吊销记录）")
	var allowIP, trustedProxy stringList
	flag.Var(&allowIP, "allow-ip", "允许访问的 IP/CIDR，逗号分隔，可重复（或环境变量 LICENSOR_ALLOW_IP）；默认只允许回环")
	flag.Var(&trustedProxy, "trusted-proxy", "受信反向代理 CIDR/IP 列表；仅来自这些代理的连接才采信 X-Forwarded-For")
	flag.Parse()

	if *key == "" {
		log.Fatal("需要 --key 私钥文件")
	}
	if *token == "" {
		log.Fatal("需要 --token 或环境变量 LICENSOR_TOKEN 作为访问口令")
	}
	if len(allowIP) == 0 && os.Getenv("LICENSOR_ALLOW_IP") != "" {
		allowIP = stringList{os.Getenv("LICENSOR_ALLOW_IP")}
	}
	allow, err := parseNets(allowIP)
	if err != nil {
		log.Fatalf("--allow-ip: %v", err)
	}
	trusted, err := parseNets(trustedProxy)
	if err != nil {
		log.Fatalf("--trusted-proxy: %v", err)
	}
	if err := checkListen(*addr, len(allow) > 0); err != nil {
		log.Fatal(err)
	}
	if len(allow) == 0 {
		allow = loopbackNets
	}
	priv, warns, encrypted, err := loadKeyWith(*key, os.Getenv, ttyPrompt, os.Stderr)
	if err != nil {
		log.Fatalf("读取私钥失败: %v", err)
	}
	for _, w := range warns {
		log.Printf("警告: %s", w)
	}
	if !encrypted {
		log.Printf("警告: ******** 私钥是明文文件，只靠文件权限保护。强烈建议执行 `licensor encrypt-key --in %s --out <密文>` 加密后改用密文启动 ********", *key)
	}
	if host, _, _ := net.SplitHostPort(*addr); host != "localhost" && !net.ParseIP(strings.Trim(host, "[]")).IsLoopback() {
		log.Printf("警告: 监听 %s 不是本机回环地址，请务必置于 TLS 反向代理之后", *addr)
	}

	st, err := openStore(*dataDir)
	if err != nil {
		log.Fatalf("打开数据目录失败: %v", err)
	}
	s := newServer(priv, warns, *token, st)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           ipGuard(allow, trusted, s.routes(), log.Printf, time.Now),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	log.Printf("licensor 已启动: http://%s （kid=%s）", *addr, s.kid)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// loadKey 读取私钥并检查文件权限；返回的警告交给调用方展示。
func loadKey(path string) (ed25519.PrivateKey, []string, error) {
	priv, warns, _, err := loadKeyWith(path, os.Getenv, ttyPrompt, os.Stderr)
	return priv, warns, err
}
