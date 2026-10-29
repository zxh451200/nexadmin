-- 新闻的 SEO 描述：与单页的 seo_desc 对齐，详情页优先用它生成 meta description。
-- 非默认语言的取值存在 i18n_json 的 seoDesc 字段里，这一列只存默认语言。
ALTER TABLE plg_website_post ADD COLUMN seo_desc VARCHAR(300) NOT NULL DEFAULT '';
