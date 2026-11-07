-- 客户改软删除：删除即进回收站（modules/internal/recbin TypeCustomer），关联的联系人/跟进
-- 原样保留在源表（还原即完整回来），只有回收站的彻底删除才物理清掉。商机保护闸不变——
-- 挂着商机的客户仍然拒删，所以回收站里的客户永远没有商机连带要清。
-- 001-006 已执行，按「只增不改」走新文件。
ALTER TABLE plg_crm_customer ADD COLUMN deleted_at DATETIME(3) NULL;
CREATE INDEX idx_crm_customer_deleted ON plg_crm_customer (deleted_at);
