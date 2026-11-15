-- 阿里交易/物流/规格等发品字段：一个 JSON 列承载（阶梯价、包装、供货能力、物流属性、样品、定制、分组、SKU），
-- 这些字段只在同步阿里时使用，拆成十几个列反而难维护；结构与校验在 plugins/website/ali_trade.go。
ALTER TABLE plg_website_product ADD COLUMN ali_trade TEXT NULL;
