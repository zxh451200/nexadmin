-- 新闻版本历史：覆盖保存一直是原地 UPDATE 同一行（SavePost），旧正文随写随丢。
-- 每次内容变化的覆盖保存前把旧行快照落到这里，改坏了能回看、也能一键回滚
-- （回滚本身再走一次正常保存，版本继续递增、可再回滚）。单条新闻滚动保留最近 50 版。
-- 索引非唯一：版本号 = 该新闻现有最大 version+1，历史行与当前行分表存放。
CREATE TABLE plg_website_post_version (
  id BIGINT PRIMARY KEY,
  post_id BIGINT NOT NULL,
  version INT NOT NULL,
  title VARCHAR(200) NOT NULL DEFAULT '',
  summary VARCHAR(500) NOT NULL DEFAULT '',
  cover_url VARCHAR(255) NOT NULL DEFAULT '',
  content TEXT,
  i18n_json TEXT,
  seo_title VARCHAR(200) NOT NULL DEFAULT '',
  seo_desc VARCHAR(300) NOT NULL DEFAULT '',
  noindex BOOLEAN NOT NULL DEFAULT FALSE,
  published BOOLEAN NOT NULL DEFAULT FALSE,
  published_at TIMESTAMP NULL,
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_website_post_version ON plg_website_post_version (post_id, version);
