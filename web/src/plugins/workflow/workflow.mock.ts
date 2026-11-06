import { defineMock, mockError } from '@/framework/request/mock'
import type { WfDef, WfDefVersion, WfDefinition, WfInst } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const def: WfDefinition = {
  key: 'expense_approval',
  name: '费用报销审批',
  start: 'start',
  nodes: [
    { key: 'start', name: '开始', kind: 'start' },
    { key: 'lead', name: '主管审批', kind: 'approval', approvers: ['1'], mode: 'any', escalateTo: 'director', timeoutMinutes: 1440 },
    { key: 'director', name: '总监审批', kind: 'approval', approvers: ['1'], mode: 'any' },
    { key: 'end', name: '结束', kind: 'end' },
  ],
  edges: [
    { from: 'start', to: 'lead' },
    { from: 'lead', to: 'director' },
    { from: 'director', to: 'end' },
  ],
}

const defXml = `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:flowlite="urn:flowlite:engine:1.0" id="Definitions_mock" targetNamespace="urn:flowlite:engine:1.0">
  <bpmn:process id="expense_approval" name="费用报销审批" isExecutable="true">
    <bpmn:startEvent id="start" name="开始" />
    <bpmn:userTask id="lead" name="主管审批" flowlite:approvers="1" flowlite:escalateTo="director" flowlite:timeoutMinutes="1440" />
    <bpmn:userTask id="director" name="总监审批" flowlite:approvers="1" />
    <bpmn:endEvent id="end" name="结束" />
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="lead" />
    <bpmn:sequenceFlow id="f2" sourceRef="lead" targetRef="director" />
    <bpmn:sequenceFlow id="f3" sourceRef="director" targetRef="end" />
  </bpmn:process>
</bpmn:definitions>`

const insts: WfInst[] = ([
  {
    id: '7440000000000000001',
    defId: '1',
    defKey: 'expense_approval',
    title: '差旅费报销 ¥3,200',
    bizType: 'expense',
    businessKey: 'EXP-2025-001',
    submitter: '1800000000000000001',
    status: 'running',
    current: ['director'],
    done: ['start', 'lead'],
    vars: { amount: '3200' },
    votes: { lead: { '1': 'approve' } },
    history: [
      { at: ago(300), node: 'start', actor: '1800000000000000001', action: 'start' },
      { at: ago(120), node: 'lead', actor: '1800000000000000001', action: 'approve', comment: '金额无误' },
    ],
    createdAt: ago(300),
    updatedAt: ago(120),
  },
  {
    id: '7440000000000000002',
    defId: '1',
    defKey: 'expense_approval',
    title: '办公用品采购 ¥180',
    bizType: 'expense',
    businessKey: 'EXP-2025-002',
    submitter: '1800000000000000001',
    status: 'approved',
    current: [],
    done: ['start', 'lead', 'director', 'end'],
    vars: { amount: '180' },
    votes: { lead: { '1': 'approve' }, director: { '1': 'approve' } },
    history: [
      { at: ago(600), node: 'start', actor: '1800000000000000001', action: 'start' },
      { at: ago(500), node: 'lead', actor: '1800000000000000001', action: 'approve' },
      { at: ago(480), node: 'director', actor: '1800000000000000001', action: 'approve' },
    ],
    createdAt: ago(600),
    updatedAt: ago(480),
  },
  {
    id: '7440000000000000003',
    defId: '1',
    defKey: 'expense_approval',
    title: '客户招待费 ¥9,800',
    bizType: 'expense',
    businessKey: 'EXP-2025-003',
    submitter: '1800000000000000002',
    status: 'rejected',
    current: [],
    done: ['start', 'lead'],
    vars: { amount: '9800' },
    votes: { lead: { '1': 'reject' } },
    history: [
      { at: ago(900), node: 'start', actor: '1800000000000000002', action: 'start' },
      { at: ago(800), node: 'lead', actor: '1800000000000000001', action: 'reject', comment: '超出季度预算' },
    ],
    createdAt: ago(900),
    updatedAt: ago(800),
  },
] as unknown as Omit<WfInst, 'revision' | 'defVersion' | 'pending' | 'definition'>[]).map((x) => ({
  ...x,
  revision: 1,
  defVersion: 3,
  definition: def,
  pending:
    x.status === 'running'
      ? [{ nodeKey: x.current[0] ?? '', nodeName: '总监审批', assignee: '1790000000000000001' }]
      : [],
}))

// 流程定义：列表要能被真删（含批量），所以放在可变数组里，而不是 GET handler 里写死一份。
// ID 用 19 位数字串（与后端 idgen 雪花同形状）：后端 DELETE /defs?ids= 只收正整数，
// mock 里用 d1/d2 这种 ID 的话前端一删就 400，接不上。
let defs: WfDef[] = [
  {
    id: '7440000000000000001',
    key: def.key,
    name: def.name,
    status: 'published',
    version: 3,
    definition: def,
    xml: defXml,
    createdAt: ago(60 * 24 * 30),
    updatedAt: ago(60 * 24 * 2),
  },
  {
    id: '7440000000000000002',
    key: 'leave_approval',
    name: '请假审批',
    status: 'draft',
    version: 1,
    definition: def,
    xml: defXml,
    createdAt: ago(60 * 24 * 6),
    updatedAt: ago(60 * 24),
  },
]

