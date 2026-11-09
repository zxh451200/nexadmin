-- 可选：对外输出的 SP 元数据用 SP 私钥做 XML 签名（IdP 侧可验证元数据来源）。
ALTER TABLE plg_saml_source
  ADD COLUMN sign_metadata BOOLEAN NOT NULL DEFAULT FALSE;
