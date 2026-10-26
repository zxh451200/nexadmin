-- 工单 SLA：响应到期时间与超时标记。只增不改。
ALTER TABLE plg_ticket ADD COLUMN due_at TIMESTAMP NULL;
ALTER TABLE plg_ticket ADD COLUMN overdue BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_plg_ticket_overdue ON plg_ticket (status, overdue, due_at);
