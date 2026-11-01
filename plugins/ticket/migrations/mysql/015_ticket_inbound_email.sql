-- 邮件渠道进单：每条入站邮件对应的回复记下 RFC 5322 Message-ID，
-- 同一封邮件重投（IMAP 重连、webhook 重试）靠它去重，回信靠 In-Reply-To/References 找回原单。只增不改。
ALTER TABLE plg_ticket_reply ADD COLUMN message_id VARCHAR(255) NOT NULL DEFAULT '';
CREATE INDEX idx_plg_ticket_reply_msgid ON plg_ticket_reply (message_id);
