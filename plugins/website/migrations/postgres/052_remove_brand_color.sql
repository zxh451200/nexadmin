-- 移除官网站点设置中的品牌强调色配置，统一沿用主题内置配色。
ALTER TABLE plg_website_setting
  DROP COLUMN IF EXISTS "brand_color",
  DROP COLUMN IF EXISTS "brand_color_dark";
