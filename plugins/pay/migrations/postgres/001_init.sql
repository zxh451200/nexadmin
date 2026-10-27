-- 支付中台插件（02-openapi-pay-v1.yaml）：资金事实一律租户级 plg_pay_*；
-- 渠道 code 全局唯一（公开回调只带 channelCode）；channel_txn_id 可 NULL 并与渠道号
-- 组成唯一索引（修复 nexHotel 渠道流水号可重复入库的缺陷 C）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_pay_order (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  payment_no VARCHAR(40) NOT NULL,
  biz_type VARCHAR(32) NOT NULL,
  biz_no VARCHAR(64) NOT NULL,
  subject_type VARCHAR(32) NOT NULL DEFAULT '',
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  subject VARCHAR(200) NOT NULL,
  amount_minor BIGINT NOT NULL,
  refunded_minor BIGINT NOT NULL DEFAULT 0,
  fee_minor BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL,
  fx_rate_micro BIGINT NOT NULL DEFAULT 1000000,
  amount_orig_minor BIGINT NOT NULL DEFAULT 0,
  orig_currency VARCHAR(8) NOT NULL DEFAULT '',
  channel_code VARCHAR(32) NOT NULL,
  method VARCHAR(32) NOT NULL DEFAULT '',
  scene VARCHAR(32) NOT NULL DEFAULT '',
  channel_txn_id VARCHAR(128) NULL,
  prepay_id VARCHAR(128) NOT NULL DEFAULT '',
  code_url VARCHAR(512) NOT NULL DEFAULT '',
  gateway_status VARCHAR(64) NOT NULL DEFAULT '',
  fail_reason VARCHAR(200) NOT NULL DEFAULT '',
  payer_id VARCHAR(64) NOT NULL DEFAULT '',
  payer_open_id VARCHAR(128) NOT NULL DEFAULT '',
  operator_id VARCHAR(64) NOT NULL DEFAULT '',
  metadata_json TEXT,
  status VARCHAR(24) NOT NULL,
  paid_at TIMESTAMP NULL,
  expire_at TIMESTAMP NULL,
  closed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_order_no ON plg_pay_order (payment_no);
CREATE UNIQUE INDEX uk_pay_order_biz ON plg_pay_order (tenant_id, biz_type, biz_no);
CREATE UNIQUE INDEX uk_pay_order_channel_txn ON plg_pay_order (channel_code, channel_txn_id);
CREATE INDEX idx_pay_order_tenant ON plg_pay_order (tenant_id, id);
CREATE INDEX idx_pay_order_status ON plg_pay_order (status, expire_at);
CREATE INDEX idx_pay_order_subject ON plg_pay_order (tenant_id, subject_id);
CREATE INDEX idx_pay_order_channel_paid ON plg_pay_order (tenant_id, channel_code, paid_at);

CREATE TABLE plg_pay_channel (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL,
  name VARCHAR(64) NOT NULL,
  provider VARCHAR(32) NOT NULL,
  method VARCHAR(32) NOT NULL DEFAULT '',
  scene VARCHAR(32) NOT NULL DEFAULT '',
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  fee_rate_bp BIGINT NOT NULL DEFAULT 0,
  notify_url VARCHAR(255) NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort INT NOT NULL DEFAULT 0,
  config_json VARCHAR(1024) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_channel_code ON plg_pay_channel (code);
CREATE INDEX idx_pay_channel_tenant ON plg_pay_channel (tenant_id, subject_id);

CREATE TABLE plg_pay_callback (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  channel_code VARCHAR(32) NOT NULL,
  payment_no VARCHAR(40) NOT NULL DEFAULT '',
  ok BOOLEAN NOT NULL DEFAULT FALSE,
  result VARCHAR(32) NOT NULL DEFAULT '',
  payload TEXT,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_pay_callback_no ON plg_pay_callback (channel_code, payment_no);

CREATE TABLE plg_pay_refund (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  refund_no VARCHAR(40) NOT NULL,
  payment_no VARCHAR(40) NOT NULL,
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  biz_type VARCHAR(32) NOT NULL,
  biz_no VARCHAR(64) NOT NULL,
  amount_minor BIGINT NOT NULL,
  fee_refund_minor BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL,
  fx_rate_micro BIGINT NOT NULL DEFAULT 1000000,
  status VARCHAR(16) NOT NULL,
  channel_refund_id VARCHAR(128) NOT NULL DEFAULT '',
  reason VARCHAR(200) NOT NULL DEFAULT '',
  fail_reason VARCHAR(200) NOT NULL DEFAULT '',
  operator_id VARCHAR(64) NOT NULL DEFAULT '',
  succeeded_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_refund_no ON plg_pay_refund (refund_no);
CREATE UNIQUE INDEX uk_pay_refund_biz ON plg_pay_refund (tenant_id, biz_type, biz_no);
CREATE INDEX idx_pay_refund_payment ON plg_pay_refund (payment_no);
CREATE INDEX idx_pay_refund_status ON plg_pay_refund (status);
CREATE INDEX idx_pay_refund_succeeded ON plg_pay_refund (tenant_id, status, succeeded_at);

CREATE TABLE plg_pay_auth (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  auth_no VARCHAR(40) NOT NULL,
  biz_type VARCHAR(32) NOT NULL,
  biz_no VARCHAR(64) NOT NULL,
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  channel_code VARCHAR(32) NOT NULL,
  method VARCHAR(32) NOT NULL DEFAULT '',
  status VARCHAR(20) NOT NULL,
  amount_minor BIGINT NOT NULL,
  captured_minor BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL,
  fx_rate_micro BIGINT NOT NULL DEFAULT 1000000,
  channel_auth_no VARCHAR(128) NOT NULL DEFAULT '',
  card_last4 VARCHAR(8) NOT NULL DEFAULT '',
  remark VARCHAR(500) NOT NULL DEFAULT '',
  payment_no VARCHAR(40) NOT NULL DEFAULT '',
  expires_at TIMESTAMP NULL,
  voided_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_auth_no ON plg_pay_auth (auth_no);
CREATE UNIQUE INDEX uk_pay_auth_biz ON plg_pay_auth (tenant_id, biz_type, biz_no);
CREATE INDEX idx_pay_auth_status ON plg_pay_auth (status, expires_at);

CREATE TABLE plg_pay_reconcile_task (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  channel_code VARCHAR(32) NOT NULL,
  bill_date VARCHAR(10) NOT NULL,
  status VARCHAR(16) NOT NULL,
  total_local_minor BIGINT NOT NULL DEFAULT 0,
  total_channel_minor BIGINT NOT NULL DEFAULT 0,
  diff_count INT NOT NULL DEFAULT 0,
  source VARCHAR(16) NOT NULL DEFAULT 'manual',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_reconcile_task ON plg_pay_reconcile_task (tenant_id, subject_id, channel_code, bill_date);

CREATE TABLE plg_pay_reconcile_diff (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  task_id BIGINT NOT NULL,
  kind VARCHAR(16) NOT NULL,
  payment_no VARCHAR(40) NOT NULL DEFAULT '',
  refund_no VARCHAR(40) NOT NULL DEFAULT '',
  local_amount_minor BIGINT NOT NULL DEFAULT 0,
  channel_amount_minor BIGINT NOT NULL DEFAULT 0,
  diff_amount_minor BIGINT NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL DEFAULT 'open',
  resolve_note VARCHAR(500) NOT NULL DEFAULT '',
  resolved_by VARCHAR(64) NOT NULL DEFAULT '',
  resolved_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_pay_reconcile_diff_task ON plg_pay_reconcile_diff (task_id, status);

CREATE TABLE plg_pay_commission_rule (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  biz_type VARCHAR(32) NOT NULL,
  rate_bp BIGINT NOT NULL DEFAULT 0,
  fixed_minor BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_commission_rule ON plg_pay_commission_rule (tenant_id, subject_id, biz_type);

CREATE TABLE plg_pay_settlement (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  subject_id VARCHAR(64) NOT NULL DEFAULT '',
  period_start VARCHAR(10) NOT NULL,
  period_end VARCHAR(10) NOT NULL,
  gross_minor BIGINT NOT NULL DEFAULT 0,
  commission_minor BIGINT NOT NULL DEFAULT 0,
  refund_minor BIGINT NOT NULL DEFAULT 0,
  fee_minor BIGINT NOT NULL DEFAULT 0,
  net_minor BIGINT NOT NULL DEFAULT 0,
  order_count BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_settlement ON plg_pay_settlement (tenant_id, subject_id, period_start, period_end, currency);

CREATE TABLE plg_pay_payout_batch (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  batch_no VARCHAR(40) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  total_minor BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL,
  settlement_count INT NOT NULL DEFAULT 0,
  method VARCHAR(32) NOT NULL DEFAULT '',
  note VARCHAR(500) NOT NULL DEFAULT '',
  receipt_no VARCHAR(64) NOT NULL DEFAULT '',
  paid_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_payout_no ON plg_pay_payout_batch (batch_no);

CREATE TABLE plg_pay_payout_item (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  batch_id BIGINT NOT NULL,
  settlement_id BIGINT NOT NULL,
  net_minor BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_payout_item_settlement ON plg_pay_payout_item (settlement_id);
CREATE INDEX idx_pay_payout_item_batch ON plg_pay_payout_item (batch_id);

CREATE TABLE plg_pay_outbox (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  event_id VARCHAR(40) NOT NULL,
  event_name VARCHAR(48) NOT NULL,
  payload TEXT,
  status VARCHAR(16) NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMP NOT NULL,
  sent_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_outbox_event ON plg_pay_outbox (event_id);
CREATE INDEX idx_pay_outbox_retry ON plg_pay_outbox (status, next_retry_at);

CREATE TABLE plg_pay_idem (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  idem_key VARCHAR(64) NOT NULL,
  path VARCHAR(128) NOT NULL DEFAULT '',
  req_hash VARCHAR(64) NOT NULL,
  resp_status INT NOT NULL DEFAULT 0,
  resp_body TEXT,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_pay_idem ON plg_pay_idem (tenant_id, idem_key);
