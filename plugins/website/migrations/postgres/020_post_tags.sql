-- 标签：与分类分层的第二个维度（分类=编辑部栏目，标签=自由主题词），对齐 WordPress/Ghost 的两层内容分类法。
CREATE TABLE plg_website_tag (
  id BIGINT PRIMARY KEY,
  name VARCHAR(64) NOT NULL DEFAULT '',
  slug VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_tag_slug ON plg_website_tag (slug);

-- 新闻-标签关联：一篇新闻可打多个标签。
CREATE TABLE plg_website_post_tag (
  post_id BIGINT NOT NULL,
  tag_id BIGINT NOT NULL,
  PRIMARY KEY (post_id, tag_id)
);
