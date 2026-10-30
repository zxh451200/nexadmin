-- 插件设置：单行 JSON（k='general'），存工作时间、离线提示与主动邀请参数。
CREATE TABLE plg_im_setting (
  id BIGINT PRIMARY KEY,
  k VARCHAR(64) NOT NULL,
  v TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_im_setting_k ON plg_im_setting (k);
