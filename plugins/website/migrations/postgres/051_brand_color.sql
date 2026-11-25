-- 品牌强调色（浅色 / 深色皮肤各一列）：仅内置 default 主题消费，空 = 沿用主题内置配色。
ALTER TABLE plg_website_setting
  ADD COLUMN "brand_color" varchar(16) NOT NULL DEFAULT '',
  ADD COLUMN "brand_color_dark" varchar(16) NOT NULL DEFAULT '';
