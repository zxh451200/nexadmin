-- 主题演示内容标记：demo_of 记录该行由哪个主题插件的演示数据创建。
-- 切换主题重新导入时，其它主题的演示行会被清除（用户自建内容 demo_of='' 永远不动）。
ALTER TABLE plg_website_product ADD COLUMN demo_of VARCHAR(48) NOT NULL DEFAULT '';
ALTER TABLE plg_website_post ADD COLUMN demo_of VARCHAR(48) NOT NULL DEFAULT '';
ALTER TABLE plg_website_page ADD COLUMN demo_of VARCHAR(48) NOT NULL DEFAULT '';
ALTER TABLE plg_website_category ADD COLUMN demo_of VARCHAR(48) NOT NULL DEFAULT '';
ALTER TABLE plg_website_product_category ADD COLUMN demo_of VARCHAR(48) NOT NULL DEFAULT '';
