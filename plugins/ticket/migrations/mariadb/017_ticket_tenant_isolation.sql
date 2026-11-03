-- 租户隔离：把工单的「客户数据」五张表按租户切开（工单本体、回复、附件、动态、关联）。
-- 换到 TenantModel 后，GORM 的租户插件会给这些表的查询/更新/删除自动追加 tenant_id 条件。
--
-- 存量行按 DEFAULT 直接落到默认租户 1（tenant.DefaultID），不另写 UPDATE：
-- 三引擎的 ADD COLUMN ... NOT NULL DEFAULT 都会回填既有行。
--
-- 配置类表（plg_ticket_canned / _field / _group / _group_member / _rule / _sla / _setting）
-- 本轮仍是平台级共享，与 CannedReply 既有注释同一条线：它们是运营配置不是客户数据。
-- 边界写进 model.go 的注释，别让人以为工单已经整体多租户了。只增不改。
ALTER TABLE plg_ticket ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_plg_ticket_tenant ON plg_ticket (tenant_id, status, last_reply_at);

ALTER TABLE plg_ticket_reply ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_plg_ticket_reply_tenant ON plg_ticket_reply (tenant_id);

ALTER TABLE plg_ticket_attachment ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_plg_ticket_attachment_tenant ON plg_ticket_attachment (tenant_id);

ALTER TABLE plg_ticket_activity ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_plg_ticket_activity_tenant ON plg_ticket_activity (tenant_id);

ALTER TABLE plg_ticket_link ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_plg_ticket_link_tenant ON plg_ticket_link (tenant_id);
