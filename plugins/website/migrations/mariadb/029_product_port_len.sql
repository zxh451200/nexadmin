-- 装运港口支持多选（逗号拼接，如 "Shenzhen, Ningbo"），与 payment_terms 的 100 宽度对齐。
ALTER TABLE plg_website_product MODIFY COLUMN port VARCHAR(100) NOT NULL DEFAULT '';
