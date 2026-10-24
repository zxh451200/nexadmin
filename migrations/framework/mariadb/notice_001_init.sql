-- 公告：sys_notice（软删除，接入回收站）、sys_notice_target（发布对象）、sys_notice_read（已读记录）
CREATE TABLE sys_notice (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  title VARCHAR(200) NOT NULL,
  type VARCHAR(32) NOT NULL,
  level VARCHAR(32) NOT NULL,
  status VARCHAR(16) NOT NULL,
  content TEXT,
  scope VARCHAR(16) NOT NULL,
  pinned TINYINT(1) NOT NULL DEFAULT 0,
  popup TINYINT(1) NOT NULL DEFAULT 0,
  platform TINYINT(1) NOT NULL DEFAULT 0,
  publish_at DATETIME(3) NULL,
  expire_at DATETIME(3) NULL,
  published_at DATETIME(3) NULL,
  attachments TEXT,
  total_count INT NOT NULL DEFAULT 0,
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  deleted_at DATETIME(3) NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_notice_tenant ON sys_notice (tenant_id, status);
CREATE INDEX idx_sys_notice_deleted ON sys_notice (deleted_at);

CREATE TABLE sys_notice_target (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  notice_id BIGINT NOT NULL,
  target_id BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_notice_target_notice ON sys_notice_target (notice_id);
CREATE INDEX idx_sys_notice_target_target ON sys_notice_target (target_id);

CREATE TABLE sys_notice_read (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  notice_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  read_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_notice_read ON sys_notice_read (notice_id, user_id);
CREATE INDEX idx_sys_notice_read_user ON sys_notice_read (user_id);
