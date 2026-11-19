-- 产品在线购买：站点开关 + 访客订单表（与 mysql/041 同构；id/时间戳/金额列同 024 惯例）。
ALTER TABLE plg_website_setting ADD COLUMN purchase_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE plg_website_order (
  id BIGINT PRIMARY KEY,
  order_no VARCHAR(40) NOT NULL,
  product_id BIGINT NOT NULL,
  product_slug VARCHAR(64) NOT NULL DEFAULT '',
  product_title VARCHAR(200) NOT NULL DEFAULT '',
  qty INT NOT NULL DEFAULT 1,
  unit_price DECIMAL(12,2) NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL DEFAULT '',
  amount_minor BIGINT NOT NULL DEFAULT 0,
  pay_currency VARCHAR(8) NOT NULL DEFAULT '',
  pay_amount_minor BIGINT NOT NULL DEFAULT 0,
  fx_rate_micro BIGINT NOT NULL DEFAULT 1000000,
  buyer_email VARCHAR(128) NOT NULL DEFAULT '',
  buyer_phone VARCHAR(64) NOT NULL DEFAULT '',
  remark VARCHAR(300) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  payment_no VARCHAR(40) NOT NULL DEFAULT '',
  paid_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  CONSTRAINT uk_website_order_no UNIQUE (order_no)
);
CREATE INDEX idx_website_order_status ON plg_website_order (status);
CREATE INDEX idx_website_order_product ON plg_website_order (product_id);
