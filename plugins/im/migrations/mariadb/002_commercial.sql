-- 商用基线：结束态、访客已读、认领、评价、标签、访客来源、消息类型、快捷回复。
-- 只增不改：001 已是存量，这里用 ALTER 补齐。
ALTER TABLE plg_im_conversation
  ADD COLUMN closed_at DATETIME(3) NULL,
  ADD COLUMN closed_by VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN visitor_read_seq BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN assignee_id VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN assignee_name VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN rating TINYINT NOT NULL DEFAULT 0,
  ADD COLUMN rating_comment VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN rated_at DATETIME(3) NULL,
  ADD COLUMN tags VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN visitor_ip VARCHAR(45) NOT NULL DEFAULT '',
  ADD COLUMN visitor_ua VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN visitor_ref VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE plg_im_message
  ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'text',
  ADD COLUMN meta TEXT NULL;

CREATE TABLE plg_im_quick_reply (
  id BIGINT NOT NULL,
  title VARCHAR(64) NOT NULL,
  content VARCHAR(2000) NOT NULL,
  sort INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_im_qr_sort ON plg_im_quick_reply (sort, id);
