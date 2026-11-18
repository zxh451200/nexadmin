package response

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ParseSort 读表头排序的两个查询参数：?sort=<字段>&desc=true|false。
//
// 字段本身不做校验（那是各模块白名单的事，见 core.OrderBy），这里只负责把
// 一串 query 归一成 (字段, 是否倒序)。前端受控组件会把 sort 写成 "created_at"、
// desc 写成 "true"/"false"；顺手兼容 1/0 与 true/false 的字面量。
func ParseSort(c *gin.Context) (sortField string, desc bool) {
	sortField = strings.TrimSpace(c.Query("sort"))
	raw := strings.TrimSpace(c.Query("desc"))
	if raw == "" {
		return sortField, false
	}
	if v, err := strconv.ParseBool(raw); err == nil {
		return sortField, v
	}
	return sortField, false
}
