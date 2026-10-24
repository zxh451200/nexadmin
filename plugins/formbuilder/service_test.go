package formbuilder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

var (
	ctxT1 = tenant.With(context.Background(), 1)
	ctxT2 = tenant.With(context.Background(), 2)
	admin = Actor{ID: 1, Username: "admin", Super: true}
)

func user(id int64) Actor { return Actor{ID: idgen.ID(id), Username: fmt.Sprintf("u%d", id)} }

func newSvc() (*Service, *memStore, *fakeDir) {
	st, dir := newMemStore(), &fakeDir{roles: map[idgen.ID][]idgen.ID{}, dicts: map[string][]Option{}}
	s := NewService(st, dir, nil)
	var n atomic.Int64
	n.Store(500000)
	s.newID = func() (idgen.ID, error) { return idgen.ID(n.Add(1)), nil }
	return s, st, dir
}

func status(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

func simpleDef(name string, fields ...Field) Definition {
	return Definition{Name: name, Fields: fields}
}

// publish 新建表单并发布，返回表单 ID。
func publish(t *testing.T, s *Service, ctx context.Context, d Definition) idgen.ID {
	t.Helper()
	f, err := s.CreateForm(ctx, admin, d.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(ctx, f.ID, SaveInput{Definition: d}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, admin, f.ID); err != nil {
		t.Fatal(err)
	}
	return f.ID
}

var fa = Field{Key: "a", Type: TypeText, Title: "A", Required: true}

func TestVersionImmutable(t *testing.T) {
	s, _, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("f", fa))

	// 改草稿（加字段 b），已发布的 v1 不受影响
	d2 := simpleDef("f", fa, Field{Key: "b", Type: TypeNumber, Title: "B"})
	dto, err := s.SaveDraft(ctxT1, id, SaveInput{Definition: d2})
	if err != nil {
		t.Fatal(err)
	}
	if !dto.HasUnpublished || dto.PublishedVersion != 1 {
		t.Fatalf("草稿有改动应标记未发布: %+v", dto)
	}
	v1, err := s.GetVersionDefinition(ctxT1, id, 1)
	if err != nil || len(v1.Fields) != 1 {
		t.Fatalf("v1 应保持 1 个字段: %v %v", v1, err)
	}
	// 再发布 -> v2；v1 仍然不变
	dto, err = s.Publish(ctxT1, admin, id)
	if err != nil || dto.PublishedVersion != 2 || dto.HasUnpublished {
		t.Fatalf("发布 v2 失败: %+v %v", dto, err)
	}
	if v1, _ = s.GetVersionDefinition(ctxT1, id, 1); len(v1.Fields) != 1 {
		t.Fatal("发布 v2 后 v1 被改动")
	}
	if v2, _ := s.GetVersionDefinition(ctxT1, id, 2); len(v2.Fields) != 2 {
		t.Fatal("v2 应有 2 个字段")
	}
	vs, _ := s.ListVersions(ctxT1, id)
	if len(vs) != 2 {
		t.Fatalf("应有 2 个版本: %v", vs)
	}
}

// 版本表没有更新入口：Store 接口里不存在 UpdateVersion / DeleteVersion；
// 并发发布同一版本时只有一个成功（乐观并发）。
func TestConcurrentPublishOnlyOneWins(t *testing.T) {
	s, _, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("f", fa))
	var wg sync.WaitGroup
	var ok, conflicts atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Publish(ctxT1, admin, id); err == nil {
				ok.Add(1)
			} else if status(err) == 409 {
				conflicts.Add(1)
			}
		}()
	}
	wg.Wait()
	vs, _ := s.ListVersions(ctxT1, id)
	// 每个成功的发布产生一个递增、不重复的版本号
	if int(ok.Load())+1 != len(vs) {
		t.Fatalf("成功 %d 次，版本数 %d", ok.Load(), len(vs))
	}
	for i, v := range vs {
		if v.Version != i+1 {
			t.Fatalf("版本号应连续: %v", vs)
		}
	}
}

