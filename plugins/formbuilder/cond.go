package formbuilder

import (
	"fmt"
	"math"
	"strings"
)

// 条件运算符。
const (
	OpEq       = "eq"
	OpNe       = "ne"
	OpIn       = "in"
	OpContains = "contains"
	OpEmpty    = "empty"
	OpNotEmpty = "notEmpty"
	OpGt       = "gt"
	OpGte      = "gte"
	OpLt       = "lt"
	OpLte      = "lte"
)

// validateCond 校验条件的结构：深度、节点数、运算符，以及引用的字段必须在 earlier 里（只能引用排在前面的有值字段，
// 因此不可能有环，求值也只需要一趟）。
func validateCond(c *Cond, earlier map[string]struct{}) error {
	nodes := 0
	return validateCondNode(c, earlier, 1, &nodes)
}

func validateCondNode(c *Cond, earlier map[string]struct{}, depth int, nodes *int) error {
	*nodes++
	if depth > MaxCondDepth || *nodes > MaxCondNodes {
		return fmt.Errorf("条件过于复杂")
	}
	kinds := 0
	if len(c.All) > 0 {
		kinds++
	}
	if len(c.Any) > 0 {
		kinds++
	}
	if c.Field != "" || c.Op != "" {
		kinds++
	}
	if kinds != 1 {
		return fmt.Errorf("条件必须是 all、any 或 field+op 三种形态之一")
	}
	for _, list := range [][]Cond{c.All, c.Any} {
		for i := range list {
			if err := validateCondNode(&list[i], earlier, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	if len(c.All) > 0 || len(c.Any) > 0 {
		return nil
	}
	if _, ok := earlier[c.Field]; !ok {
		return fmt.Errorf("条件引用的字段 %q 不存在，或排在当前字段之后", c.Field)
	}
	switch c.Op {
	case OpEmpty, OpNotEmpty:
		return nil
	case OpEq, OpNe, OpContains:
		if !isScalar(c.Value) {
			return fmt.Errorf("条件 %s 的值必须是字符串、数字或布尔", c.Op)
		}
	case OpGt, OpGte, OpLt, OpLte:
		switch c.Value.(type) {
		case float64, string:
		default:
			return fmt.Errorf("条件 %s 的值必须是数字或字符串", c.Op)
		}
	case OpIn:
		list, ok := c.Value.([]any)
		if !ok || len(list) == 0 || len(list) > MaxCondList {
			return fmt.Errorf("条件 in 的值必须是 1-%d 个元素的数组", MaxCondList)
		}
		for _, v := range list {
			if !isScalar(v) {
				return fmt.Errorf("条件 in 的元素必须是字符串、数字或布尔")
			}
		}
	default:
		return fmt.Errorf("未知的条件运算符 %q", c.Op)
	}
	return nil
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, float64, bool:
		return true
	}
	return false
}

// evalCond 在已清洗的值 vals 上求条件。vals 只包含可见字段的值，所以引用到隐藏字段等价于该字段为空。
// 任何类型不匹配都得到 false，不会 panic。
func evalCond(c *Cond, vals map[string]any) bool {
	switch {
	case len(c.All) > 0:
		for i := range c.All {
			if !evalCond(&c.All[i], vals) {
				return false
			}
		}
		return true
	case len(c.Any) > 0:
		for i := range c.Any {
			if evalCond(&c.Any[i], vals) {
				return true
			}
		}
		return false
	}
	actual, present := vals[c.Field]
	empty := !present || isEmptyValue(actual)
	switch c.Op {
	case OpEmpty:
		return empty
	case OpNotEmpty:
		return !empty
	case OpEq:
		return !empty && scalarEq(actual, c.Value)
	case OpNe:
		return empty || !scalarEq(actual, c.Value)
	case OpContains:
		return !empty && containsValue(actual, c.Value)
	case OpIn:
		list, _ := c.Value.([]any)
		if empty {
			return false
		}
		if arr, ok := actual.([]any); ok {
			for _, a := range arr {
				if inList(a, list) {
					return true
				}
			}
			return false
		}
		return inList(actual, list)
	case OpGt, OpGte, OpLt, OpLte:
		if empty {
			return false
		}
		return compare(c.Op, actual, c.Value)
	}
	return false
}

func inList(v any, list []any) bool {
	for _, x := range list {
		if scalarEq(v, x) {
			return true
		}
	}
	return false
}

func scalarEq(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	}
	return false
}

func containsValue(actual, want any) bool {
	switch x := actual.(type) {
	case []any:
		for _, e := range x {
			if scalarEq(e, want) {
				return true
			}
		}
	case string:
		if w, ok := want.(string); ok {
			return w != "" && strings.Contains(x, w)
		}
	}
	return false
}

func compare(op string, a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		if !ok || math.IsNaN(x) || math.IsNaN(y) {
			return false
		}
		return cmpOrdered(op, x < y, x == y)
	case string:
		y, ok := b.(string)
		if !ok {
			return false
		}
		return cmpOrdered(op, x < y, x == y)
	}
	return false
}

func cmpOrdered(op string, lt, eq bool) bool {
	switch op {
	case OpGt:
		return !lt && !eq
	case OpGte:
		return !lt
	case OpLt:
		return lt
	case OpLte:
		return lt || eq
	}
	return false
}

// isEmptyValue：nil、空串、空数组视为空；false 与 0 是有效值。
func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	}
	return false
}
