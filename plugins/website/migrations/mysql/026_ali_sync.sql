-- 阿里国际站同步（二期）：开放平台凭据（单行）与商品的同步辅助列。
CREATE TABLE plg_website_ali_config (
  id BIGINT NOT NULL,
  app_key VARCHAR(64) NOT NULL DEFAULT '',
  app_secret_enc TEXT NULL,
  session_key_enc TEXT NULL,
  api_gateway VARCHAR(100) NOT NULL DEFAULT 'https://gw.api.taobao.com/router/rest',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);

-- 商品的阿里类目绑定（同步对话框选叶子类目后回填）与最近一次同步失败原因。
ALTER TABLE plg_website_product ADD COLUMN alibaba_category_id VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE plg_website_product ADD COLUMN sync_error VARCHAR(500) NOT NULL DEFAULT '';
