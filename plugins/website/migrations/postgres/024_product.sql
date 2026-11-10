-- 产品中心：类目（树形，对标阿里「商品分组」）与商品（对标阿里发品的字段集）。
CREATE TABLE plg_website_product_category (
  id BIGINT PRIMARY KEY,
  parent_id BIGINT NOT NULL DEFAULT 0,
  title VARCHAR(100) NOT NULL DEFAULT '',
  slug VARCHAR(64) NOT NULL DEFAULT '',
  sort INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_product_category_slug ON plg_website_product_category (slug);
CREATE INDEX idx_plg_website_prod_cat_parent ON plg_website_product_category (parent_id);

CREATE TABLE plg_website_product (
  id BIGINT PRIMARY KEY,
  slug VARCHAR(64) NOT NULL DEFAULT '',
  title VARCHAR(200) NOT NULL DEFAULT '',
  keywords VARCHAR(300) NOT NULL DEFAULT '',
  summary VARCHAR(500) NOT NULL DEFAULT '',
  content TEXT NULL,
  seo_title VARCHAR(200) NOT NULL DEFAULT '',
  seo_desc VARCHAR(300) NOT NULL DEFAULT '',
  seo_keywords VARCHAR(300) NOT NULL DEFAULT '',
  noindex BOOLEAN NOT NULL DEFAULT FALSE,
  i18n_json TEXT NULL,
  published_json TEXT NULL,
  cover_url VARCHAR(255) NOT NULL DEFAULT '',
  images TEXT NULL,
  video_url VARCHAR(255) NOT NULL DEFAULT '',
  price_min DECIMAL(12,2) NOT NULL DEFAULT 0,
  price_max DECIMAL(12,2) NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL DEFAULT 'USD',
  price_unit VARCHAR(32) NOT NULL DEFAULT '',
  moq INT NOT NULL DEFAULT 0,
  lead_time_days INT NOT NULL DEFAULT 0,
  port VARCHAR(64) NOT NULL DEFAULT '',
  specs TEXT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  alibaba_product_id VARCHAR(64) NOT NULL DEFAULT '',
  sync_status VARCHAR(16) NOT NULL DEFAULT 'none',
  synced_at TIMESTAMP NULL,
  views BIGINT NOT NULL DEFAULT 0,
  author VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_product_slug ON plg_website_product (slug);
CREATE INDEX idx_plg_website_product_status ON plg_website_product (status);
CREATE INDEX idx_plg_website_product_alibaba ON plg_website_product (alibaba_product_id);

-- 商品-类目关联：一个商品可挂多个类目。
CREATE TABLE plg_website_product_category_rel (
  product_id BIGINT NOT NULL,
  category_id BIGINT NOT NULL,
  PRIMARY KEY (product_id, category_id)
);
