package engine

import (
	"strconv"
	"strings"
)

// EvalCondition 计算条件表达式（形如 "amount>10000"、"level==high"）。
// 支持运算符：>= <= == != > <；左值是流程变量名，右值是数字或字符串字面量。
// 空表达式返回 true（默认流）；无法解析的表达式返回 false（宁可不通分支，也不误走分支）。
func EvalCondition(expr string, vars map[string]string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	for _, op := range []string{">=", "<=", "==", "!=", ">", "<"} {
		i := strings.Index(expr, op)
		if i < 0 {
			continue
		}
		left := strings.TrimSpace(expr[:i])
		right := strings.Trim(strings.TrimSpace(expr[i+len(op):]), "\"'")
		if left == "" || right == "" {
			return false
		}
		lv, ok := vars[left]
		if !ok {
			return false
		}
		return compare(op, lv, right)
	}
	return false
}

// compare 先按数字比较，数字不成立再按字符串比较（此时只支持 == / !=）。
func compare(op, a, b string) bool {
	af, aerr := strconv.ParseFloat(strings.TrimSpace(a), 64)
	bf, berr := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if aerr == nil && berr == nil {
		switch op {
		case ">":
			return af > bf
		case "<":
			return af < bf
		case ">=":
			return af >= bf
		case "<=":
			return af <= bf
		case "==":
			return af == bf
		case "!=":
			return af != bf
		}
		return false
	}
	switch op {
	case "==":
		return a == b
	case "!=":
		return a != b
	}
	return false
}
