-- 规则的条件与动作补标签维度：match_tags 按标签命中（多个标签之间是「与」），
-- add_tags 命中后追加标签。两列都沿用 009 的「,a,b,」包裹串存储，边界逗号避免 vip 命中 viptag。只增不改。
ALTER TABLE plg_ticket_rule ADD COLUMN match_tags VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plg_ticket_rule ADD COLUMN add_tags VARCHAR(255) NOT NULL DEFAULT '';
