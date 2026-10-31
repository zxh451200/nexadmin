package engine

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestExportXML(t *testing.T) {
	out, err := ExportXML(branchDef())
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	for _, want := range []string{
		"<bpmn:startEvent", "<bpmn:userTask", "<bpmn:exclusiveGateway", "<bpmn:endEvent",
		"conditionExpression", "amount&gt;10000", "bpmn:process", `default="flow_`,
		"xmlns:flowlite", `flowlite:approvers="u1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("XML 缺少 %q:\n%s", want, out)
		}
	}
	var parsed struct {
		XMLName xml.Name
		Process struct {
			ID string `xml:"id,attr"`
		} `xml:"process"`
	}
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("XML 不可解析: %v", err)
	}
	if parsed.Process.ID != "d2" {
		t.Fatalf("process id = %q", parsed.Process.ID)
	}
	if _, err := ExportXML(Definition{Key: "bad"}); err == nil {
		t.Fatal("非法定义导出应报错")
	}
}

func TestXMLRoundTrip(t *testing.T) {
	for _, def := range []Definition{seqDef(), branchDef()} {
		out, err := ExportXML(def)
		if err != nil {
			t.Fatalf("导出 %s 失败: %v", def.Key, err)
		}
		back, err := ImportXML(out)
		if err != nil {
			t.Fatalf("回导 %s 失败: %v\n%s", def.Key, err, out)
		}
		if !reflect.DeepEqual(normalize(def), normalize(back)) {
			t.Fatalf("往返不一致\n原始: %+v\n回导: %+v", def, back)
		}
	}
}

// normalize 把省略的默认值补全（mode 空 = any），避免往返比较被等价写法干扰。
func normalize(def Definition) Definition {
	out := def
	out.Nodes = append([]Node(nil), def.Nodes...)
	for i := range out.Nodes {
		out.Nodes[i].Mode = modeOf(out.Nodes[i])
		out.Nodes[i].Approvers = append([]string(nil), out.Nodes[i].Approvers...)
	}
	out.Edges = append([]Edge(nil), def.Edges...)
	return out
}

func TestImportXMLInterop(t *testing.T) {
	// 建模器常见形态：命名空间前缀不同、带 documentation / extensionElements / BPMNDiagram、
	// 审批人写在 camunda:candidateUsers 上、网关默认流带 default 属性。
	raw := `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
  xmlns:camunda="http://camunda.org/schema/1.0/bpmn"
  xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI"
  id="Definitions_1" targetNamespace="http://bpmn.io/schema/bpmn">
  <bpmn:process id="leave" name="请假审批" isExecutable="true">
    <bpmn:documentation>示例</bpmn:documentation>
    <bpmn:startEvent id="StartEvent_1" name="开始">
      <bpmn:extensionElements><camunda:properties/></bpmn:extensionElements>
    </bpmn:startEvent>
    <bpmn:userTask id="Task_Manager" name="主管审批" camunda:candidateUsers="u1,u2" />
    <bpmn:exclusiveGateway id="Gateway_1" name="天数判断" default="Flow_Default" />
    <bpmn:userTask id="Task_HR" name="HR 审批" camunda:assignee="hr" />
    <bpmn:endEvent id="EndEvent_1" name="结束" />
    <bpmn:sequenceFlow id="Flow_1" sourceRef="StartEvent_1" targetRef="Task_Manager" />
    <bpmn:sequenceFlow id="Flow_2" sourceRef="Task_Manager" targetRef="Gateway_1" />
    <bpmn:sequenceFlow id="Flow_Long" sourceRef="Gateway_1" targetRef="Task_HR">
      <bpmn:conditionExpression xsi:type="bpmn:tFormalExpression" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">days&gt;3</bpmn:conditionExpression>
    </bpmn:sequenceFlow>
    <bpmn:sequenceFlow id="Flow_Default" sourceRef="Gateway_1" targetRef="EndEvent_1" />
    <bpmn:sequenceFlow id="Flow_3" sourceRef="Task_HR" targetRef="EndEvent_1" />
    <bpmn:extensionElements />
  </bpmn:process>
  <bpmndi:BPMNDiagram id="BPMNDiagram_1"><bpmndi:BPMNPlane id="Plane_1" bpmnElement="leave" /></bpmndi:BPMNDiagram>
</bpmn:definitions>`

	def, err := ImportXML(raw)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if def.Key != "leave" || def.Name != "请假审批" {
		t.Fatalf("process = %q/%q", def.Key, def.Name)
	}
	if def.Start != "StartEvent_1" {
		t.Fatalf("Start = %q", def.Start)
	}
	var mgr, hr Node
	for _, n := range def.Nodes {
		switch n.Key {
		case "Task_Manager":
			mgr = n
		case "Task_HR":
			hr = n
		}
	}
	if !reflect.DeepEqual(mgr.Approvers, []string{"u1", "u2"}) {
		t.Fatalf("candidateUsers 未解析: %+v", mgr.Approvers)
	}
	if !reflect.DeepEqual(hr.Approvers, []string{"hr"}) {
		t.Fatalf("assignee 未解析: %+v", hr.Approvers)
	}
	var cond string
	for _, e := range def.Edges {
		if e.From == "Gateway_1" && e.To == "Task_HR" {
			cond = e.When
		}
		if e.From == "Gateway_1" && e.To == "EndEvent_1" && e.When != "" {
			t.Fatalf("默认流不应带条件: %q", e.When)
		}
	}
	if cond != "days>3" {
		t.Fatalf("条件 = %q", cond)
	}
}

func TestImportXMLUnsupported(t *testing.T) {
	raw := `<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <bpmn:process id="p" name="并行">
    <bpmn:startEvent id="s" />
    <bpmn:parallelGateway id="pg" name="并行网关" />
    <bpmn:endEvent id="e" />
    <bpmn:sequenceFlow id="f1" sourceRef="s" targetRef="pg" />
    <bpmn:sequenceFlow id="f2" sourceRef="pg" targetRef="e" />
  </bpmn:process>
</bpmn:definitions>`
	if _, err := ImportXML(raw); err == nil || !strings.Contains(err.Error(), "parallelGateway") {
		t.Fatalf("不支持的节点应明确报错，实际 %v", err)
	}
}

// TestImportXMLBpmnJsOutput 模拟 bpmn-js 保存出来的 XML：带 DI、带 incoming/outgoing 子元素、
// 扩展属性写在 flowlite 命名空间上。导入必须只取执行语义，不能被图形信息和子元素干扰。
func TestImportXMLBpmnJsOutput(t *testing.T) {
	raw := `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
  xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI"
  xmlns:dc="http://www.omg.org/spec/DD/20100524/DC"
  xmlns:di="http://www.omg.org/spec/DD/20100524/DI"
  xmlns:flowlite="urn:flowlite:engine:1.0"
  id="Definitions_1" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="p" name="bpmn-js 输出" isExecutable="true">
    <bpmn:startEvent id="start" name="开始">
      <bpmn:outgoing>f1</bpmn:outgoing>
    </bpmn:startEvent>
    <bpmn:userTask id="a" name="审批A" flowlite:approvers="u1,u2" flowlite:mode="all" flowlite:timeoutMinutes="30" flowlite:escalateTo="b">
      <bpmn:incoming>f1</bpmn:incoming>
      <bpmn:outgoing>f2</bpmn:outgoing>
    </bpmn:userTask>
    <bpmn:userTask id="b" name="审批B">
      <bpmn:incoming>f2</bpmn:incoming>
      <bpmn:outgoing>f3</bpmn:outgoing>
    </bpmn:userTask>
    <bpmn:endEvent id="end" name="结束">
      <bpmn:incoming>f3</bpmn:incoming>
    </bpmn:endEvent>
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="a" />
    <bpmn:sequenceFlow id="f2" sourceRef="a" targetRef="b" />
    <bpmn:sequenceFlow id="f3" sourceRef="b" targetRef="end" />
  </bpmn:process>
  <bpmndi:BPMNDiagram id="BPMNDiagram_1">
    <bpmndi:BPMNPlane id="BPMNPlane_1" bpmnElement="p">
      <bpmndi:BPMNShape id="a_di" bpmnElement="a">
        <dc:Bounds x="160" y="80" width="100" height="80" />
      </bpmndi:BPMNShape>
    </bpmndi:BPMNPlane>
  </bpmndi:BPMNDiagram>
</bpmn:definitions>`
	def, err := ImportXML(raw)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if def.Key != "p" || def.Start != "start" || len(def.Nodes) != 4 || len(def.Edges) != 3 {
		t.Fatalf("定义 = %+v", def)
	}
	var a Node
	for _, n := range def.Nodes {
		if n.Key == "a" {
			a = n
		}
	}
	if !reflect.DeepEqual(a.Approvers, []string{"u1", "u2"}) || a.Mode != "all" || a.TimeoutMinutes != 30 || a.EscalateTo != "b" {
		t.Fatalf("扩展属性未解析: %+v", a)
	}
}
