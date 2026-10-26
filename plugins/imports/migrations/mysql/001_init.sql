-- 导入中心插件:plg_imports_job(导入台账,租户级)。错误明细以 JSON 存于 job 行(上限 500 条)。
-- 只增不改;后续变更用 002_*.sql。
CREATE TABLE plg_imports_job (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  source VARCHAR(64) NOT NULL,
  file_name VARCHAR(200) NOT NULL DEFAULT '',
  total_rows INT NOT NULL DEFAULT 0,
  ok_rows INT NOT NULL DEFAULT 0,
  fail_rows INT NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  error_json TEXT,
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_imports_job_tenant ON plg_imports_job (tenant_id, source, created_at);