func TestSubmitUsesPublishedVersionNotDraft(t *testing.T) {
	s, _, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("f", fa))
	// 草稿里去掉 a、新增必填 b，但没有发布
	draft := simpleDef("f", Field{Key: "b", Type: TypeText, Title: "B", Required: true})
	if _, err := s.SaveDraft(ctxT1, id, SaveInput{Definition: draft}); err != nil {
		t.Fatal(err)
	}
	u := user(10)
	// 按草稿提交 b：必须失败（已发布版本要求 a）
	if _, err := s.Submit(ctxT1, u, id, SubmitInput{Data: map[string]any{"b": "x"}}); status(err) != 400 {
		t.Fatalf("按草稿提交应被拒绝: %v", err)
	}
	// 按已发布版本提交 a：成功，b 被丢弃，记录版本 1
	res, err := s.Submit(ctxT1, u, id, SubmitInput{Data: map[string]any{"a": "hello", "b": "draft-only"}})
	if err != nil || res.Version != 1 {
		t.Fatalf("提交失败: %+v %v", res, err)
	}
	det, err := s.GetSubmission(ctxT1, id, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if det.Version != 1 || det.Data["a"] != "hello" || det.Data["b"] != nil {
		t.Fatalf("提交内容不对: %+v", det)
	}
	// 填写页拿到的是已发布版本，不含草稿字段
	fd, err := s.FillGet(ctxT1, u, id)
	if err != nil || len(fd.Definition.Fields) != 1 || fd.Definition.Fields[0].Key != "a" {
		t.Fatalf("填写页应渲染已发布版本: %+v %v", fd, err)
	}
	// 发布 v2 后，新提交记录 v2；历史提交仍是 v1 且详情按 v1 的定义展示
	if _, err := s.Publish(ctxT1, admin, id); err != nil {
		t.Fatal(err)
	}
	res2, err := s.Submit(ctxT1, user(11), id, SubmitInput{Data: map[string]any{"b": "now"}})
	if err != nil || res2.Version != 2 {
		t.Fatalf("v2 提交: %+v %v", res2, err)
	}
	old, _ := s.GetSubmission(ctxT1, id, res.ID)
	if old.Version != 1 || old.Definition.Fields[0].Key != "a" {
		t.Fatalf("改版后历史提交应按 v1 定义展示: %+v", old)
	}
}

func TestNeverPublishedAndOffline(t *testing.T) {
	s, _, _ := newSvc()
	f, _ := s.CreateForm(ctxT1, admin, "draft-only", "")
	if _, err := s.Submit(ctxT1, user(2), f.ID, SubmitInput{Data: map[string]any{}}); status(err) != 404 {
		t.Fatalf("未发布的表单不能提交: %v", err)
	}
	if _, err := s.FillGet(ctxT1, user(2), f.ID); status(err) != 404 {
		t.Fatalf("未发布的表单不能打开填写页: %v", err)
	}
	if _, err := s.Publish(ctxT1, admin, f.ID); status(err) != 400 {
		t.Fatalf("没有字段的表单不能发布: %v", err)
	}
	id := publish(t, s, ctxT1, simpleDef("f", fa))
	if _, err := s.SetOnline(ctxT1, id, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctxT1, user(2), id, SubmitInput{Data: map[string]any{"a": "x"}}); status(err) != 404 {
		t.Fatalf("下线后不能提交: %v", err)
	}
	if _, err := s.SetOnline(ctxT1, id, false); status(err) != 409 {
		t.Fatalf("重复下线应冲突: %v", err)
	}
	if _, err := s.SetOnline(ctxT1, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctxT1, user(2), id, SubmitInput{Data: map[string]any{"a": "x"}}); err != nil {
		t.Fatalf("重新上线后可提交: %v", err)
	}
}

func TestSubmissionLimitAtomic(t *testing.T) {
	s, st, _ := newSvc()
	d := simpleDef("f", fa)
	d.Settings = Settings{AllowRepeat: true, MaxSubmissions: 3}
	id := publish(t, s, ctxT1, d)

	var wg sync.WaitGroup
	var ok, full atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Submit(ctxT1, user(int64(100+i)), id, SubmitInput{Data: map[string]any{"a": "x"}})
			switch {
			case err == nil:
				ok.Add(1)
			case status(err) == 409:
				full.Add(1)
			default:
				t.Errorf("意外错误: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if ok.Load() != 3 || full.Load() != 17 {
		t.Fatalf("上限 3：成功 %d，被拒 %d", ok.Load(), full.Load())
	}
	f, _ := st.GetForm(ctxT1, id)
	if f.SubmissionCount != 3 {
		t.Fatalf("计数应为 3: %d", f.SubmissionCount)
	}
	// 删除一条后归还名额
	list, _ := s.ListSubmissions(ctxT1, SubQuery{FormID: id, Page: 1, PageSize: 10})
	if err := s.DeleteSubmission(ctxT1, id, list.List[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctxT1, user(999), id, SubmitInput{Data: map[string]any{"a": "x"}}); err != nil {
		t.Fatalf("删除后应有名额: %v", err)
	}
	// 次数上限 + 列表里标记为 full
	if _, err := s.Submit(ctxT1, user(998), id, SubmitInput{Data: map[string]any{"a": "x"}}); status(err) != 409 {
		t.Fatalf("应再次满额: %v", err)
	}
	items, _ := s.FillList(ctxT1, user(997))
	if len(items) != 1 || items[0].Closed != "full" {
		t.Fatalf("列表应标记 full: %+v", items)
	}
}

func TestNoRepeatAndIdempotency(t *testing.T) {
	s, st, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("once", fa)) // 默认 AllowRepeat=false
	in := SubmitInput{Data: map[string]any{"a": "x"}}
	u := user(10)
	if _, err := s.Submit(ctxT1, u, id, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctxT1, u, id, in); status(err) != 409 {
		t.Fatalf("不允许重复提交: %v", err)
	}
	if _, err := s.Submit(ctxT1, user(11), id, in); err != nil {
		t.Fatalf("其他用户不受影响: %v", err)
	}
	f, _ := st.GetForm(ctxT1, id)
	if f.SubmissionCount != 2 {
		t.Fatalf("重复提交不应占用名额: %d", f.SubmissionCount)
	}
	if n, _ := s.PendingCount(ctxT1, u); n != 0 {
		t.Fatalf("已提交的不算待填: %d", n)
	}
	// 带幂等 key 的重试：返回原结果，不新增
	id2 := publish(t, s, ctxT1, simpleDef("once2", fa))
	in2 := SubmitInput{Data: map[string]any{"a": "x"}, IdemKey: "key-abcdefgh-1"}
	r1, err := s.Submit(ctxT1, u, id2, in2)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Submit(ctxT1, u, id2, in2)
	if err != nil || !r2.Replayed || r2.ID != r1.ID {
		t.Fatalf("同 key 重放应返回原记录: %+v %v", r2, err)
	}
	// 并发：同一用户同时提交 10 次，只有 1 条落库
	id3 := publish(t, s, ctxT1, simpleDef("once3", fa))
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Submit(ctxT1, u, id3, in); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("并发重复提交应只成功 1 次，实际 %d", ok.Load())
	}
	if f, _ := st.GetForm(ctxT1, id3); f.SubmissionCount != 1 {
		t.Fatalf("计数应为 1: %d", f.SubmissionCount)
	}
}

