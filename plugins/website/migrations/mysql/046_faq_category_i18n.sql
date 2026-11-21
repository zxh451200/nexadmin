-- FAQ 与两类栏目的非默认语言译文：{"en-US":{"question":"…"}}。
-- 原文仍在原列（判重、slug 都按原文），空译文按原有逻辑回退原文；FAQ 在缺译语言会上不展示。
ALTER TABLE plg_website_faq ADD COLUMN i18n_json TEXT NULL;
ALTER TABLE plg_website_category ADD COLUMN i18n_json TEXT NULL;
ALTER TABLE plg_website_product_category ADD COLUMN i18n_json TEXT NULL;
