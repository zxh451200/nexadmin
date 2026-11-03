-- 站点标题后缀：拼在页面 <title> 的「标题」之后，替换默认的「站点名」。
-- 空 = 沿用「{标题} - {站点名}」的内置组合。限 100 字符。
ALTER TABLE plg_website_setting ADD COLUMN title_suffix VARCHAR(100) NOT NULL DEFAULT '';
