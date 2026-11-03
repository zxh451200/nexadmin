-- 内容级 SEO 元字段（对标成熟 CMS 的页面/新闻 SEO 设置）：
-- seo_title 独立 meta 标题：留空回退「标题 - 站点名」；非默认语言存 i18n_json 的 seoTitle，本列只存默认语言。
-- og_image 社交分享图（仅单页；新闻沿用封面做 og:image），空 = 站点默认分享图。
-- noindex 关闭搜索引擎收录：<meta name="robots"> 与 sitemap 同步排除，站内列表照常展示。
ALTER TABLE plg_website_page ADD COLUMN seo_title VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE plg_website_page ADD COLUMN og_image VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plg_website_page ADD COLUMN noindex BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE plg_website_post ADD COLUMN seo_title VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE plg_website_post ADD COLUMN noindex BOOLEAN NOT NULL DEFAULT FALSE;
