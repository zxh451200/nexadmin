-- 官网页脚与联系页：电话、二维码、社交图标、友情链接；外加「在线留言」投稿表。
-- 社交/友链走 JSON 文本列（[]SocialLink / []FriendLink），三库行为一致。
ALTER TABLE plg_website_setting ADD COLUMN contact_phone VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN qr_code_url VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN qr_code_caption VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN socials TEXT NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN friend_links TEXT NOT NULL DEFAULT '';

CREATE TABLE plg_website_message (
  id BIGINT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  email VARCHAR(128) NOT NULL,
  phone VARCHAR(32) NOT NULL DEFAULT '',
  content VARCHAR(2000) NOT NULL,
  ip VARCHAR(64) NOT NULL DEFAULT '',
  ua VARCHAR(255) NOT NULL DEFAULT '',
  is_read BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_website_message_created ON plg_website_message (created_at);
CREATE INDEX idx_plg_website_message_read ON plg_website_message (is_read, created_at);
