package im

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// liveDB 连接真实库（未设置 NEXADMIN_LIVE_DB 时跳过）。库名默认 nexadmin_im_live，
// 与业务库分开：本用例会建表并删数据。
// 例：NEXADMIN_LIVE_DB=mysql NEXADMIN_LIVE_HOST=127.0.0.1 NEXADMIN_LIVE_PORT=3306 \
//
//	NEXADMIN_LIVE_USER=root NEXADMIN_LIVE_PASSWORD=... go test ./plugins/im/ -run TestLiveIM
func liveDB(t *testing.T) (*gorm.DB, bool) {
	t.Helper()
	kind := os.Getenv("NEXADMIN_LIVE_DB")
	if kind == "" {
		t.Skip("未设置 NEXADMIN_LIVE_DB，跳过真实库验证")
	}
	port, _ := strconv.Atoi(os.Getenv("NEXADMIN_LIVE_PORT"))
	host := os.Getenv("NEXADMIN_LIVE_HOST")
	if host == "" {
		host = "localhost"
	}
	name := os.Getenv("NEXADMIN_LIVE_DB_NAME")
	if name == "" {
		name = "nexadmin_im_live"
	}
	cfg := config.Database{Type: kind, Host: host, Port: port, User: os.Getenv("NEXADMIN_LIVE_USER"),
		Password: os.Getenv("NEXADMIN_LIVE_PASSWORD"), Name: name, NodeID: 11}
	ctx := context.Background()
	if err := database.EnsureDatabase(ctx, cfg); err != nil {
		t.Fatalf("ensure database: %v", err)
	}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&Conversation{}, &Message{}, &Attachment{}, &Setting{}, &QuickReply{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db, true
}

// TestLiveIMLifecycle 在真实库上验证：备注落库、级联删除（含附件存储对象）、
// 进行中会话拒删、保留期清理、欢迎语。
func TestLiveIMLifecycle(t *testing.T) {
	db, ok := liveDB(t)
	if !ok {
		return
	}
	if err := idgen.Init(11); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())

	// 每个用例用独立 db 名，但表是共享的：先把本文件造的数据清干净。
	cleanup := func() {
		db.Exec("DELETE FROM plg_im_message")
		db.Exec("DELETE FROM plg_im_attachment")
		db.Exec("DELETE FROM plg_im_conversation")
		db.Exec("DELETE FROM plg_im_setting")
	}
	cleanup()
	t.Cleanup(cleanup)

	s := NewService(db, nopLogger{})
	store := &stubStore{}
	s.SetStorage(store)

	openConv := func(tag string) *Conversation {
		c, err := s.Open(ctx, OpenInput{BizType: "live", BizNo: tag + uniq, VisitorToken: "visitor-" + uniq, VisitorName: "live"})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return c
	}

	// 进行中的会话不能删。
	open1 := openConv("a")
	if _, err := s.DeleteConversation(ctx, open1.ConvNo); statusOf(err) != 409 {
		t.Fatalf("delete open: %v", err)
	}

	// 备注落库。
	if _, err := s.SetNote(ctx, open1.ConvNo, "已电话回访"); err != nil {
		t.Fatalf("note: %v", err)
	}
	var noted Conversation
	if err := db.Where("conv_no = ?", open1.ConvNo).Take(&noted).Error; err != nil || noted.Note != "已电话回访" {
		t.Fatalf("note not persisted: %+v %v", noted, err)
	}

	// 附件 + 消息，再结束，然后级联删除。
	att, err := s.SaveAttachment(ctx, open1.ConvNo, SenderAgent, "a.png", "image/png", 10, "live/"+uniq+"/a.png")
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}
	if _, err := s.Send(ctx, open1.ConvNo, SendInput{SenderType: SenderAgent, SenderID: "1", SenderName: "agent", Content: "hi", Attachment: att.Token}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, _, err := s.Close(ctx, open1.ConvNo, "agent-1"); err != nil {
		t.Fatalf("close: %v", err)
	}
	res, err := s.DeleteConversation(ctx, open1.ConvNo)
	if err != nil {
		t.Fatalf("delete closed: %v", err)
	}
	if res.Deleted != 1 {
		t.Fatalf("delete result = %+v", res)
	}
	var n int64
	db.Model(&Conversation{}).Where("conv_no = ?", open1.ConvNo).Count(&n)
	if n != 0 {
		t.Fatalf("conversation rows left = %d", n)
	}
	db.Model(&Message{}).Where("conv_id = ?", int64(open1.ID)).Count(&n)
	if n != 0 {
		t.Fatalf("message rows left = %d", n)
	}
	db.Model(&Attachment{}).Where("conv_id = ?", int64(open1.ID)).Count(&n)
	if n != 0 {
		t.Fatalf("attachment rows left = %d", n)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "live/"+uniq+"/a.png" {
		t.Fatalf("storage keys deleted = %v", store.deleted)
	}

	// 保留期清理：旧会话被删，新会话保留。
	open2 := openConv("b")
	if _, _, err := s.Close(ctx, open2.ConvNo, "agent-1"); err != nil {
		t.Fatalf("close: %v", err)
	}
	old := time.Now().AddDate(0, 0, -40)
	if err := db.Model(&Conversation{}).Where("id = ?", int64(open2.ID)).Update("closed_at", old).Error; err != nil {
		t.Fatalf("age conversation: %v", err)
	}
	keep := openConv("c")
	if _, _, err := s.Close(ctx, keep.ConvNo, "agent-1"); err != nil {
		t.Fatalf("close: %v", err)
	}
	scanned, deleted, err := s.PurgeConversations(ctx, time.Now().AddDate(0, 0, -30), 50)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if scanned < 1 || deleted < 1 {
		t.Fatalf("purge scanned=%d deleted=%d", scanned, deleted)
	}
	db.Model(&Conversation{}).Where("conv_no = ?", open2.ConvNo).Count(&n)
	if n != 0 {
		t.Fatal("aged conversation should be purged")
	}
	db.Model(&Conversation{}).Where("conv_no = ?", keep.ConvNo).Count(&n)
	if n != 1 {
		t.Fatal("recent closed conversation must be kept")
	}

	// 欢迎语：设置后新会话的第一条消息就是欢迎语；访客消息里的敏感词被替换。
	if _, err := s.SaveSettings(ctx, Settings{WelcomeText: "你好，请描述你的问题", BannedWords: []string{"退款"}}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	welcome := openConv("d")
	if _, err := s.Send(ctx, welcome.ConvNo, SendInput{SenderType: SenderVisitor, SenderID: "v", Content: "我要退款"}); err != nil {
		t.Fatalf("visitor send: %v", err)
	}
	_, msgs, _, err := s.Messages(ctx, welcome.ConvNo, MessageQuery{Limit: 10})
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Content != "你好，请描述你的问题" || msgs[0].SenderType != SenderAgent {
		t.Fatalf("welcome message = %+v", msgs)
	}
	if msgs[1].Content != "我要**" {
		t.Fatalf("banned word not masked: %q", msgs[1].Content)
	}
}

// TestLiveIMFeatures 在真实库上验证：排队领取、撤回、合并、统计。
func TestLiveIMFeatures(t *testing.T) {
	db, ok := liveDB(t)
	if !ok {
		return
	}
	if err := idgen.Init(12); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	cleanup := func() {
		db.Exec("DELETE FROM plg_im_message")
		db.Exec("DELETE FROM plg_im_attachment")
		db.Exec("DELETE FROM plg_im_conversation")
		db.Exec("DELETE FROM plg_im_setting")
	}
	cleanup()
	t.Cleanup(cleanup)

	s := NewService(db, nopLogger{})
	openConv := func(tag string) *Conversation {
		c, err := s.Open(ctx, OpenInput{BizType: "live", BizNo: tag + uniq, VisitorToken: "visitor-" + uniq})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return c
	}

	// 自动分配：开启后排队会话被心跳领取，且第二次领取为空。
	if _, err := s.SaveSettings(ctx, Settings{AutoAssign: true}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	queued := openConv("q")
	no, err := s.AssignNext(ctx, "77", "agent-seven")
	if err != nil || no != queued.ConvNo {
		t.Fatalf("assignNext = %q, %v", no, err)
	}
	var assigned Conversation
	if err := db.Where("conv_no = ?", queued.ConvNo).Take(&assigned).Error; err != nil || assigned.AssigneeID != "77" {
		t.Fatalf("assignee = %+v (%v)", assigned, err)
	}
	if again, _ := s.AssignNext(ctx, "78", "agent-eight"); again != "" {
		t.Fatalf("unexpected second assign %q", again)
	}

	// 撤回：本人窗口内的消息可撤，重复撤回与访客消息都拒绝。
	c1 := openConv("r")
	m, err := s.Send(ctx, c1.ConvNo, SendInput{SenderType: SenderAgent, SenderID: "7", SenderName: "agent", Content: "oops"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	rec, err := s.Recall(ctx, c1.ConvNo, m.Seq, "7")
	if err != nil || rec.Content != "" || rec.Kind != KindSystem {
		t.Fatalf("recall = %+v, %v", rec, err)
	}
	// 撤回时间必须是「现在」，不能复用会话创建时间。
	if rec.RecalledAt == nil || time.Since(*rec.RecalledAt).Abs() > time.Minute {
		t.Fatalf("recalledAt should be now, got %v", rec.RecalledAt)
	}
	var stored Message
	if err := db.Where("id = ?", int64(m.ID)).Take(&stored).Error; err != nil || stored.Content != "" || stored.RecalledAt == nil {
		t.Fatalf("recalled row = %+v (%v)", stored, err)
	}
	if _, err := s.Recall(ctx, c1.ConvNo, m.Seq, "7"); err == nil {
		t.Fatal("second recall must fail")
	}
	v, err := s.Send(ctx, c1.ConvNo, SendInput{SenderType: SenderVisitor, Content: "hello"})
	if err != nil {
		t.Fatalf("visitor send: %v", err)
	}
	if _, err := s.Recall(ctx, c1.ConvNo, v.Seq, "7"); err == nil {
		t.Fatal("visitor message must not be recallable")
	}

	// 已结束会话被再次打开时重新开启（挂件「再发消息开新会话」依赖这条）。
	if _, _, err := s.Close(ctx, c1.ConvNo, "7"); err != nil {
		t.Fatalf("close: %v", err)
	}
	re, err := s.Open(ctx, OpenInput{BizType: "live", BizNo: "r" + uniq, VisitorToken: "visitor-" + uniq})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if re.ConvNo != c1.ConvNo || re.Status != StatusOpen {
		t.Fatalf("reopen = %+v", re)
	}
	if _, err := s.Send(ctx, re.ConvNo, SendInput{SenderType: SenderVisitor, Content: "again"}); err != nil {
		t.Fatalf("send after reopen: %v", err)
	}

	// 合并：源会话消息搬进目标，源会话结束并留合并提示，目标序号连续。
	c2, c3 := openConv("m2"), openConv("m3")
	if _, err := s.Send(ctx, c2.ConvNo, SendInput{SenderType: SenderVisitor, Content: "from c2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, c3.ConvNo, SendInput{SenderType: SenderVisitor, Content: "from c3"}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Merge(ctx, c2.ConvNo, c3.ConvNo, "7")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res.Moved < 1 {
		t.Fatalf("moved = %+v", res)
	}
	var src, tgt Conversation
	if err := db.Where("conv_no = ?", c2.ConvNo).Take(&src).Error; err != nil || src.Status != StatusClosed {
		t.Fatalf("source = %+v (%v)", src, err)
	}
	if err := db.Where("conv_no = ?", c3.ConvNo).Take(&tgt).Error; err != nil {
		t.Fatal(err)
	}
	var merged []Message
	if err := db.Where("conv_id = ?", int64(tgt.ID)).Order("seq ASC").Find(&merged).Error; err != nil {
		t.Fatal(err)
	}
	for i := range merged {
		if merged[i].Seq != int64(i+1) {
			t.Fatalf("target seq not contiguous at %d: %+v", i, merged[i])
		}
	}
	// 合并系统提示里要留操作坐席，便于追溯。
	lastMerged := merged[len(merged)-1]
	if lastMerged.Kind != KindSystem || !strings.Contains(lastMerged.Meta, `"by":"7"`) {
		t.Fatalf("merge note should record the agent: %+v", lastMerged)
	}
	_, srcMsgs, _, err := s.Messages(ctx, c2.ConvNo, MessageQuery{Limit: 10})
	if err != nil || len(srcMsgs) == 0 {
		t.Fatalf("source messages = %v, %v", srcMsgs, err)
	}
	last := srcMsgs[len(srcMsgs)-1]
	if last.Kind != KindSystem || !strings.Contains(last.Meta, "conv_merged_into") {
		t.Fatalf("source note = %+v", last)
	}

	// 统计：窗口内总量、按天趋势、首响样本。
	c4 := openConv("f")
	if _, err := s.Send(ctx, c4.ConvNo, SendInput{SenderType: SenderVisitor, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, c4.ConvNo, SendInput{SenderType: SenderAgent, SenderID: "7", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats(ctx, 7)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Total < 4 || len(st.Daily) != 7 || st.Truncated {
		t.Fatalf("stats = %+v", st)
	}
	if st.FirstResponseCount < 1 {
		t.Fatalf("first response samples = %d", st.FirstResponseCount)
	}
}

// TestLiveIMKeywordSearchesMessageBody 在真实库上验证关键字能命中消息正文：
// 搜索串只写进 a 的消息内容，会话号/访客名/摘要都不含它，命中 a 只可能来自正文。
func TestLiveIMKeywordSearchesMessageBody(t *testing.T) {
	db, ok := liveDB(t)
	if !ok {
		return
	}
	if err := idgen.Init(12); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("kw%d", time.Now().UnixNano())
	token := "ORD" + uniq

	mkConv := func(tag, name string) *Conversation {
		c := &Conversation{ConvNo: "IMT" + uniq + tag, BizType: "live", BizNo: uniq + tag,
			VisitorToken: uniq + tag, VisitorName: name, Status: StatusOpen, LastPreview: "摘要里没有编号"}
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create conv: %v", err)
		}
		return c
	}
	mkMsg := func(c *Conversation, seq int64, content string) {
		m := &Message{ConvID: c.ID, Seq: seq, SenderType: SenderVisitor, SenderName: "访客",
			Content: content, Kind: KindText}
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create msg: %v", err)
		}
	}
	a := mkConv("A", "搜索用例甲")
	b := mkConv("B", "搜索用例乙")
	mkMsg(a, 1, "帮我查 "+token+" 到哪了")
	mkMsg(b, 1, "帮我查别的")
	t.Cleanup(func() {
		db.Exec("DELETE FROM plg_im_message WHERE conv_id IN (?, ?)", int64(a.ID), int64(b.ID))
		db.Exec("DELETE FROM plg_im_conversation WHERE conv_no IN (?, ?)", a.ConvNo, b.ConvNo)
	})

	s := NewService(db, nopLogger{})
	search := func(kw string) []string {
		t.Helper()
		list, _, err := s.ListConversations(ctx, ConversationQuery{Keyword: kw, Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("search %q: %v", kw, err)
		}
		nos := make([]string, 0, len(list))
		for _, c := range list {
			nos = append(nos, c.ConvNo)
		}
		return nos
	}

	if got := search(token); len(got) != 1 || got[0] != a.ConvNo {
		t.Fatalf("body search = %v, want only %s", got, a.ConvNo)
	}
	// 摘要字段仍要能命中（没有因为加了子查询而把原条件挤掉）。
	if got := search("搜索用例乙"); len(got) != 1 || got[0] != b.ConvNo {
		t.Fatalf("name search = %v, want %s", got, b.ConvNo)
	}
	// 负控制：谁都不含的串必须空，否则上面两条是「全都返回」蒙对的。
	if got := search("NOPE" + uniq); len(got) != 0 {
		t.Fatalf("unmatched keyword = %v, want empty", got)
	}
}

// TestLiveIMExportMessages 在真实库上验证「完整对话」导出数据：会话号来自 join、
// 按会话号 + 会话内序号排序（插入时刻意反着插）、撤回消息带 RecalledAt、limit 生效。
func TestLiveIMExportMessages(t *testing.T) {
	db, ok := liveDB(t)
	if !ok {
		return
	}
	if err := idgen.Init(13); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("exp%d", time.Now().UnixNano())

	mkConv := func(tag string) *Conversation {
		c := &Conversation{ConvNo: "IME" + uniq + tag, BizType: "live", BizNo: uniq + tag,
			VisitorToken: uniq + tag, VisitorName: "导出用例", Status: StatusClosed}
		if err := db.Create(c).Error; err != nil {
			t.Fatalf("create conv: %v", err)
		}
		return c
	}
	mkMsg := func(c *Conversation, seq int64, senderType, name, content string, recalled bool) {
		m := &Message{ConvID: c.ID, Seq: seq, SenderType: senderType, SenderName: name,
			Content: content, Kind: KindText}
		if recalled {
			now := time.Now().UTC().Truncate(time.Second)
			m.RecalledAt = &now
			m.Kind = KindSystem
		}
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create msg: %v", err)
		}
	}
	c1 := mkConv("C")
	c2 := mkConv("D")
	// 反着插 seq：若实现按插入顺序（id）排，这两条会颠倒，用例就瞎了。
	mkMsg(c1, 2, SenderAgent, "客服一号", "稍等，我查一下", false)
	mkMsg(c1, 1, SenderVisitor, "访客一号", "订单还没到", true)
	mkMsg(c2, 1, SenderVisitor, "访客二号", "怎么退货", false)
	t.Cleanup(func() {
		db.Exec("DELETE FROM plg_im_message WHERE conv_id IN (?, ?)", int64(c1.ID), int64(c2.ID))
		db.Exec("DELETE FROM plg_im_conversation WHERE conv_no IN (?, ?)", c1.ConvNo, c2.ConvNo)
	})

	s := NewService(db, nopLogger{})
	rows, err := s.ExportMessages(ctx, []idgen.ID{c1.ID, c2.ID}, 100)
	if err != nil {
		t.Fatalf("export messages: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].ConvNo != c1.ConvNo || rows[0].Seq != 1 || rows[1].Seq != 2 || rows[2].ConvNo != c2.ConvNo {
		t.Fatalf("order = %+v", rows)
	}
	if rows[0].RecalledAt == nil {
		t.Fatal("recalled message lost RecalledAt")
	}
	if rows[1].SenderType != SenderAgent || rows[1].SenderName != "客服一号" || rows[1].Content != "稍等，我查一下" {
		t.Fatalf("agent row = %+v", rows[1])
	}
	// 上限：只给 1 行就必须只有 1 行，否则「截断提示」那条分支永远不会触发。
	one, err := s.ExportMessages(ctx, []idgen.ID{c1.ID, c2.ID}, 1)
	if err != nil {
		t.Fatalf("export limit: %v", err)
	}
	if len(one) != 1 {
		t.Fatalf("limited rows = %d, want 1", len(one))
	}
	// 空入参不发查询。
	none, err := s.ExportMessages(ctx, nil, 100)
	if err != nil || len(none) != 0 {
		t.Fatalf("empty ids = %v, %v", none, err)
	}
}

// TestLiveIMExportWritesTranscriptSheet 端到端验证「导出含完整对话」这条主张：
// 直接调导出 handler，把生成的 xlsx 用我们自己的 reader 读回来数 sheet 和正文行。
func TestLiveIMExportWritesTranscriptSheet(t *testing.T) {
	db, ok := liveDB(t)
	if !ok {
		return
	}
	if err := idgen.Init(14); err != nil {
		t.Fatal(err)
	}
	uniq := fmt.Sprintf("xlsx%d", time.Now().UnixNano())

	c := &Conversation{ConvNo: "IMX" + uniq, BizType: "live", BizNo: uniq,
		VisitorToken: uniq, VisitorName: "导出用例", Status: StatusClosed}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("create conv: %v", err)
	}
	for i, m := range []*Message{
		{Seq: 1, SenderType: SenderVisitor, SenderName: "访客甲", Content: "订单还没到 " + uniq, Kind: KindText},
		{Seq: 2, SenderType: SenderAgent, SenderName: "客服乙", Content: "已催发货", Kind: KindText},
	} {
		m.ConvID = c.ID
		m.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create msg: %v", err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM plg_im_message WHERE conv_id = ?", int64(c.ID))
		db.Exec("DELETE FROM plg_im_conversation WHERE conv_no = ?", c.ConvNo)
	})

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/plugins/im/conversations-export?keyword="+uniq, nil)
	(&handler{svc: NewService(db, nopLogger{})}).export(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	buf := rec.Body.Bytes()
	rd, err := excel.Open(bytes.NewReader(buf), int64(len(buf)), excel.ReadOptions{})
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	names := rd.SheetNames()
	if len(names) != 2 {
		t.Fatalf("sheets = %v, want 2", names)
	}
	rows, err := excel.ReadAll(bytes.NewReader(buf), int64(len(buf)), excel.ReadOptions{Sheet: names[1]})
	if err != nil {
		t.Fatalf("read transcript sheet: %v", err)
	}
	if len(rows) != 3 { // 表头 + 2 条消息
		t.Fatalf("transcript rows = %d: %v", len(rows), rows)
	}
	if rows[1][0] != c.ConvNo || rows[1][5] != "订单还没到 "+uniq || rows[2][5] != "已催发货" {
		t.Fatalf("transcript content wrong: %v", rows[1:])
	}
	if rows[1][1] != "1" || rows[2][1] != "2" {
		t.Fatalf("transcript seq = %v", rows[1:])
	}
	// 第一张 sheet 只剩会话清单，正文不该混进去。
	list, err := excel.ReadAll(bytes.NewReader(buf), int64(len(buf)), excel.ReadOptions{Sheet: names[0]})
	if err != nil {
		t.Fatalf("read list sheet: %v", err)
	}
	if len(list) != 2 || strings.Contains(strings.Join(list[1], "|"), "已催发货") {
		t.Fatalf("list sheet = %v", list)
	}
}
