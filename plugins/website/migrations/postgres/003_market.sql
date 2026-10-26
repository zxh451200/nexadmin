-- 官网插件市场开关：默认开启；客户站点可在「站点设置」关闭公开市场入口与页面。
ALTER TABLE plg_website_setting ADD COLUMN market_enabled BOOLEAN NOT NULL DEFAULT TRUE;
