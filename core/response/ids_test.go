package response_test

import (
	"strconv"
	"strings"
	"testing"

	"nexadmin/core/response"
)

// 批量入参的三条底线：非法就整体拒；一条都没有也拒；条数超上限也拒。
// 第二条最容易出事——空列表如果放行，删不掉任何一行还是退化成「全表都删」，取决于调用方怎么写 IN 子句。
func TestParseIDs(t *testing.T) {
	ok := map[string]int{
		"1":       1,
		"1,2":     2,
		" 1 , 2 ": 2,
		"1,,2":    2, // 多余分隔符容忍，尾部空项不算非法
	}
	for raw, want := range ok {
		ids, err := response.ParseIDs(raw)
		if err != nil {
			t.Errorf("ParseIDs(%q) err = %v, want ok", raw, err)
			continue
		}
		if len(ids) != want {
			t.Errorf("ParseIDs(%q) = %v, want %d ids", raw, ids, want)
		}
	}

	for _, raw := range []string{"", "   ", "abc", "1,abc", "0", "-3", "1,0"} {
		ids, err := response.ParseIDs(raw)
		if err == nil {
			t.Errorf("ParseIDs(%q) = %v, want error", raw, ids)
		}
	}
}

// 条数上限要卡在边界上：恰好 MaxBatchIDs 通过、多一条就拒。
// 只测「很多条报错」是假闸——把上限写成 1 也能过，所以正控制（正好放行）必须一起断言。
func TestParseIDsBoundary(t *testing.T) {
	atLimit := strings.Join(nums(response.MaxBatchIDs), ",")
	ids, err := response.ParseIDs(atLimit)
	if err != nil {
		t.Fatalf("MaxBatchIDs 条应当放行，却报错：%v", err)
	}
	if len(ids) != response.MaxBatchIDs {
		t.Fatalf("len = %d, want %d", len(ids), response.MaxBatchIDs)
	}
	if _, err := response.ParseIDs(strings.Join(nums(response.MaxBatchIDs+1), ",")); err == nil {
		t.Fatal("超过 MaxBatchIDs 应当报错，绝不把几千个占位符交给一条 IN (...)")
	}
}

func nums(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return out
}
