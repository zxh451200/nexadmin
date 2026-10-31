package im

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
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
