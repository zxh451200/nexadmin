package core

import "strings"

// OrderBy 把请求里的排序参数翻成 SQL 的 ORDER BY 子句（表头点击排序的后端一半）。
//
// allow 是「请求值 → 列名」白名单：请求里的字符串只用来查表，永远拼不进 SQL。
// 没命中白名单就退回 fallback 列，方向仍按 desc 生效——用户在别的列点了一下却看到
// 「没变化」，比报错更让人困惑，退化成默认列至少顺序真的变了。
// 末尾统一补 id DESC：同值行（比如同一秒创建）在翻页时不至于来回跳。
//
// 用法（模块里只需这三行）：
//
//	var allow = map[string]string{"nickname": "nickname", "created_at": "created_at"}
//	tx.Order(core.OrderBy(q.Sort, q.Desc, allow, "created_at"))
func OrderBy(sort string, desc bool, allow map[string]string, fallback string) string {
	col, ok := allow[strings.ToLower(strings.TrimSpace(sort))]
	if !ok {
		col = fallback
	}
	dir := "DESC"
	if !desc {
		dir = "ASC"
	}
	return col + " " + dir + ", id DESC"
}
