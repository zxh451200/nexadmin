-- 产品在线购买：站点开关 + 访客订单表（与 mysql/041 同构）。
ALTER TABLE plg_website_setting ADD COLUMN purchase_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE plg_website_order (
  id BIGINT NOT NULL PRIMARY KEY,
  order_no VARCHAR(40) NOT NULL,
  product_id BIGINT NOT NULL,
  product_slug VARCHAR(64) NOT NULL,
  product_title VARCHAR(200) NOT NULL,
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
  paid_at DATETIME NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  UNIQUE KEY uk_website_order_no (order_no),
  KEY idx_website_order_status (status),
  KEY idx_website_order_product (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
