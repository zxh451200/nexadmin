package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"nexadmin/core/plugin/market"
)

// cmdVerify 实现 `nexadmin plugin verify <zip> --sig <文件> --key <公钥文件>`：
// 校验官网下载包附带的 detached ed25519 签名。
func cmdVerify(args []string, out io.Writer) error {
	fl := newFlags("verify")
	sigPath := fl.String("sig", "", "签名文件（base64，来自 /plugins/<id>/download.sig）")
	keyPath := fl.String("key", "", "公钥文件（base64，来自 /plugins/signing-key.pub）")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 || *sigPath == "" || *keyPath == "" {
		return errors.New("用法: nexadmin plugin verify --sig <签名文件> --key <公钥文件> <zip>")
	}
	sigRaw, err := os.ReadFile(*sigPath)
	if err != nil {
		return err
	}
	keyRaw, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	sig, err := market.DecodeSignature(string(sigRaw))
	if err != nil {
		return err
	}
	pub, err := market.DecodePublicKey(string(keyRaw))
	if err != nil {
		return err
	}
	zipPath := fl.Arg(0)
	if err := market.VerifyFile(zipPath, sig, pub); err != nil {
		return fmt.Errorf("签名校验失败: %w", err)
	}
	fmt.Fprintf(out, "签名有效：%s\n", zipPath)
	return nil
}
