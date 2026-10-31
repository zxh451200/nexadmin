package engine

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// Namespace 是 flowlite 扩展属性使用的命名空间。bpmn-js 通过同名的 moddle 扩展
// 描述符保留这些属性；未注册描述符的建模器可能丢弃未知属性，因此引擎同时接受
// Camunda 的 assignee / candidateUsers 作为互操作输入。
const Namespace = "urn:flowlite:engine:1.0"

// ExportXML 把定义序列化为 BPMN 2.0 XML（本引擎可表达的子集）。
// 只导出 startEvent/endEvent/userTask/exclusiveGateway 与顺序流；不含 BPMNDiagram，
// 由 bpmn-js 等工具导入时自动布局。
func ExportXML(def Definition) (string, error) {
	if errs := Validate(def); len(errs) > 0 {
		return "", fmt.Errorf("%w: %s", ErrInvalidDefinition, strings.Join(errs, "；"))
	}
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:flowlite="urn:flowlite:engine:1.0" id="Definitions_flowlite" targetNamespace="urn:flowlite:engine:1.0">`)
	b.WriteString("\n")
	defaultByGateway := make(map[string]string, len(def.Nodes))
	for _, n := range def.Nodes {
		if n.Kind != KindExclusive {
			continue
		}
		for i, e := range def.Edges {
			if e.From == n.Key && strings.TrimSpace(e.When) == "" {
				defaultByGateway[n.Key] = flowIDAt(i, e)
				break
			}
		}
	}
	fmt.Fprintf(&b, "  <bpmn:process id=\"%s\" name=\"%s\" isExecutable=\"true\">\n", xmlAttr(def.Key), xmlAttr(def.Name))
	for _, n := range def.Nodes {
		id, name := xmlAttr(n.Key), xmlAttr(n.Name)
		switch n.Kind {
		case KindStart:
			fmt.Fprintf(&b, "    <bpmn:startEvent id=\"%s\" name=\"%s\"/>\n", id, name)
		case KindEnd:
			fmt.Fprintf(&b, "    <bpmn:endEvent id=\"%s\" name=\"%s\"/>\n", id, name)
		case KindApproval:
			fmt.Fprintf(&b, "    <bpmn:userTask id=\"%s\" name=\"%s\"%s/>\n", id, name, approvalAttrs(n))
		case KindExclusive:
			gatewayAttr := ""
			if fid, ok := defaultByGateway[n.Key]; ok {
				gatewayAttr = fmt.Sprintf(" default=\"%s\"", xmlAttr(fid))
			}
			fmt.Fprintf(&b, "    <bpmn:exclusiveGateway id=\"%s\" name=\"%s\"%s/>\n", id, name, gatewayAttr)
		}
	}
	for i, e := range def.Edges {
		fid, from, to := flowIDAt(i, e), xmlAttr(e.From), xmlAttr(e.To)
		if strings.TrimSpace(e.When) == "" {
			fmt.Fprintf(&b, "    <bpmn:sequenceFlow id=\"%s\" sourceRef=\"%s\" targetRef=\"%s\"/>\n", xmlAttr(fid), from, to)
			continue
		}
		fmt.Fprintf(&b, "    <bpmn:sequenceFlow id=\"%s\" sourceRef=\"%s\" targetRef=\"%s\">\n", xmlAttr(fid), from, to)
		fmt.Fprintf(&b, "      <bpmn:conditionExpression xsi:type=\"bpmn:tFormalExpression\">%s</bpmn:conditionExpression>\n", xmlText(e.When))
		b.WriteString("    </bpmn:sequenceFlow>\n")
	}
	b.WriteString("  </bpmn:process>\n")
	b.WriteString("</bpmn:definitions>\n")
	return b.String(), nil
}

func approvalAttrs(n Node) string {
	var parts []string
	if len(n.Approvers) > 0 {
		parts = append(parts, fmt.Sprintf("flowlite:approvers=\"%s\"", xmlAttr(strings.Join(n.Approvers, ","))))
	}
	if m := modeOf(n); m != ModeAny {
		parts = append(parts, fmt.Sprintf("flowlite:mode=\"%s\"", xmlAttr(m)))
	}
	if t := strings.TrimSpace(n.EscalateTo); t != "" {
		parts = append(parts, fmt.Sprintf("flowlite:escalateTo=\"%s\"", xmlAttr(t)))
	}
	if n.TimeoutMinutes > 0 {
		parts = append(parts, fmt.Sprintf("flowlite:timeoutMinutes=\"%d\"", n.TimeoutMinutes))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func flowIDAt(i int, e Edge) string {
	return fmt.Sprintf("flow_%d_%s_%s", i, e.From, e.To)
}

func xmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}

func xmlText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// ImportXML 解析 BPMN 2.0 XML 的本引擎子集，供标准建模器（bpmn-js / Camunda Modeler）
// 与引擎互换。忽略图形信息（BPMNDiagram），不支持的节点类型会明确报错而不是静默丢弃。
func ImportXML(raw string) (Definition, error) {
	var doc xmlDefinitions
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		return Definition{}, fmt.Errorf("%w: xml 解析失败: %v", ErrInvalidDefinition, err)
	}
	if len(doc.Processes) == 0 {
		return Definition{}, fmt.Errorf("%w: 未找到 bpmn:process", ErrInvalidDefinition)
	}
	p := doc.Processes[0]
	def := Definition{Key: strings.TrimSpace(p.ID), Name: p.Name}
	if def.Key == "" {
		def.Key = strings.TrimSpace(p.Name)
	}
	defaultFlows := make(map[string]bool)
	var flows []xmlNode
	for _, n := range p.Children {
		switch n.XMLName.Local {
		case "startEvent":
			def.Nodes = append(def.Nodes, Node{Key: n.ID, Name: n.Name, Kind: KindStart})
			if def.Start == "" {
				def.Start = n.ID
			} else {
				return Definition{}, fmt.Errorf("%w: 只支持一个开始事件", ErrInvalidDefinition)
			}
		case "endEvent":
			def.Nodes = append(def.Nodes, Node{Key: n.ID, Name: n.Name, Kind: KindEnd})
		case "userTask", "task":
			node := Node{
				Key: n.ID, Name: n.Name, Kind: KindApproval,
				Approvers: n.Approvers, Mode: n.Mode,
				EscalateTo: n.EscalateTo, TimeoutMinutes: n.TimeoutMinutes,
			}
			def.Nodes = append(def.Nodes, node)
		case "exclusiveGateway":
			def.Nodes = append(def.Nodes, Node{Key: n.ID, Name: n.Name, Kind: KindExclusive})
			if n.Default != "" {
				defaultFlows[n.Default] = true
			}
		case "sequenceFlow":
			flows = append(flows, n)
		case "documentation", "extensionElements", "laneSet", "textAnnotation", "association", "dataObject", "dataObjectReference":
			// 与执行语义无关，忽略。
		default:
			return Definition{}, fmt.Errorf("%w: 不支持的 BPMN 元素 <%s>（本引擎只支持开始/结束/用户任务/排他网关）", ErrInvalidDefinition, n.XMLName.Local)
		}
	}
	for _, f := range flows {
		when := ""
		if !defaultFlows[f.ID] && len(f.Conditions) > 0 {
			when = f.Conditions[0]
		}
		def.Edges = append(def.Edges, Edge{From: f.SourceRef, To: f.TargetRef, When: when})
	}
	if errs := Validate(def); len(errs) > 0 {
		return Definition{}, fmt.Errorf("%w: %s", ErrInvalidDefinition, strings.Join(errs, "；"))
	}
	return def, nil
}

type xmlDefinitions struct {
	XMLName   xml.Name     `xml:"definitions"`
	Processes []xmlProcess `xml:"process"`
}

type xmlProcess struct {
	ID       string    `xml:"id,attr"`
	Name     string    `xml:"name,attr"`
	Children []xmlNode `xml:",any"`
}

// xmlNode 用自定义 UnmarshalXML 读取属性：按 local name 匹配，命名空间无关，
// 这样 flowlite:、camunda:、或不带前缀的同名属性都能识别。
type xmlNode struct {
	XMLName        xml.Name
	ID             string
	Name           string
	SourceRef      string
	TargetRef      string
	Default        string
	Approvers      []string
	Mode           string
	EscalateTo     string
	TimeoutMinutes int
	Conditions     []string
}

func (n *xmlNode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	n.XMLName = start.Name
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "id":
			n.ID = a.Value
		case "name":
			n.Name = a.Value
		case "sourceRef":
			n.SourceRef = a.Value
		case "targetRef":
			n.TargetRef = a.Value
		case "default":
			n.Default = a.Value
		case "approvers", "candidateUsers":
			n.Approvers = splitApprovers(a.Value)
		case "assignee":
			if v := strings.TrimSpace(a.Value); v != "" {
				n.Approvers = []string{v}
			}
		case "mode":
			n.Mode = a.Value
		case "escalateTo":
			n.EscalateTo = a.Value
		case "timeoutMinutes":
			if v, err := strconv.Atoi(strings.TrimSpace(a.Value)); err == nil {
				n.TimeoutMinutes = v
			}
		}
	}
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "conditionExpression" {
				var text string
				if err := d.DecodeElement(&text, &t); err != nil {
					return err
				}
				if v := strings.TrimSpace(text); v != "" {
					n.Conditions = append(n.Conditions, v)
				}
				continue
			}
			if err := d.Skip(); err != nil {
				return err
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

func splitApprovers(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
