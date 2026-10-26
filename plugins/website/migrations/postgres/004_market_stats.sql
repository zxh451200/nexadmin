-- 官网插件市场公开统计与匿名评分（平台级）。
CREATE TABLE plg_website_market_stat (
  id BIGINT PRIMARY KEY,
  plugin_id VARCHAR(48) NOT NULL,
  downloads BIGINT NOT NULL DEFAULT 0,
  rating_sum BIGINT NOT NULL DEFAULT 0,
  rating_count BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  CONSTRAINT uk_plg_website_market_stat UNIQUE (plugin_id)
);

CREATE TABLE plg_website_market_rating (
  id BIGINT PRIMARY KEY,
  plugin_id VARCHAR(48) NOT NULL,
  voter VARCHAR(64) NOT NULL,
  score SMALLINT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  CONSTRAINT uk_plg_website_market_rating UNIQUE (plugin_id, voter)
);
