-- 在线留言治理：收件箱从「读没读」升到「处理完没有」，is_read 换成三态 status；
-- 站点设置加一封邮件抄送开关（新留言除了站内信，可再抄一份到站点联系邮箱）。
ALTER TABLE plg_website_message ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'new';
UPDATE plg_website_message SET status = 'read' WHERE is_read = TRUE;
DROP INDEX idx_plg_website_message_read;
CREATE INDEX idx_plg_website_message_status ON plg_website_message (status, created_at);
ALTER TABLE plg_website_message DROP COLUMN is_read;

ALTER TABLE plg_website_setting ADD COLUMN message_notify_email BOOLEAN NOT NULL DEFAULT TRUE;
