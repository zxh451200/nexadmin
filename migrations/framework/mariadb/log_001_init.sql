-- 操作日志与登录日志
CREATE TABLE sys_oper_log (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  module VARCHAR(64) NOT NULL,
  type VARCHAR(32) NOT NULL,
  operator VARCHAR(64) NOT NULL,
  ip VARCHAR(64),
  location VARCHAR(128),
  status VARCHAR(16) NOT NULL,
  cost_ms INT NOT NULL DEFAULT 0,
  method VARCHAR(16) NOT NULL,
  url VARCHAR(500) NOT NULL,
  params TEXT,
  response TEXT,
  error_stack TEXT,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_oper_log_tenant ON sys_oper_log (tenant_id, created_at);

CREATE TABLE sys_login_log (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  username VARCHAR(64) NOT NULL,
  login_type VARCHAR(32) NOT NULL,
  ip VARCHAR(64),
  location VARCHAR(128),
  browser VARCHAR(64),
  os VARCHAR(64),
  status VARCHAR(16) NOT NULL,
  fail_reason VARCHAR(32),
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_login_log_tenant ON sys_login_log (tenant_id, created_at);
