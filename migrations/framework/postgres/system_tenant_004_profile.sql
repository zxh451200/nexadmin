-- 租户资料与品牌：企业信息、联系人、租户独立 logo（留空 = 沿用平台站点设置）。
ALTER TABLE sys_tenant ADD COLUMN company_name VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE sys_tenant ADD COLUMN contact_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE sys_tenant ADD COLUMN contact_phone VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE sys_tenant ADD COLUMN contact_email VARCHAR(200) NOT NULL DEFAULT '';
ALTER TABLE sys_tenant ADD COLUMN logo_url VARCHAR(500) NOT NULL DEFAULT '';
