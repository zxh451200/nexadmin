-- 导入台账记录提交时的界面语言：异步执行没有请求上下文，错误文案按它翻译。
ALTER TABLE plg_imports_job ADD COLUMN lang VARCHAR(16) NOT NULL DEFAULT '';
