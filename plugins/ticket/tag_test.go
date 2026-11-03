package ticket

import (
	"strconv"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeTags(t *testing.T) {
	got, err := normalizeTags([]string{" vip ", "refund", "vip", "", "紧急"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "vip" || got[1] != "refund" || got[2] != "紧急" {
		t.Fatalf("unexpected tags: %+v", got)
	}
	if _, err := normalizeTags([]string{"a,b"}); err == nil {
		t.Fatal("comma in tag accepted")
	}
	long := make([]rune, 21)
	for i := range long {
		long[i] = '标'
	}
	if _, err := normalizeTags([]string{string(long)}); err == nil {
		t.Fatal("overlong tag accepted")
	}
	tooMany := make([]string, maxTags+1)
	for i := range tooMany {
		tooMany[i] = string(rune('a' + i))
	}
	if _, err := normalizeTags(tooMany); err == nil {
		t.Fatal("too many tags accepted")
	}
}

func TestCreateCarriesTagsIntoRuleMatching(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO `plg_ticket`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_ticket_reply`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("INSERT INTO `plg_ticket_activity`").WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectCommit()

	tk, err := s.Create(testCtx(), CreateInput{
		Title: "order broken", Content: "x", ReporterType: ReplierReporter,
		Tags: []string{"vip", " vip ", "", "refund"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Tags != ",vip,refund," {
		t.Errorf("存储串 = %q, want ,vip,refund,", tk.Tags)
	}
	if len(tk.TagList) != 2 || tk.TagList[0] != "vip" {
		t.Errorf("TagList = %+v", tk.TagList)
	}
	// 规则读的就是这份规范串：建单带的标签必须能当场命中 match_tags。
	// 走 AfterFind 装载（生产路径就是这么把 ,vip, 拆成数组的），手工构造会跳过标签条件。
	hit := Rule{Enabled: true, MatchTagsRaw: ",vip,"}
	miss := Rule{Enabled: true, MatchTagsRaw: ",vip,notthere,"}
	for _, r := range []*Rule{&hit, &miss} {
		if err := r.AfterFind(nil); err != nil {
			t.Fatal(err)
		}
	}
	if !ruleMatches(hit, *tk) {
		t.Error("建单标签应能被 match_tags 命中")
	}
	if ruleMatches(miss, *tk) {
		t.Error("多标签是 AND，缺一个不该命中")
	}
}

// 超上限要在下发任何 SQL 之前就拒：这里一条期望都不登记，走到 INSERT 就会红。
func TestCreateRejectsTooManyTagsBeforeTouchingDB(t *testing.T) {
	db, m := mockDB(t)
	s := NewService(db, nopLogger{})
	tags := make([]string, 0, maxTags+1)
	for i := 0; i <= maxTags; i++ {
		tags = append(tags, "t"+strconv.Itoa(i))
	}
	if _, err := s.Create(testCtx(), CreateInput{
		Title: "t", Content: "c", ReporterType: ReplierReporter, Tags: tags,
	}); err == nil {
		t.Fatal("超过标签上限应被拒绝")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCanonicalAndSplitTags(t *testing.T) {
	if canonicalTags(nil) != "" {
		t.Fatal("empty tags should store empty string")
	}
	if got := canonicalTags([]string{"vip", "refund"}); got != ",vip,refund," {
		t.Fatalf("canonical tags = %q", got)
	}
	got := splitTags(",vip,refund,")
	if len(got) != 2 || got[0] != "vip" || got[1] != "refund" {
		t.Fatalf("split tags = %+v", got)
	}
}
