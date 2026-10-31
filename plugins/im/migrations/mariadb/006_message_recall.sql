-- 消息撤回：记录撤回时间；撤回后正文清空、kind 改为 system。
ALTER TABLE plg_im_message ADD COLUMN recalled_at DATETIME(3) NULL;
