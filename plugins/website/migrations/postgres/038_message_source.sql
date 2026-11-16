-- 询盘来源与名片同意：商品详情页的「发送询盘」会带上来源商品，后台据此推荐同品类；
-- card_consent 记录买家是否勾选「同意与供应商共享名片信息」（默认勾选，可取消）。
ALTER TABLE plg_website_message ADD COLUMN product_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE plg_website_message ADD COLUMN card_consent BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_plg_website_message_product ON plg_website_message (product_id);
