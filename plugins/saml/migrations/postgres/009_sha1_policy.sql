-- SHA-1 签名默认拒绝；仅老 IdP 只能 SHA-1 时按源显式放行。
ALTER TABLE plg_saml_source
  ADD COLUMN allow_sha1_signatures BOOLEAN NOT NULL DEFAULT FALSE;
