-- 告警事件生命周期：active（未处理）→ ack（已认领）→ cleared（已恢复）。
-- 静默只写 silenced_until 压制后续通知，不改变生命周期状态。只增不改。
-- 存量事件没有生命周期数据，先按 cleared 落库，避免升级后全部涌进「未处理」；
-- 列建好后再把默认值改成 active，新事件由应用显式写 active。
ALTER TABLE plg_devhub_alarm_event ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'cleared';
ALTER TABLE plg_devhub_alarm_event ALTER COLUMN status SET DEFAULT 'active';
ALTER TABLE plg_devhub_alarm_event ADD COLUMN ack_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE plg_devhub_alarm_event ADD COLUMN ack_by_name VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_devhub_alarm_event ADD COLUMN ack_at DATETIME(3) NULL;
ALTER TABLE plg_devhub_alarm_event ADD COLUMN cleared_at DATETIME(3) NULL;
ALTER TABLE plg_devhub_alarm_event ADD COLUMN silenced_until DATETIME(3) NULL;
CREATE INDEX idx_plg_devhub_alarm_event_device_status ON plg_devhub_alarm_event (device_id, status, id);
