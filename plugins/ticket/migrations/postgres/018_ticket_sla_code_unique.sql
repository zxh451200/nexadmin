-- SLA 策略编码封口：013 给 plg_ticket_sla.code 建的是**故意非唯一**的 idx_plg_ticket_sla_code，
-- 于是 SaveSLAPolicy 的「先查后建」（plugins/ticket/sla.go）在并发下能把同一个 code 落成两行；
-- 读侧一律 Order("id ASC").Take，重复行从不参与计时、只在列表里露头，被静默掩盖。
-- 这里先清数据、再加唯一索引：同一 code 只保留 id 最小的那一行——正是今天读侧实际生效的那一行，
-- 删掉的重复行不改变任何队列的计时行为，只把列表里的幽灵行清掉。
-- 唯一轴是 code 本身：plg_ticket_sla 是平台级配置表（017 明确没给它加 tenant_id，与
-- plg_ticket_group / plg_ticket_field 同一条线），所以没有 (tenant_id, code) 这一说。
-- PG 侧唯一约束走独立 CREATE UNIQUE INDEX（内联 UNIQUE (col) 会被三引擎结构闸当成一列，
-- 报 column count mismatch，见 014 的写法）；自连接删除用 USING 而不是 MySQL 的多表 DELETE。
-- 迁移里的 DELETE 只会被 core/migrate 记一条 WARN、不阻断（见 core/migrate/migrate.go 的 DML 口径）。只增不改。
DELETE FROM plg_ticket_sla dup
  USING plg_ticket_sla keep
  WHERE keep.code = dup.code AND keep.id < dup.id;
CREATE UNIQUE INDEX uk_plg_ticket_sla_code ON plg_ticket_sla (code);
