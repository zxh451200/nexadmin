-- 租户用量日切片：每天首次读取用量时幂等写一行，供趋势与配额预警使用。day 为 yyyy-MM-dd。
CREATE TABLE sys_tenant_usage_daily (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  day VARCHAR(10) NOT NULL,
  users BIGINT NOT NULL,
  roles BIGINT NOT NULL,
  storage_mb BIGINT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_tenant_usage_daily ON sys_tenant_usage_daily (tenant_id, day);
