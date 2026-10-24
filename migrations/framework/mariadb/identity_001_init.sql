-- 第三方账号绑定：一个本地用户在某个 provider 下至多一条，同一个第三方身份至多属于一个用户。
-- provider：wecom | dingtalk | feishu | google | github | oidc；external_id 是该 provider 内稳定唯一的用户标识。
CREATE TABLE sys_user_identity (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  provider VARCHAR(32) NOT NULL,
  external_id VARCHAR(128) NOT NULL,
  external_name VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_user_identity_ext ON sys_user_identity (tenant_id, provider, external_id);
CREATE UNIQUE INDEX uk_sys_user_identity_user ON sys_user_identity (user_id, provider);
