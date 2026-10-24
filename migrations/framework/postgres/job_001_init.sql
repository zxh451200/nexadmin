-- 异步任务（导出中心等）：参数与结果不含敏感字段；result_file 是对象存储键，不是下载链接
CREATE TABLE sys_job (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  owner_id BIGINT NOT NULL,
  type VARCHAR(64) NOT NULL,
  params TEXT NOT NULL,
  status VARCHAR(16) NOT NULL,
  progress INT NOT NULL,
  message VARCHAR(255) NOT NULL DEFAULT '',
  result_file VARCHAR(500) NOT NULL DEFAULT '',
  result_name VARCHAR(255) NOT NULL DEFAULT '',
  result_size BIGINT NOT NULL DEFAULT 0,
  result_mime VARCHAR(150) NOT NULL DEFAULT '',
  error VARCHAR(500) NOT NULL DEFAULT '',
  node VARCHAR(64) NOT NULL DEFAULT '',
  started_at TIMESTAMP(3) NULL,
  finished_at TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_job_owner ON sys_job (tenant_id, owner_id, created_at);
CREATE INDEX idx_sys_job_status ON sys_job (status, type, created_at);
CREATE INDEX idx_sys_job_finished ON sys_job (finished_at);
