-- 字典项的按语言文案：{"ja-JP":{"label":"…"}}，默认语言仍在 dict_label 列。
-- label_en 保留做升级兼容（读取时当英文兜底），不再写入新数据。
ALTER TABLE sys_dict_data ADD COLUMN i18n_json TEXT NULL;
