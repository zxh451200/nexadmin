-- 官网插件：plg_website_media（logo / 封面等小图，按 id 不可变，重传生成新 id）。
CREATE TABLE plg_website_media (
  id BIGINT NOT NULL,
  mime VARCHAR(64) NOT NULL,
  size INT NOT NULL DEFAULT 0,
  data MEDIUMBLOB,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
