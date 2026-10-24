-- 租户套餐与租户（平台级，超管端管理）。menu_ids 为菜单 ID 的 JSON 数组。
CREATE TABLE sys_tenant_pkg (
  id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  menu_ids TEXT NOT NULL,
  max_users INT NOT NULL,
  max_storage_mb BIGINT NOT NULL,
  status VARCHAR(1) NOT NULL,
  remark VARCHAR(500) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);

CREATE TABLE sys_tenant (
  id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  domain VARCHAR(255) NOT NULL,
  package_id BIGINT NOT NULL,
  expire_at DATETIME(3) NOT NULL,
  status VARCHAR(16) NOT NULL,
  admin_username VARCHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_tenant_domain ON sys_tenant (domain);
CREATE INDEX idx_sys_tenant_package ON sys_tenant (package_id);
