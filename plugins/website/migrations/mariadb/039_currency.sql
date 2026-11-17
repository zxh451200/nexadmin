

-- 站点级展示币种与汇率表（对标阿里右上角「语言-币种」选择器）。
-- default_currency：未选择币种的访客（含爬虫）看到的币种；currency_rates 是
-- {"USD":1,"EUR":0.92,...} 文本列，键 = 可选币种（USD 恒为 1 基准），空 = 不启用换算，
-- 价格按商品各自 currency 字段直出（行为与旧版一致）。
ALTER TABLE plg_website_setting ADD COLUMN default_currency VARCHAR(8) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN currency_rates TEXT NULL;
