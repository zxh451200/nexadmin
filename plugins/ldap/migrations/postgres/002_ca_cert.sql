-- 内网自签 CA：信任该 CA 后 LDAPS/StartTLS 可用；空值使用系统根证书。
-- 只增不改;后续变更用 003_*.sql。
ALTER TABLE plg_ldap_config ADD COLUMN ca_cert TEXT;