// 历史版本不含当前版本、列表新→旧（同后端 DefVersions）；XML 另按 `${defId}:${version}` 取，DefVersionView 里没有 XML。
const defHistory: Record<string, WfDefVersion[]> = {
  '7440000000000000001': [
    { version: 2, name: def.name, status: 'published', updatedAt: ago(60 * 24 * 9) },
    { version: 1, name: def.name, status: 'draft', updatedAt: ago(60 * 24 * 28) },
  ],
}
const defHistoryXml: Record<string, string> = {
  '7440000000000000001:2': defXml.replace('Definitions_mock', 'Definitions_mock_v2').replace('flowlite:timeoutMinutes="1440"', 'flowlite:timeoutMinutes="720"'),
  '7440000000000000001:1': defXml.replace('Definitions_mock', 'Definitions_mock_v1').replace(' flowlite:escalateTo="director" flowlite:timeoutMinutes="1440"', ''),
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/workflow/defs',
    handler: ({ query }) => {
      const page = Number(query.page ?? 1)
      const pageSize = Number(query.pageSize ?? 10)
      return { list: defs.slice((page - 1) * pageSize, page * pageSize), total: defs.length, page, pageSize }
    },
  },
  { method: 'GET', url: '/plugins/workflow/templates', handler: () => [
    { key: 'general_two_step', name: '两级审批(或签+超时升级)', definition: def, xml: defXml },
  ] },
  {
    method: 'GET',
    url: '/plugins/workflow/instances',
    handler: ({ query }) => {
      let rows = insts
      if (query.status) rows = rows.filter((x) => x.status === query.status)
      if (query.bizType) rows = rows.filter((x) => x.bizType === query.bizType)
      if (query.keyword) {
        const kw = String(query.keyword).toLowerCase()
        rows = rows.filter((x) => [x.title, x.defKey, x.businessKey].some((v) => v?.toLowerCase().includes(kw)))
      }
      if (query.mine === 'started') rows = rows.filter((x) => x.submitter === '1')
      if (query.todo === '1') rows = rows.filter((x) => x.status === 'running' && x.pending.some((p) => !p.assignee || p.assignee === '1790000000000000001'))
      if (query.done === '1') rows = rows.filter((x) => x.history.some((e) => e.actor === '1'))
      const page = Number(query.page ?? 1)
      const pageSize = Number(query.pageSize ?? 20)
      return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances',
    handler: ({ body }) => {
      const x: WfInst = {
        id: `mock-${Date.now()}`,
        defId: String(body.defId ?? '1'),
        defKey: def.key,
        defVersion: 3,
        title: String(body.title ?? ''),
        bizType: String(body.bizType ?? ''),
        businessKey: String(body.businessKey ?? ''),
        submitter: '1',
        status: 'running',
        revision: 1,
        current: ['lead'],
        done: ['start'],
        vars: body.vars && typeof body.vars === 'object' ? body.vars : {},
        votes: {},
        history: [{ at: new Date().toISOString(), node: 'start', actor: '1800000000000000001', action: 'start' }],
        pending: [{ nodeKey: 'lead', nodeName: '主管审批', assignee: '1790000000000000001' }],
        definition: def,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      }
      insts.unshift(x)
      return x
    },
  },
  {
    method: 'GET',
    url: '/plugins/workflow/instances/:id',
    handler: ({ params }) => insts.find((x) => x.id === params.id) ?? { error: 'not found' },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/approve',
    handler: ({ params, body }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: '1800000000000000001', action: 'approve', comment: String(body.comment ?? '') })
      const idx = x.current.indexOf(x.current[0])
      if (idx >= 0) x.current.splice(idx, 1)
      if (x.current.length === 0) {
        x.status = 'approved'
        x.done.push('end')
      }
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/reject',
    handler: ({ params, body }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: '1800000000000000001', action: 'reject', comment: String(body.comment ?? '') })
      x.current = []
      x.status = 'rejected'
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/withdraw',
    handler: ({ params }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.status = 'cancelled'
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/countersign',
    handler: ({ params, body }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.pending.push({ nodeKey: x.current[0] ?? '', nodeName: '加签审批', assignee: String(body.approver ?? '') })
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: '1800000000000000001', action: 'countersign', comment: String(body.comment ?? '') })
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/transfer',
    handler: ({ params, body }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      // 转办：当前节点的待办整组换成受让人
      const to = String(body.to ?? '')
      x.pending = x.pending.filter((p) => p.nodeKey !== (x.current[0] ?? '') || p.assignee === '')
      if (to) x.pending.push({ nodeKey: x.current[0] ?? '', nodeName: '转办审批', assignee: to })
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: '1800000000000000001', action: 'transfer', comment: String(body.comment ?? '') })
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'GET',
    url: '/plugins/workflow/todo-count',
    handler: () => {
      const me = '1790000000000000001'
      return { count: insts.filter((x) => x.status === 'running' && x.pending.some((p) => !p.assignee || p.assignee === me)).length }
    },
  },
  {
    method: 'GET',
    url: '/plugins/workflow/stats',
    // 与后端 Stats 同口径：各状态实例数 + 我的待办。缺这条时流程中心的卡片恒为 0，
    // 开发态看不出「加了一张卡但拿不到数」这类回归。
    handler: () => {
      const me = '1790000000000000001'
      const by = (s: string) => insts.filter((x) => x.status === s).length
      return {
        running: by('running'),
        approved: by('approved'),
        rejected: by('rejected'),
        cancelled: by('cancelled'),
        todo: insts.filter((x) => x.status === 'running' && x.pending.some((p) => !p.assignee || p.assignee === me)).length,
      }
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/rollback',
    handler: ({ params, body }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.current = [String(body.toNode ?? 'lead')]
      x.status = 'running'
      x.history.push({ at: new Date().toISOString(), node: String(body.toNode ?? ''), actor: '1800000000000000001', action: 'rollback', comment: String(body.comment ?? '') })
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/escalate',
    handler: ({ params }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: 'system', action: 'escalate' })
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/instances/:id/urge',
    handler: ({ params }) => {
      const x = insts.find((i) => i.id === params.id) ?? mockError(404, 'Not found')
      if (x.status !== 'running') mockError(400, 'The workflow has finished and cannot be advanced')
      return null
    },
  },
  { method: 'POST', url: '/plugins/workflow/defs', handler: ({ body }) => body },
  { method: 'PUT', url: '/plugins/workflow/defs/:id', handler: ({ body }) => body },
  {
    method: 'GET',
    url: '/plugins/workflow/defs/:id/versions',
    handler: ({ params }) => {
      if (!defs.find((d) => d.id === params.id)) mockError(404, 'Not found')
      return defHistory[params.id] ?? []
    },
  },
  {
    method: 'GET',
    url: '/plugins/workflow/defs/:id/versions/:ver/xml',
    handler: ({ params, query }) => {
      if (!/^\d+$/.test(params.ver) || Number(params.ver) <= 0) mockError(400, 'Bad request')
      if (!defs.find((d) => d.id === params.id)) mockError(404, 'Not found')
      const ver = Number(params.ver)
      const row = (defHistory[params.id] ?? []).find((v) => v.version === ver)
      if (!row) mockError(404, 'Not found')
      const xml = defHistoryXml[`${params.id}:${row.version}`] ?? defXml
      return query.format === 'json' ? { xml } : xml
    },
  },
  {
    method: 'POST',
    url: '/plugins/workflow/defs/:id/versions/:ver/rollback',
    handler: ({ params }) => {
      if (!/^\d+$/.test(params.ver) || Number(params.ver) <= 0) mockError(400, 'Bad request')
      const d = defs.find((x) => x.id === params.id) ?? mockError(404, 'Not found')
      const ver = Number(params.ver)
      const rows = defHistory[d.id] ?? []
      if (!rows.some((v) => v.version === ver)) mockError(404, 'Not found')
      defHistoryXml[`${d.id}:${d.version}`] = d.xml ?? defXml
      rows.unshift({ version: d.version, name: d.name, status: d.status, updatedAt: d.updatedAt })
      d.xml = defHistoryXml[`${d.id}:${ver}`] ?? defXml
      d.version += 1
      d.updatedAt = new Date().toISOString()
      return d
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/workflow/defs/:id',
    handler: ({ params }) => {
      defs = defs.filter((d) => d.id !== params.id)
      return { ok: true }
    },
  },
  {
    // 批量删除：与后端 DELETE /plugins/workflow/defs?ids= 同口径（Service.DefDeleteMany）。
    // ids 缺失 / 整体为空 / 任一项不是正整数（含 0）一律 400，绝不退化成「删光所有定义」；
    // 只真删点名的那几条（实例、历史版本、待办都不动，与单条一致），返回实删条数 deleted；
    // 一条都没命中 404，不静默返回 deleted=0。
    method: 'DELETE',
    url: '/plugins/workflow/defs',
    handler: ({ query }) => {
      const parts = String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean)
      if (parts.length === 0 || parts.some((s) => !/^\d+$/.test(s) || /^0+$/.test(s))) mockError(400, 'Bad request')
      const ids = new Set(parts)
      const before = defs.length
      defs = defs.filter((d) => !ids.has(d.id))
      const deleted = before - defs.length
      if (deleted === 0) mockError(404, 'Not found')
      return { deleted }
    },
  },
])
