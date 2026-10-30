-- 附件：访客/坐席上传的图片与文件，消息通过 meta 引用 token。
CREATE TABLE plg_im_attachment (
  id BIGINT NOT NULL,
  token VARCHAR(40) NOT NULL,
  conv_id BIGINT NOT NULL,
  uploader VARCHAR(16) NOT NULL,
  name VARCHAR(255) NOT NULL,
  mime VARCHAR(128) NOT NULL,
  size BIGINT NOT NULL,
  storage_key VARCHAR(255) NOT NULL,
  used TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_im_att_token (token)
);
CREATE INDEX idx_plg_im_att_conv ON plg_im_attachment (conv_id, created_at);
