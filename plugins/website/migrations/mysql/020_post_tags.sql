-- 标签：与分类分层的第二个维度（分类=编辑部栏目，标签=自由主题词），对齐 WordPress/Ghost 的两层内容分类法。
CREATE TABLE plg_website_tag (
  id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL DEFAULT '',
  slug VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_website_tag_slug (slug)
);

-- 新闻-标签关联：一篇新闻可打多个标签。
CREATE TABLE plg_website_post_tag (
  post_id BIGINT NOT NULL,
  tag_id BIGINT NOT NULL,
  PRIMARY KEY (post_id, tag_id)
);
