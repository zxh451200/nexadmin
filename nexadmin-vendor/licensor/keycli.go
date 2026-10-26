package main

import (
	"bufio"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// passPrompt 读取一次口令（不回显）。做成类型以便测试注入，测试里绝不读 /dev/tty。
type passPrompt func(label string) (string, error)

// ttyPrompt 从 /dev/tty 读取口令并用 stty 关闭回显（Linux/macOS；Windows 不支持，请改用环境变量）。
// 无法关闭回显时拒绝读取，绝不在回显状态下读口令。
func ttyPrompt(label string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("无法打开 /dev/tty，请改用环境变量 %s", passEnv)
	}
	defer tty.Close() //nolint:errcheck
	stty := func(args ...string) ([]byte, error) {
		c := exec.Command("stty", args...)
		c.Stdin = tty
		return c.Output()
	}
	saved, err := stty("-g")
	if err != nil {
		return "", errors.New("无法关闭终端回显（stty 不可用），已拒绝读取口令")
	}
	state := strings.TrimSpace(string(saved))
	if _, err := stty("-echo"); err != nil {
		return "", errors.New("无法关闭终端回显，已拒绝读取口令")
	}
	restore := func() { _, _ = stty(state) }
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-sig:
			restore()
			fmt.Fprintln(tty)
			os.Exit(130)
		case <-done:
		}
	}()
	defer func() { close(done); signal.Stop(sig); restore() }()

	fmt.Fprint(tty, label)
	line, err := bufio.NewReader(tty).ReadString('\n')
	fmt.Fprintln(tty)
	if err != nil && line == "" {
		return "", errors.New("读取口令失败")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// newPassphrase 取新口令：环境变量优先；否则提示输入两次并要求一致。
func newPassphrase(env func(string) string, prompt passPrompt) (string, error) {
	if p := env(passEnv); p != "" {
		return p, nil
	}
	p1, err := prompt("请输入新口令: ")
	if err != nil {
		return "", err
	}
	p2, err := prompt("请再输入一次: ")
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", errors.New("两次输入的口令不一致")
	}
	return p1, nil
}

// runEncryptKey 实现 `licensor encrypt-key --in <明文 PEM> --out <密文>`。不会删除明文文件。
func runEncryptKey(args []string, env func(string) string, prompt passPrompt, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("encrypt-key", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "明文 PKCS#8 PEM 私钥文件")
	out := fs.String("out", "", "密文输出文件（必须不存在）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *out == "" {
		return errors.New("需要 --in 和 --out")
	}
	if *in == *out {
		return errors.New("--in 与 --out 不能相同")
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	if isEncryptedKey(raw) {
		return errors.New("--in 已经是密文文件")
	}
	priv, err := parseKeyPEM(raw)
	if err != nil {
		return fmt.Errorf("--in 不是有效的 ed25519 PKCS#8 PEM: %w", err)
	}
	pass, err := newPassphrase(env, prompt)
	if err != nil {
		return err
	}
	blob, err := encryptKey(raw, pass, kdfIterations)
	if err != nil {
		return err
	}
	// 立即回读验证，避免写出一个解不开的文件。
	if chk, err := decryptKey(blob, pass); err != nil || string(chk) != string(raw) {
		return errors.New("加密后自检失败，未写出文件")
	} else {
		wipe(chk)
	}
	// O_EXCL：不覆盖已有文件。
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(blob)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(*out)
		return errors.Join(werr, cerr)
	}
	if err := os.Chmod(*out, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "已写入 %s（权限 0600，kid=%s）\n请安全删除明文文件 %s（本工具不会自动删除）。\n", *out, keyID(priv.Public().(ed25519.PublicKey)), *in)
	return nil
}

// runDecryptCheck 实现 `licensor decrypt-check --key <密文>`：只验证口令并打印 kid。
func runDecryptCheck(args []string, env func(string) string, prompt passPrompt, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("decrypt-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	key := fs.String("key", "", "密文私钥文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" {
		return errors.New("需要 --key")
	}
	blob, err := os.ReadFile(*key)
	if err != nil {
		return err
	}
	if !isEncryptedKey(blob) {
		return errors.New("不是密文私钥文件")
	}
	plain, err := decryptWithRetry(blob, env, prompt, stderr)
	if err != nil {
		return err
	}
	defer wipe(plain)
	priv, err := parseKeyPEM(plain)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "口令正确，kid=%s\n", keyID(priv.Public().(ed25519.PublicKey)))
	return nil
}
