-- 数据库备份：sys_backup（备份记录，平台级）、sys_backup_setting（保留策略与定时，只有 id=1 一行）
CREATE TABLE sys_backup (
  id BIGINT NOT NULL,
  file_name VARCHAR(200) NOT NULL,
  engine VARCHAR(16) NOT NULL,
  version VARCHAR(32) NOT NULL,
  size BIGINT NOT NULL DEFAULT 0,
  sha256 VARCHAR(64) NOT NULL DEFAULT '',
  source VARCHAR(16) NOT NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL,
  error VARCHAR(1000) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_backup_created ON sys_backup (created_at);
CREATE INDEX idx_sys_backup_status ON sys_backup (status);

CREATE TABLE sys_backup_setting (
  id BIGINT NOT NULL,
  schedule VARCHAR(5) NOT NULL DEFAULT '',
  keep_days INT NOT NULL DEFAULT 0,
  keep_count INT NOT NULL DEFAULT 0,
  last_run_date VARCHAR(10) NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
