-- 回收站：只登记“谁删了哪一行”，数据留在源表（deleted_at 软删除）
CREATE TABLE sys_recycle (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  type VARCHAR(32) NOT NULL,
  ref_id BIGINT NOT NULL,
  name VARCHAR(200),
  deleted_by VARCHAR(64),
  deleted_at TIMESTAMP(3) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_recycle_tenant ON sys_recycle (tenant_id, type, deleted_at);
