-- 凭证级调用配额：0 表示不限流；正数表示 rate_window_sec 秒内的最大请求数。
ALTER TABLE sys_pat
  ADD COLUMN rate_limit INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN rate_window_sec INTEGER NOT NULL DEFAULT 60;

ALTER TABLE sys_openapi_app
  ADD COLUMN rate_limit INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN rate_window_sec INTEGER NOT NULL DEFAULT 60;
