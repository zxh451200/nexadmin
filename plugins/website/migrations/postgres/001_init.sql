-- 企业官网插件：plg_website_setting（站点设置，单行）、plg_website_nav（导航）、
-- plg_website_feature（特性卡片）、plg_website_page（自定义页）、plg_website_post（新闻）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_website_setting (
  id BIGINT PRIMARY KEY,
  site_name VARCHAR(64) NOT NULL DEFAULT '',
  site_desc VARCHAR(300) NOT NULL DEFAULT '',
  logo_url VARCHAR(255) NOT NULL DEFAULT '',
  home_title VARCHAR(200) NOT NULL DEFAULT '',
  home_subtitle VARCHAR(500) NOT NULL DEFAULT '',
  home_cta_label VARCHAR(64) NOT NULL DEFAULT '',
  home_cta_href VARCHAR(255) NOT NULL DEFAULT '',
  seo_keywords VARCHAR(300) NOT NULL DEFAULT '',
  contact_email VARCHAR(128) NOT NULL DEFAULT '',
  icp VARCHAR(128) NOT NULL DEFAULT '',
  footer_note VARCHAR(300) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

CREATE TABLE plg_website_nav (
  id BIGINT PRIMARY KEY,
  label VARCHAR(64) NOT NULL,
  href VARCHAR(255) NOT NULL,
  sort INT NOT NULL DEFAULT 0,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

CREATE TABLE plg_website_feature (
  id BIGINT PRIMARY KEY,
  icon VARCHAR(64) NOT NULL DEFAULT '',
  title VARCHAR(128) NOT NULL,
  description VARCHAR(500) NOT NULL DEFAULT '',
  sort INT NOT NULL DEFAULT 0,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

CREATE TABLE plg_website_page (
  id BIGINT PRIMARY KEY,
  slug VARCHAR(64) NOT NULL,
  title VARCHAR(200) NOT NULL,
  seo_desc VARCHAR(300) NOT NULL DEFAULT '',
  content TEXT,
  published BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_page_slug ON plg_website_page (slug);

CREATE TABLE plg_website_post (
  id BIGINT PRIMARY KEY,
  slug VARCHAR(64) NOT NULL,
  title VARCHAR(200) NOT NULL,
  cover_url VARCHAR(255) NOT NULL DEFAULT '',
  summary VARCHAR(500) NOT NULL DEFAULT '',
  content TEXT,
  published BOOLEAN NOT NULL DEFAULT FALSE,
  published_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_post_slug ON plg_website_post (slug);
CREATE INDEX idx_plg_website_post_pub ON plg_website_post (published, published_at);
