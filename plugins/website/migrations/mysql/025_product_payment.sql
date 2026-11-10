-- 产品交易条件：付款方式（对标阿里商详「交易信息」块的 Payment Terms）。
ALTER TABLE plg_website_product ADD COLUMN payment_terms VARCHAR(100) NOT NULL DEFAULT '';
