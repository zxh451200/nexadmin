-- 询盘附件：访客在商品详情页的「发送询盘」弹窗里上传的文件。
-- 先落一行（message_id = 0），提交询盘时按 token 绑定到留言；没人认领的超时行由提交时顺手清理。
CREATE TABLE plg_website_message_file (
  id BIGINT PRIMARY KEY,
  token VARCHAR(40) NOT NULL,
  name VARCHAR(255) NOT NULL,
  mime VARCHAR(128) NOT NULL,
  size BIGINT NOT NULL DEFAULT 0,
  storage_key VARCHAR(255) NOT NULL,
  message_id BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_message_file_token ON plg_website_message_file (token);
CREATE INDEX idx_plg_website_message_file_message ON plg_website_message_file (message_id);
