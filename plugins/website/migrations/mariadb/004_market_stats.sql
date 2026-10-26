-- 官网插件市场公开统计与匿名评分（平台级）。
CREATE TABLE plg_website_market_stat (
  id BIGINT NOT NULL,
  plugin_id VARCHAR(48) NOT NULL,
  downloads BIGINT NOT NULL DEFAULT 0,
  rating_sum BIGINT NOT NULL DEFAULT 0,
  rating_count BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_website_market_stat (plugin_id)
);

CREATE TABLE plg_website_market_rating (
  id BIGINT NOT NULL,
  plugin_id VARCHAR(48) NOT NULL,
  voter VARCHAR(64) NOT NULL,
  score TINYINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_website_market_rating (plugin_id, voter)
);
