-- 新闻运营三件套：浏览量（公开详情自增）、置顶（列表置顶排序）、作者（展示名，不关联账号）。
ALTER TABLE plg_website_post ADD COLUMN views BIGINT NOT NULL DEFAULT 0;
ALTER TABLE plg_website_post ADD COLUMN pinned TINYINT(1) NOT NULL DEFAULT 0;
ALTER TABLE plg_website_post ADD COLUMN author VARCHAR(64) NOT NULL DEFAULT '';
