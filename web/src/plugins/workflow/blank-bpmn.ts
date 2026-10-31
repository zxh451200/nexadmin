// 新建流程的空白 BPMN：开始 → 审批 → 结束。与建模器组件分开，
// 这样列表页不需要为了拿一个常量而加载 bpmn-js。
export const BLANK_BPMN_XML = `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:flowlite="urn:flowlite:engine:1.0" id="Definitions_flowlite" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="new_process" name="新流程" isExecutable="true">
    <bpmn:startEvent id="start" name="开始" />
    <bpmn:userTask id="approve" name="审批" />
    <bpmn:endEvent id="end" name="结束" />
    <bpmn:sequenceFlow id="flow_1" sourceRef="start" targetRef="approve" />
    <bpmn:sequenceFlow id="flow_2" sourceRef="approve" targetRef="end" />
  </bpmn:process>
</bpmn:definitions>`
