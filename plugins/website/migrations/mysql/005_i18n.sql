-- 官网内容多语言：每种内容一行 JSON 保存非默认语言译文（按语言 code 分组）。
-- 空值/非法 JSON 时公开页回退到原文，不影响旧数据。
ALTER TABLE plg_website_setting ADD COLUMN i18n_json TEXT NULL;
ALTER TABLE plg_website_nav ADD COLUMN i18n_json TEXT NULL;
ALTER TABLE plg_website_feature ADD COLUMN i18n_json TEXT NULL;
ALTER TABLE plg_website_page ADD COLUMN i18n_json TEXT NULL;
ALTER TABLE plg_website_post ADD COLUMN i18n_json TEXT NULL;
