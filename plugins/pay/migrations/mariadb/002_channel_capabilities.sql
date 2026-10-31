-- 支付中台渠道能力扩展：退款补偿查询、预授权真实渠道、账单驱动对账、
-- 渠道代付、outbox 死信、回调通知号取证。只增不改；后续变更用 003_*.sql。

-- 退款：发起时发给渠道的幂等号必须落库，补偿查询要带同一个号渠道才认得出是同一笔；
-- sync_attempts/last_sync_at 是 sweeper 补查的节流与留痕。
ALTER TABLE plg_pay_refund ADD COLUMN idempotency_key VARCHAR(80) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_refund ADD COLUMN sync_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE plg_pay_refund ADD COLUMN last_sync_at TIMESTAMP NULL;

-- 回调：渠道给这条通知的流水号，只做取证与排障检索，不做唯一约束。
ALTER TABLE plg_pay_callback ADD COLUMN notify_id VARCHAR(128) NOT NULL DEFAULT '';
CREATE INDEX idx_pay_callback_notify ON plg_pay_callback (notify_id);

-- 预授权：真实渠道冻结需要渠道侧买家标识；失败原因用于「渠道拒绝」与「本地状态」区分。
ALTER TABLE plg_pay_auth ADD COLUMN payer_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_auth ADD COLUMN fail_reason VARCHAR(200) NOT NULL DEFAULT '';

-- 对账：账单下载只留摘要与行数（账单本体可达数 MB，不落库）。
ALTER TABLE plg_pay_reconcile_task ADD COLUMN bill_digest VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_reconcile_task ADD COLUMN bill_rows INT NOT NULL DEFAULT 0;
ALTER TABLE plg_pay_reconcile_task ADD COLUMN fail_reason VARCHAR(200) NOT NULL DEFAULT '';

-- 打款：渠道代付路径的批次号与逐笔结论；人工回执号（receipt_no）保留为兜底路径。
ALTER TABLE plg_pay_payout_batch ADD COLUMN channel_code VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_payout_batch ADD COLUMN channel_batch_no VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_payout_batch ADD COLUMN fail_reason VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_payout_item ADD COLUMN receiver_id VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_payout_item ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'pending';
ALTER TABLE plg_pay_payout_item ADD COLUMN channel_no VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_payout_item ADD COLUMN fail_reason VARCHAR(200) NOT NULL DEFAULT '';

-- 出站箱：投递失败留痕，超过上限进死信，由管理端人工处置。
ALTER TABLE plg_pay_outbox ADD COLUMN last_error VARCHAR(500) NOT NULL DEFAULT '';
ALTER TABLE plg_pay_outbox ADD COLUMN dead_at TIMESTAMP NULL;
