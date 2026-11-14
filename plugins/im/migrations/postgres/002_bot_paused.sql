-- 会话加「人工接管」标记：坐席认领或访客转人工后 AI 停答（kb 插件读它判断要不要回）。只增不改。
ALTER TABLE plg_im_conversation ADD COLUMN bot_paused BOOLEAN NOT NULL DEFAULT FALSE;
