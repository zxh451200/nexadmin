package ticket

import "testing"

// 事件目录必须包含可被 webhook 订阅的全部 ticket.* 事件。
func TestCatalogIncludesWebhookEvents(t *testing.T) {
	names := make(map[string]bool)
	for _, info := range CatalogInfos() {
		names[info.Name] = true
	}
	for _, want := range []string{EventCreated, EventReplied, EventResolved, EventClosed, EventRated, EventOverdue} {
		if !names[want] {
			t.Errorf("catalog missing %s", want)
		}
	}
}
