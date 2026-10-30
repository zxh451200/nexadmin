-- 插件设置：单行 JSON（k='general'），存工作时间、离线提示与主动邀请参数。
CREATE TABLE plg_im_setting (
  id BIGINT NOT NULL,
  k VARCHAR(64) NOT NULL,
  v TEXT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_im_setting_k (k)
);
