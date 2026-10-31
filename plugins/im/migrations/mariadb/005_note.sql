-- 会话内部备注：只给坐席看，不推给访客。
ALTER TABLE plg_im_conversation ADD COLUMN note VARCHAR(500) NOT NULL DEFAULT '';
