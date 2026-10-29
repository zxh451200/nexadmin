-- 按语言发布：非默认语言的发布状态一行 JSON 保存（{"en-US":true,"ja-JP":false}）。
-- 默认语言的发布态就是已有的 published 列；本列为空时所有语言沿用 published，
-- 升级前保存的内容不会因为新规则突然下线。
ALTER TABLE plg_website_post ADD COLUMN published_json TEXT NULL;
ALTER TABLE plg_website_page ADD COLUMN published_json TEXT NULL;
