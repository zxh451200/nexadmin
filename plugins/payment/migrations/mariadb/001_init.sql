-- 支付中心插件：plg_payment_order（支付单）、plg_payment_channel（渠道配置）、
-- plg_payment_callback（回调审计）、plg_payment_refund（退款单）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_payment_order (
  id BIGINT NOT NULL,
  payment_no VARCHAR(40) NOT NULL,
  biz_type VARCHAR(32) NOT NULL,
  biz_no VARCHAR(64) NOT NULL,
  subject VARCHAR(200) NOT NULL,
  amount_minor BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL,
  channel VARCHAR(32) NOT NULL,
  channel_txn_id VARCHAR(128) NOT NULL DEFAULT '',
  status VARCHAR(24) NOT NULL,
  fail_reason VARCHAR(200) NOT NULL DEFAULT '',
  refunded_minor BIGINT NOT NULL DEFAULT 0,
  paid_at DATETIME(3) NULL,
  expire_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_payment_order_no (payment_no),
  UNIQUE KEY uk_plg_payment_order_biz (biz_type, biz_no)
);
CREATE INDEX idx_plg_payment_order_status ON plg_payment_order (status, expire_at);

CREATE TABLE plg_payment_channel (
  id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL,
  name VARCHAR(64) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort INT NOT NULL DEFAULT 0,
  config_json VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_payment_channel_code (code)
);

CREATE TABLE plg_payment_callback (
  id BIGINT NOT NULL,
  channel VARCHAR(32) NOT NULL,
  payment_no VARCHAR(40) NOT NULL DEFAULT '',
  ok TINYINT(1) NOT NULL DEFAULT 0,
  result VARCHAR(200) NOT NULL DEFAULT '',
  payload TEXT,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_payment_callback_no ON plg_payment_callback (channel, payment_no);

CREATE TABLE plg_payment_refund (
  id BIGINT NOT NULL,
  refund_no VARCHAR(40) NOT NULL,
  payment_no VARCHAR(40) NOT NULL,
  biz_type VARCHAR(32) NOT NULL,
  biz_no VARCHAR(64) NOT NULL,
  amount_minor BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL,
  reason VARCHAR(200) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL,
  channel_refund_id VARCHAR(128) NOT NULL DEFAULT '',
  succeeded_at DATETIME(3) NULL,
  operator VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_payment_refund_no (refund_no),
  UNIQUE KEY uk_plg_payment_refund_biz (biz_type, biz_no)
);
CREATE INDEX idx_plg_payment_refund_payment ON plg_payment_refund (payment_no);
CREATE INDEX idx_plg_payment_refund_status ON plg_payment_refund (status);
