-- 官网访问统计：公开页浏览行（匿名会话 token，不含 IP 与个人身份；bot 不采集）。
-- day 按站点时区归日，行级存储 + 查询时聚合：官网 PV 量级（日千级）下无需预聚合表。
CREATE TABLE plg_website_page_view (
  id BIGINT PRIMARY KEY,
  day DATE NOT NULL,
  path VARCHAR(200) NOT NULL,
  ref_host VARCHAR(120) NOT NULL DEFAULT '',
  ref_kind VARCHAR(16) NOT NULL DEFAULT 'direct',
  session_key VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_website_pv_day ON plg_website_page_view (day);
CREATE INDEX idx_website_pv_path ON plg_website_page_view (path, day);
CREATE INDEX idx_website_pv_session ON plg_website_page_view (session_key, created_at);
