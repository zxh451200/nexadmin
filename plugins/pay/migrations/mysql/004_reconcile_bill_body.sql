-- 对账任务归档渠道账单解析行（CSV）：财务复核需要可下载的账单留存。只增不改。
ALTER TABLE plg_pay_reconcile_task ADD COLUMN bill_body LONGTEXT NULL;
