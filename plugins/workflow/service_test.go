package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nexadmin/flowlite/runtime"
	"github.com/nexadmin/flowlite/store/memory"
	"nexadmin/core/notify"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type nopLogger struct{}

func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

func TestMain(m *testing.M) {
	if err := idgen.Init(1); err != nil {
		panic(err)
	}
	gin.SetMode(gin.TestMode)
	m.Run()
}

// memService 用内存存储构造适配层，验证业务语义而不依赖数据库。
func memService(t *testing.T) *Service {
	t.Helper()
	b := memory.New()
	b.SetIDGenerator(func(string) string {
		id, err := idgen.Next()
		if err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(int64(id), 10)
	})
	rt := runtime.New(b.Defs, b.Insts, b.Tasks, b.Actions)
	return newWithRuntime(rt, nopLogger{})
}

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
  xmlns:flowlite="urn:flowlite:engine:1.0" id="Definitions_1" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="leave" name="请假审批" isExecutable="true">
    <bpmn:startEvent id="start" name="开始" />
    <bpmn:userTask id="manager" name="主管审批" flowlite:approvers="alice" />
    <bpmn:userTask id="hr" name="HR 会签" flowlite:approvers="bob,carol" flowlite:mode="all" />
    <bpmn:endEvent id="end" name="结束" />
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="manager" />
    <bpmn:sequenceFlow id="f2" sourceRef="manager" targetRef="hr" />
    <bpmn:sequenceFlow id="f3" sourceRef="hr" targetRef="end" />
  </bpmn:process>
