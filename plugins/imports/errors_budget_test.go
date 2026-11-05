package imports

import (
	"encoding/json"
	"strings"
	"testing"
)

// 台账一列是 text：MySQL/MariaDB 撞过 64KB 不是截断而是整条写入失败，
// 后果是「已经导进库的行被报成失败」或「异步任务永远停在 running」。
// 所以明细必须在写入前压进预算，而不是指望调用方少犯错。
func TestFitErrorsJSONStaysWithinBudget(t *testing.T) {
	errs := make([]RowError, 0, 500)
	for i := 0; i < 500; i++ {
		data := map[string]string{}
		for col := 0; col < 12; col++ {
			data[strings.Repeat("c", col+3)] = strings.Repeat("值", 60)
		}
		errs = append(errs, RowError{Row: i + 1, Key: "name", Message: "重复", Data: data})
	}
	raw, dropped := fitErrorsJSON(errs)
	if len(raw) > errorsBudgetBytes {
		t.Fatalf("明细 JSON %d 字节，超出台账预算 %d", len(raw), errorsBudgetBytes)
	}
	if dropped >= len(errs) {
		t.Fatalf("全部条目被丢弃（dropped=%d），原因明细一条都没留下", dropped)
	}
	// 收敛顺序：先剥行内容保住原因，所以留下的条目至少还能看出错在哪行
	var back []RowError
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatalf("压完不是合法 JSON：%v", err)
	}
	if len(back) != len(errs)-dropped {
		t.Fatalf("条数与 dropped 对不上：存了 %d 条，丢了 %d 条，原本 %d 条", len(back), dropped, len(errs))
	}
	for _, e := range back {
		if e.Message == "" || e.Row == 0 {
			t.Fatalf("留下的明细缺行号或原因：%+v", e)
		}
	}
}

// 装得下的时候不许动任何东西：压预算是为了兜底，不是常态化的丢数据。
func TestFitErrorsJSONKeepsSmallLedgerIntact(t *testing.T) {
	errs := []RowError{{Row: 1, Key: "name", Message: "重复", Data: map[string]string{"name": "张三"}}}
	raw, dropped := fitErrorsJSON(errs)
	if dropped != 0 {
		t.Fatalf("小台账不该丢条目：%d", dropped)
	}
	if !strings.Contains(raw, `"data"`) || !strings.Contains(raw, `"张三"`) {
		t.Fatalf("原始行内容被无谓剥掉：%s", raw)
	}
}
