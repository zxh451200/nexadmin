-- 协议级加固与请求选项：CBC 加密断言要求响应先签名、拒绝未知 Conditions、强制重新认证、请求/校验 AuthnContext。
-- 默认全关以保持兼容：多数 IdP 只签断言并用 AES-CBC 加密，开启 CBC 完整性要求需 IdP 同时签响应。
ALTER TABLE plg_saml_source
  ADD COLUMN require_signed_cbc TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN reject_unknown_conditions TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN force_authn TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN authn_context_class_ref VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN require_authn_context TINYINT(1) NOT NULL DEFAULT 0;