func TestIdempotencyWhenRepeatAllowed(t *testing.T) {
	s, st, _ := newSvc()
	d := simpleDef("rep", fa)
	d.Settings.AllowRepeat = true
	id := publish(t, s, ctxT1, d)
	u := user(5)
	in := SubmitInput{Data: map[string]any{"a": "x"}, IdemKey: "retry-key-0001"}
	var wg sync.WaitGroup
	results := make([]*SubmitResult, 6)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := s.Submit(ctxT1, u, id, in)
			if err != nil {
				t.Errorf("重放不应报错: %v", err)
			}
			results[i] = r
		}(i)
	}
	wg.Wait()
	for _, r := range results[1:] {
		if r == nil || results[0] == nil || r.ID != results[0].ID {
			t.Fatalf("同一幂等 key 应得到同一条记录: %+v", results)
		}
	}
	if f, _ := st.GetForm(ctxT1, id); f.SubmissionCount != 1 {
		t.Fatalf("计数应为 1: %d", f.SubmissionCount)
	}
	// 不带 key（或换 key）= 新提交
	if _, err := s.Submit(ctxT1, u, id, SubmitInput{Data: in.Data}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctxT1, u, id, SubmitInput{Data: in.Data, IdemKey: "another-key-0002"}); err != nil {
		t.Fatal(err)
	}
	// 其他用户使用同一个 key 不会命中别人的记录
	if r, err := s.Submit(ctxT1, user(6), id, in); err != nil || r.Replayed {
		t.Fatalf("幂等 key 按用户隔离: %+v %v", r, err)
	}
	if _, err := s.Submit(ctxT1, u, id, SubmitInput{Data: in.Data, IdemKey: "bad key!"}); status(err) != 400 {
		t.Fatalf("非法幂等 key: %v", err)
	}
}

func TestDeadline(t *testing.T) {
	s, _, _ := newSvc()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	d := simpleDef("dl", fa)
	dl := now.Add(time.Hour)
	d.Settings.Deadline = &dl
	id := publish(t, s, ctxT1, d)
	if _, err := s.Submit(ctxT1, user(1), id, SubmitInput{Data: map[string]any{"a": "x"}}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := s.Submit(ctxT1, user(2), id, SubmitInput{Data: map[string]any{"a": "x"}}); status(err) != 409 {
		t.Fatalf("过截止时间应拒绝: %v", err)
	}
	items, _ := s.FillList(ctxT1, user(3))
	if len(items) != 1 || items[0].Closed != "expired" {
		t.Fatalf("应标记 expired: %+v", items)
	}
}

func TestPayloadSizeLimit(t *testing.T) {
	s, _, _ := newSvc()
	var fs []Field
	for i := 0; i < 8; i++ {
		fs = append(fs, Field{Key: fmt.Sprintf("t%d", i), Type: TypeTextarea, Title: "T", MaxLen: ptr(MaxTextRunes)})
	}
	id := publish(t, s, ctxT1, simpleDef("big", fs...))
	data := map[string]any{}
	for i := 0; i < 8; i++ {
		data[fmt.Sprintf("t%d", i)] = strings.Repeat("字", MaxTextRunes) // 每个 30000 字节
	}
	if _, err := s.Submit(ctxT1, user(1), id, SubmitInput{Data: data}); status(err) != 400 || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("超过 MaxDataBytes 应拒绝: %v", err)
	}
}
