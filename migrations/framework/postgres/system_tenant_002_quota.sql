-- 租户额度：套餐新增角色数上限（0 = 不限）；租户可单独覆盖套餐额度（NULL = 沿用套餐，0 = 不限）。
ALTER TABLE sys_tenant_pkg ADD COLUMN max_roles INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sys_tenant ADD COLUMN max_users INTEGER NULL;
ALTER TABLE sys_tenant ADD COLUMN max_roles INTEGER NULL;
ALTER TABLE sys_tenant ADD COLUMN max_storage_mb BIGINT NULL;
