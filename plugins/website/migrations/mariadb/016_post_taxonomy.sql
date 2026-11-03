-- 新闻分类（扁平单层）：给新闻一个组织维度，对标成熟 CMS 的分类能力。
CREATE TABLE plg_website_category (
  id BIGINT NOT NULL,
  title VARCHAR(100) NOT NULL DEFAULT '',
  slug VARCHAR(64) NOT NULL DEFAULT '',
  sort INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_website_category_slug (slug)
);

-- 新闻-分类关联：一篇新闻可挂多个分类。
CREATE TABLE plg_website_post_category (
  post_id BIGINT NOT NULL,
  category_id BIGINT NOT NULL,
  PRIMARY KEY (post_id, category_id)
);

-- 新闻 SEO 关键词：与站点设置 seo_keywords 对齐，详情页非空时优先用它生成 meta keywords；只存默认语言。
ALTER TABLE plg_website_post ADD COLUMN seo_keywords VARCHAR(300) NOT NULL DEFAULT '';
