-- 岗位表（按租户隔离）：同租户内岗位编码唯一，可关联部门。
CREATE TABLE sys_post (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  code VARCHAR(64) NOT NULL,
  dept_id BIGINT NULL,
  sort INT NOT NULL,
  status VARCHAR(1) NOT NULL,
  remark VARCHAR(500) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_post_tenant_code ON sys_post (tenant_id, code);
