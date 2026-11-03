package ticket

import "testing"

// feed 是跨租户共用主题，守卫不验数据范围；帧里带正文摘要等于把别人的工单内容推给范围外坐席。
// 这两条是那条泄漏的探测器：把 publishWS 改回发完整帧、或把 replyPreview 的分支删掉，这里必须红。
func TestFeedFrameCarriesNoContent(t *testing.T) {
	tk := &Ticket{TicketNo: "TK-1", Status: StatusPending}
	f := feedFrame(wsKindReplied, tk)
	if f.Preview != "" {
		t.Errorf("feed 帧带了正文摘要: %q", f.Preview)
	}
	if f.Type != wsKindReplied || f.TicketNo != "TK-1" {
		t.Errorf("feed 帧丢了列表刷新要用的字段: %+v", f)
	}
}

// 内部备注只对坐席可见，而单工单主题的订阅者里就有报告人（客户）：正文一律不进帧。
func TestReplyPreviewDropsInternalNote(t *testing.T) {
	if got := replyPreview(true, "客户其实想走退款，先别告诉他"); got != "" {
		t.Errorf("内部备注的正文进了 WS 帧: %q", got)
	}
	if got := replyPreview(false, "您好，已为您处理"); got == "" {
		t.Error("公开回复的摘要被一并抹掉了，实时气泡就没内容了")
	}
}
