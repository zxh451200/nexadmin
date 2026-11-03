-- 岗位接入回收站：加软删除列，并把同租户内编码的唯一索引降级成普通索引（名字保留，语义已变）。
-- 为什么降级：三引擎都拿不到「仅对未删行唯一」的干净写法（PG 的部分索引 MySQL 没有，跨引擎结构闸会红），
-- 对齐 sys_user 的既有做法。唯一性改由 post.codeTaken（只比对 deleted_at IS NULL 的行）保证，
-- 还原撞码由回收站在还原前显式检查并回 409 recycle.restore_conflict。
ALTER TABLE sys_post ADD COLUMN deleted_at TIMESTAMP(3) NULL;
CREATE INDEX idx_sys_post_deleted ON sys_post (deleted_at);
DROP INDEX uk_sys_post_tenant_code;
CREATE INDEX uk_sys_post_tenant_code ON sys_post (tenant_id, code);
