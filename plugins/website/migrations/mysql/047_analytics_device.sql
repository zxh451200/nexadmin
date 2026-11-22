-- 官网访问统计补设备画像：浏览行带上 UA 解析出的浏览器/操作系统（匿名口径不变：
-- 不含 IP 与个人身份，bot 不采集）。聚合在坐席端按列 GROUP BY，无需预聚合表。
ALTER TABLE plg_website_page_view
  ADD COLUMN browser VARCHAR(32) NOT NULL DEFAULT '' AFTER session_key,
  ADD COLUMN os VARCHAR(32) NOT NULL DEFAULT '' AFTER browser;
