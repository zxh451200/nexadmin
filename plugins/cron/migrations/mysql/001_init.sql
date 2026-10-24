-- 定时任务插件：plg_cron_job（任务）、plg_cron_log（执行日志）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_cron_job (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  cron_expr VARCHAR(100) NOT NULL,
  handler VARCHAR(64) NOT NULL,
  params TEXT,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  retry_count INT NOT NULL DEFAULT 0,
  last_run_at DATETIME(3) NULL,
  last_status VARCHAR(16) NOT NULL DEFAULT '',
  last_slot BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_cron_job_tenant ON plg_cron_job (tenant_id, enabled);

CREATE TABLE plg_cron_log (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  job_id BIGINT NOT NULL,
  job_name VARCHAR(100) NOT NULL,
  handler VARCHAR(64) NOT NULL,
  trigger_type VARCHAR(16) NOT NULL,
  status VARCHAR(16) NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  started_at DATETIME(3) NOT NULL,
  finished_at DATETIME(3) NULL,
  cost_ms INT NOT NULL DEFAULT 0,
  output TEXT,
  error TEXT,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_cron_log_job ON plg_cron_log (tenant_id, job_id, started_at);
CREATE INDEX idx_plg_cron_log_status ON plg_cron_log (tenant_id, status, started_at);
