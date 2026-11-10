package maps

// core/secret 包装：静态加密厂商服务端 Key。测试环境没有默认密钥时退化为明文/原样，
// 存量明文（升级前写入的）靠 secret.IsEncrypted 判别后原样读出，兼容不迁移。

import "nexadmin/core/secret"

func secretEncrypt(plain string) (string, error) {
	if secret.Default() == nil {
		return plain, nil
	}
	return secret.Encrypt(plain)
}

func secretDecrypt(enc string) (string, error) {
	if secret.Default() == nil || !secret.IsEncrypted(enc) {
		return enc, nil
	}
	return secret.Decrypt(enc)
}
