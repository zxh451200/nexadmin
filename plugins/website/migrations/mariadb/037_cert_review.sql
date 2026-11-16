-- 商品认证资质（CE / UL / UN38.3 等）与供应商实力档案，外加商品评价表。
-- 认证存 JSON 数组文本（[]string）：明细没有固定键，走文本列三库一致。
ALTER TABLE plg_website_product ADD COLUMN certifications TEXT NULL;

-- 供应商资质：对标阿里商详供应商卡的四项实力数据。区间写法（500+ / US$10M-US$50M）用字符串。
ALTER TABLE plg_website_setting ADD COLUMN established_year INT NOT NULL DEFAULT 0;
ALTER TABLE plg_website_setting ADD COLUMN staff_count VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN annual_revenue VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN main_markets VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN response_time VARCHAR(32) NOT NULL DEFAULT '';

-- 商品评价：匿名官网没有登录买家，评价由运营录入真实回访内容，公开站只读。
CREATE TABLE plg_website_product_review (
  id BIGINT PRIMARY KEY,
  product_id BIGINT NOT NULL,
  author VARCHAR(64) NOT NULL DEFAULT '',
  country VARCHAR(64) NOT NULL DEFAULT '',
  rating INT NOT NULL DEFAULT 5,
  content TEXT NULL,
  verified TINYINT(1) NOT NULL DEFAULT 0,
  quantity VARCHAR(64) NOT NULL DEFAULT '',
  published TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_website_review_product ON plg_website_product_review (product_id);
