-- 待跟进提醒去重：跟进记录建后不可编辑（next_at 不变），到期的待办由清扫器发一次
-- 站内信（发给客户归属人），reminded_at 落发送时间，NULL = 还没提醒过。发过一次就
-- 不再重发——要再提醒得等新的待办。索引服务清扫查询：done + next_at 的到期扫描。
ALTER TABLE plg_crm_activity ADD COLUMN reminded_at DATETIME(3) NULL;
CREATE INDEX idx_crm_activity_due ON plg_crm_activity (done, next_at);
