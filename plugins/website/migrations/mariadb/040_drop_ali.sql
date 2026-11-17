-- 移除阿里国际站对接（同步发品/反向拉取/模板互通）残留的表与列。
-- ali_trade 承载的阶梯价/包装/供货/样品/定制/SKU 随对接一并下线，商详不再渲染「交易信息」块。

DROP TABLE IF EXISTS plg_website_ali_config;
DROP TABLE IF EXISTS plg_website_ali_column;

DROP INDEX idx_plg_website_product_alibaba ON plg_website_product;
ALTER TABLE plg_website_product
  DROP COLUMN alibaba_product_id,
  DROP COLUMN alibaba_category_id,
  DROP COLUMN ali_trade,
  DROP COLUMN sync_status,
  DROP COLUMN synced_at,
  DROP COLUMN sync_error;
