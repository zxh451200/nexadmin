-- 商品运费金额：与价格同币种，0 = 未报价（详情页仍标「待定」）。
ALTER TABLE plg_website_product ADD COLUMN freight_amount DECIMAL(12,2) NOT NULL DEFAULT 0;