</bpmn:definitions>`

func ctxOf() context.Context { return context.Background() }

func savePublished(t *testing.T, s *Service) *DefView {
	t.Helper()
	v, err := s.DefSave(ctxOf(), 0, DefForm{Key: "leave", Name: "请假审批", Status: DefPublished, XML: sampleXML})
	if err != nil {
		t.Fatalf("保存定义失败: %v", err)
	}
	return v
}

func TestTemplatesValid(t *testing.T) {
	for _, tpl := range Templates() {
		if errs := Validate(tpl.Definition); len(errs) != 0 {
			t.Fatalf("模板 %s 非法: %v", tpl.Key, errs)
		}
	}
}

func TestDefSaveFromXML(t *testing.T) {
	s := memService(t)
	v := savePublished(t, s)
	if v.Version != 1 || v.Key != "leave" {
		t.Fatalf("定义 = %+v", v)
	}
	if len(v.Definition.Nodes) != 4 {
		t.Fatalf("节点数 = %d", len(v.Definition.Nodes))
	}
	if v.Definition.Nodes[1].Approvers[0] != "alice" {
		t.Fatalf("审批人未解析: %+v", v.Definition.Nodes[1])
	}

	// 建模器保存的 XML 必须原样保留，导出时不再重新生成（避免丢布局）。
	xml, err := s.DefXML(ctxOf(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, `flowlite:approvers="alice"`) {
		t.Fatalf("导出 XML 丢失扩展属性:\n%s", xml)
	}

	// 更新：版本 +1，ID 不变。
	v2, err := s.DefSave(ctxOf(), v.ID, DefForm{Key: "leave", Name: "请假审批 v2", Status: DefPublished, XML: sampleXML})
	if err != nil {
		t.Fatal(err)
	}
	if v2.ID != v.ID || v2.Version != 2 {
		t.Fatalf("更新后 = %+v", v2)
	}
	list, total, err := s.DefList(ctxOf(), DefQuery{Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("定义列表 = %d %v", total, err)
	}
}

func TestDefSaveRejectsInvalidXML(t *testing.T) {
	s := memService(t)
	_, err := s.DefSave(ctxOf(), 0, DefForm{Key: "bad", Name: "坏流程", XML: `<bpmn:definitions><bpmn:process id="bad"></bpmn:process></bpmn:definitions>`})
	e, ok := err.(*response.Error)
	if !ok || e.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

func TestInstanceLifecycle(t *testing.T) {
	s := memService(t)
	def := savePublished(t, s)
	ctx := ctxOf()

	inst, err := s.InstStart(ctx, StartInput{DefID: idText(def.ID), Title: "张三请假", BizType: "leave", BusinessKey: "L-1"}, "alice")
	if err != nil {
		t.Fatalf("发起失败: %v", err)
	}
	if inst.Status != StatusRunning || len(inst.Current) != 1 || inst.Current[0] != "manager" {
		t.Fatalf("发起后 = %s %v", inst.Status, inst.Current)
	}
	if len(inst.Pending) != 1 || inst.Pending[0].Assignee != "alice" {
		t.Fatalf("待办 = %+v", inst.Pending)
	}
	if inst.Definition == nil || len(inst.Definition.Nodes) != 4 {
		t.Fatal("实例视图必须带发起时的定义快照")
	}

	// 幂等：同业务键再次发起返回同一实例。
	again, err := s.InstStart(ctx, StartInput{DefID: idText(def.ID), BizType: "leave", BusinessKey: "L-1"}, "alice")
	if err != nil || again.ID != inst.ID {
		t.Fatalf("幂等发起 = %v %v", again, err)
	}

	todo, total, err := s.InstList(ctx, InstQuery{Todo: "1", Page: 1, PageSize: 20}, "alice")
	if err != nil || total != 1 || todo[0].ID != inst.ID {
		t.Fatalf("我的待办 = %d %v", total, err)
	}

	if _, err := s.InstApprove(ctx, inst.ID, "bob", ""); err == nil {
		t.Fatal("非当前审批人不应该能审批")
	} else if e, ok := err.(*response.Error); !ok || e.Status != http.StatusForbidden {
		t.Fatalf("非审批人错误 = %v", err)
	}

	if _, err := s.InstApprove(ctx, inst.ID, "alice", "同意"); err != nil {
		t.Fatalf("主管审批失败: %v", err)
	}
	todo, total, _ = s.InstList(ctx, InstQuery{Todo: "1", Page: 1, PageSize: 20}, "bob")
	if total != 1 || todo[0].Current[0] != "hr" {
		t.Fatalf("会签待办 = %d %+v", total, todo)
	}

	if _, err := s.InstApprove(ctx, inst.ID, "bob", ""); err != nil {
		t.Fatal(err)
	}
	out, err := s.InstApprove(ctx, inst.ID, "carol", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusApproved {
		t.Fatalf("状态 = %s", out.Status)
	}

	// 已结束：待办清空，我已办与我发起可查。
	if _, total, _ := s.InstList(ctx, InstQuery{Todo: "1", Page: 1, PageSize: 20}, "carol"); total != 0 {
		t.Fatalf("结束后仍有待办 %d", total)
	}
	done, total, err := s.InstList(ctx, InstQuery{Done: "1", Page: 1, PageSize: 20}, "bob")
	if err != nil || total != 1 || done[0].ID != inst.ID {
		t.Fatalf("我已办 = %d %v", total, err)
	}
	mine, total, err := s.InstList(ctx, InstQuery{Mine: "started", Page: 1, PageSize: 20}, "alice")
	if err != nil || total != 1 || mine[0].ID != inst.ID {
		t.Fatalf("我发起 = %d %v", total, err)
	}

	// 已结束再审批返回 409 而不是 500。
	if _, err := s.InstApprove(ctx, inst.ID, "carol", ""); err == nil {
		t.Fatal("已结束实例不应能审批")
	} else if e, ok := err.(*response.Error); !ok || e.Status != http.StatusConflict {
		t.Fatalf("已结束错误 = %v", err)
	}
}

func TestInstanceCountersignRollbackWithdraw(t *testing.T) {
	s := memService(t)
	def := savePublished(t, s)
	ctx := ctxOf()
	inst, err := s.InstStart(ctx, StartInput{DefID: idText(def.ID), Title: "操作"}, "alice")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.InstCountersign(ctx, inst.ID, "alice", "dave", "请一起看"); err != nil {
		t.Fatalf("加签失败: %v", err)
	}
	got, err := s.InstGet(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pending) != 2 {
		t.Fatalf("加签后待办 = %+v", got.Pending)
	}

	if _, err := s.InstRollback(ctx, inst.ID, "alice", "manager", "重审"); err != nil {
		t.Fatalf("回退失败: %v", err)
	}

	if _, err := s.InstWithdraw(ctx, inst.ID, "bob", ""); err == nil {
		t.Fatal("非提交人不应能撤回")
	} else if e, ok := err.(*response.Error); !ok || e.Status != http.StatusForbidden {
		t.Fatalf("越权撤回 = %v", err)
	}
	out, err := s.InstWithdraw(ctx, inst.ID, "alice", "不需要了")
	if err != nil || out.Status != StatusCancelled {
		t.Fatalf("撤回 = %v %v", out, err)
	}
}

func TestDefinitionSnapshotIsPinned(t *testing.T) {
	s := memService(t)
	def := savePublished(t, s)
	ctx := ctxOf()
	inst, err := s.InstStart(ctx, StartInput{DefID: idText(def.ID), Title: "改流程"}, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// 把流程改成「主管审批后直接结束」，删除会签节点。
	changed := Definition{
		Key: "leave", Name: "请假审批", Start: "start",
		Nodes: []Node{
			{Key: "start", Kind: KindStart},
			{Key: "manager", Name: "主管审批", Kind: KindApproval, Approvers: []string{"alice"}},
			{Key: "end", Kind: KindEnd},
		},
		Edges: []Edge{{From: "start", To: "manager"}, {From: "manager", To: "end"}},
	}
	if _, err := s.DefSave(ctx, def.ID, DefForm{Key: "leave", Name: "请假审批", Status: DefPublished, Definition: changed}); err != nil {
		t.Fatal(err)
	}
	out, err := s.InstApprove(ctx, inst.ID, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Current) != 1 || out.Current[0] != "hr" {
		t.Fatalf("在途实例必须按快照执行，Current = %v", out.Current)
	}
}

func TestDefJSONStillSupported(t *testing.T) {
	s := memService(t)
	def, err := json.Marshal(seqDefinition())
	if err != nil {
		t.Fatal(err)
	}
	_ = def
	v, err := s.DefSave(ctxOf(), 0, DefForm{Key: "d1", Name: "两级", Status: DefPublished, Definition: seqDefinition()})
	if err != nil {
		t.Fatalf("JSON 定义保存失败: %v", err)
	}
	xml, err := s.DefXML(ctxOf(), v.ID)
	if err != nil {
		t.Fatalf("无 XML 时应由引擎生成: %v", err)
	}
	if !strings.Contains(xml, "<bpmn:userTask") {
		t.Fatalf("生成的 XML 不合法:\n%s", xml)
	}
}

func seqDefinition() Definition {
	return Definition{
		Key: "d1", Name: "两级会签", Start: "start",
		Nodes: []Node{
			{Key: "start", Name: "开始", Kind: KindStart},
			{Key: "n1", Name: "保管员", Kind: KindApproval, Approvers: []string{"u1"}, Mode: ModeAny, EscalateTo: "n2"},
			{Key: "n2", Name: "科长", Kind: KindApproval, Approvers: []string{"u2", "u3"}, Mode: ModeAll},
			{Key: "end", Name: "结束", Kind: KindEnd},
		},
		Edges: []Edge{
			{From: "start", To: "n1"},
			{From: "n1", To: "n2"},
			{From: "n2", To: "end"},
		},
	}
}

// ---- 转办与驳回理由 ----

// 驳回必须给理由：空理由打回给提交人，不留任何解释是不行的。
func TestInstanceRejectRequiresComment(t *testing.T) {
	s := memService(t)
	def := savePublished(t, s)
	inst, err := s.InstStart(ctxOf(), StartInput{DefID: idText(def.ID), Title: "张三请假"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstReject(ctxOf(), inst.ID, "alice", "   "); err == nil {
		t.Fatal("空理由驳回必须被拒绝")
	} else if e, ok := err.(*response.Error); !ok || e.Key != "workflow.reject_comment_required" {
		t.Fatalf("error = %v", err)
	}
	if _, err := s.InstReject(ctxOf(), inst.ID, "alice", "材料不全"); err != nil {
		t.Fatal(err)
	}
}

// 转办：原审批人失去审批权，受让人接管；历史留痕 transfer。
func TestInstanceTransfer(t *testing.T) {
	s := memService(t)
	def := savePublished(t, s)
	inst, err := s.InstStart(ctxOf(), StartInput{DefID: idText(def.ID), Title: "张三请假"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	// 非当前审批人不能转办
	if _, err := s.InstTransfer(ctxOf(), inst.ID, "bob", "dave", ""); err == nil {
		t.Fatal("非审批人不应能转办")
	}
	transferred, err := s.InstTransfer(ctxOf(), inst.ID, "alice", "dave", "休假交接")
	if err != nil {
		t.Fatal(err)
	}
	if got := transferred.Pending; len(got) != 1 || got[0].Assignee != "dave" {
		t.Fatalf("待办应转给 dave, got %+v", got)
	}
	if _, err := s.InstApprove(ctxOf(), inst.ID, "alice", ""); err == nil {
		t.Fatal("转办后原审批人不应再能批")
	}
	if _, err := s.InstApprove(ctxOf(), inst.ID, "dave", "同意"); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := s.InstList(ctxOf(), InstQuery{Done: "1", Page: 1, PageSize: 20}, "dave"); total != 1 {
		t.Fatalf("受让人的我已办 = %d", total)
	}
	found := false
	for _, e := range transferred.History {
		if e.Action == "transfer" {
			found = true
		}
	}
	if !found {
		t.Fatal("转办必须留痕")
	}
}

// ---- 催办 ----

type urgeRecorder struct {
	urgent map[string]bool // actor → 收到过催办
}

func (r *urgeRecorder) Send(_ context.Context, m notify.Msg) error {
	for _, to := range m.To {
		r.urgent[to.String()] = true
	}
	return nil
}

// tctx 带租户的上下文：催办走请求上下文取租户。
func tctx(id int64) context.Context {
	return tenant.With(context.Background(), idgen.ID(id))
}

// 催办：只有发起人能催，收件人是当前待审批人（去重）；审批人之间不能互相催。
// 审批人用数字形态的用户 ID——站内信只认 ID，非 ID 主体（测试/外部系统的字符串）静默跳过。
func TestUrgeNotifiesPendingApprovers(t *testing.T) {
	s := memService(t)
	rec := &urgeRecorder{urgent: map[string]bool{}}
	s.SetUrgeSender(rec)
	def, err := s.DefSave(ctxOf(), 0, DefForm{Key: "urge", Name: "催办流程", Status: DefPublished, XML: `
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
  xmlns:flowlite="urn:flowlite:engine:1.0" id="D1" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="urge" name="催办流程" isExecutable="true">
    <bpmn:startEvent id="start" />
    <bpmn:userTask id="lead" name="主管审批" flowlite:approvers="9001" />
    <bpmn:userTask id="hr" name="HR 会签" flowlite:approvers="9002,9003" flowlite:mode="all" />
    <bpmn:endEvent id="end" />
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="lead" />
    <bpmn:sequenceFlow id="f2" sourceRef="lead" targetRef="hr" />
    <bpmn:sequenceFlow id="f3" sourceRef="hr" targetRef="end" />
  </bpmn:process>
</bpmn:definitions>`})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := s.InstStart(ctxOf(), StartInput{DefID: idText(def.ID), Title: "张三请假"}, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// 非发起人不能催
	if err := s.Urge(tctx(7), inst.ID, "9002"); err == nil {
		t.Fatal("非发起人不应能催办")
	}
	// 发起人催：主管审批节点 9001
	if err := s.Urge(tctx(7), inst.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if !rec.urgent["9001"] {
		t.Fatalf("9001 应收到催办, got %v", rec.urgent)
	}

	// 推进到会签节点：9002/9003 都该收到
	if _, err := s.InstApprove(ctxOf(), inst.ID, "9001", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Urge(tctx(7), inst.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if !rec.urgent["9002"] || !rec.urgent["9003"] {
		t.Fatalf("会签审批人应都收到催办, got %v", rec.urgent)
	}

	// 全部通过后运行态结束，催办被拒
	if _, err := s.InstApprove(ctxOf(), inst.ID, "9002", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstApprove(ctxOf(), inst.ID, "9003", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Urge(tctx(7), inst.ID, "alice"); err == nil {
		t.Fatal("已结束的流程不应能催办")
	}
}
