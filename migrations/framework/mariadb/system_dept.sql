-- 部门表（按租户隔离）：parent_id + 物化路径 path 双写，"本部门及以下" = path LIKE '/1/4/%'。
CREATE TABLE sys_dept (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  parent_id BIGINT NULL,
  path VARCHAR(512) NOT NULL,
  sort INT NOT NULL,
  leader_id BIGINT NULL,
  leader_name VARCHAR(64) NOT NULL,
  phone VARCHAR(32) NOT NULL,
  email VARCHAR(100) NOT NULL,
  status VARCHAR(1) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_dept_tenant_parent ON sys_dept (tenant_id, parent_id);
CREATE INDEX idx_sys_dept_path ON sys_dept (path);
