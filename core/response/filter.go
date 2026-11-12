package response

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/pkg/idgen"
)

// MaxFilterValues 是单个筛选参数一次允许点名的取值上限。
//
// 筛选枚举值来自选项列表（字典/类目/渠道），正常个位数；给个宽松上限防止把
// 无穷长的 CSV 灌进 IN (...)，超出的尾部直接丢弃（筛选是读操作，宽容语义）。
const MaxFilterValues = 20

// parseCSV 把「逗号拼接多值」筛选参数拆成去空、去重后的取值列表，超上限封顶。
// raw 为空返回 nil（= 不过滤）。前后兼容：单值 "draft" 就是 ["draft"]。
func parseCSV(raw string, max int) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0, max)
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if !slices.Contains(out, part) {
			out = append(out, part)
		}
		if len(out) >= max {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ParseList 解析枚举类筛选参数（如 ?status=draft,online）。
// 与批量删除的 ParseIDs 不同：筛选是读操作，非法/超限不报错，能解析多少算多少。
func ParseList(raw string, max int) []string {
	return parseCSV(raw, max)
}

// ParseListQuery 读当前请求的枚举筛选参数并解析。
func ParseListQuery(c *gin.Context, name string, max int) []string {
	return ParseList(c.Query(name), max)
}

// ParseFilterIDs 解析 ID 类筛选参数（如 ?deptIds=1,2,3）。
// 非法项忽略而不是整体 400：AI 筛选/历史链接里的坏值不该让整个列表打不开。
func ParseFilterIDs(raw string, max int) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]int64, 0, max)
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		id, err := idgen.Parse(part)
		if err != nil || id <= 0 || slices.Contains(out, int64(id)) {
			continue
		}
		out = append(out, int64(id))
		if len(out) >= max {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
