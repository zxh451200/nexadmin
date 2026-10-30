-- 附件：访客/坐席上传的图片与文件，消息通过 meta 引用 token。
CREATE TABLE plg_im_attachment (
  id BIGINT PRIMARY KEY,
  token VARCHAR(40) NOT NULL,
  conv_id BIGINT NOT NULL,
  uploader VARCHAR(16) NOT NULL,
  name VARCHAR(255) NOT NULL,
  mime VARCHAR(128) NOT NULL,
  size BIGINT NOT NULL,
  storage_key VARCHAR(255) NOT NULL,
  used BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_im_att_token ON plg_im_attachment (token);
CREATE INDEX idx_plg_im_att_conv ON plg_im_attachment (conv_id, created_at);
