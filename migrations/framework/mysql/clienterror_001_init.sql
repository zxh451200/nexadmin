-- 前端错误上报：同一租户下相同指纹聚合为一行并累计次数（hit_count），保留 14 天由后台任务清理
CREATE TABLE sys_client_error (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL DEFAULT 0,
  username VARCHAR(64),
  route VARCHAR(255),
  message VARCHAR(500) NOT NULL,
  stack TEXT,
  ua VARCHAR(255),
  version VARCHAR(32),
  fingerprint VARCHAR(64) NOT NULL,
  hit_count INT NOT NULL DEFAULT 1,
  last_seen_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_client_error_fp ON sys_client_error (tenant_id, fingerprint);
CREATE INDEX idx_sys_client_error_seen ON sys_client_error (tenant_id, last_seen_at);
CREATE INDEX idx_sys_client_error_purge ON sys_client_error (last_seen_at);
