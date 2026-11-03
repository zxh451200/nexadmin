-- 角色接入回收站：只加软删除列。
-- (tenant_id, code) 本来就是普通索引 idx_sys_role_tenant_code（唯一性一直靠应用层 codeTaken），
-- 降级与还原撞码的处理见 system_post_002_soft_delete.sql 的同类注释。
ALTER TABLE sys_role ADD COLUMN deleted_at DATETIME(3) NULL;
CREATE INDEX idx_sys_role_deleted ON sys_role (deleted_at);
