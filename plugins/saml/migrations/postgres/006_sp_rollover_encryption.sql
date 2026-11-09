-- SP 证书两段式轮换与加密断言要求。
-- 轮换分两步：先发布 next 证书（元数据同时列出 current+next，签发仍用 current），
-- 待 IdP 更新后再切换；切换后旧证书保留 prev_* 作为解密兜底，到期自动不再公布。
ALTER TABLE plg_saml_source
  ADD COLUMN require_encrypted_assertion BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN next_sp_cert TEXT NULL,
  ADD COLUMN next_sp_key_enc TEXT NULL,
  ADD COLUMN prev_sp_cert TEXT NULL,
  ADD COLUMN prev_sp_key_enc TEXT NULL,
  ADD COLUMN prev_sp_expires_at TIMESTAMP(3) NULL;
