-- 租户 AI：套餐新增每日调用次数上限（0 = 不限）；租户可单独关闭 AI（ai_enabled）并覆盖每日配额（NULL = 沿用套餐，0 = 不限）。
ALTER TABLE sys_tenant_pkg ADD COLUMN ai_daily_quota INT NOT NULL DEFAULT 0;
ALTER TABLE sys_tenant ADD COLUMN ai_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE sys_tenant ADD COLUMN ai_daily_quota INT NULL;
