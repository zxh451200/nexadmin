import { defineMock } from '@/framework/request/mock'
import type { WfInst } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const def = {
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

const insts: WfInst[] = [
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
      { at: ago(120), node: 'lead', actor: '1', action: 'approve', comment: '金额无误' },
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
      { at: ago(500), node: 'lead', actor: '1', action: 'approve' },
      { at: ago(480), node: 'director', actor: '1', action: 'approve' },
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
      { at: ago(800), node: 'lead', actor: '1', action: 'reject', comment: '超出季度预算' },
    ],
    createdAt: ago(900),
    updatedAt: ago(800),
  },
]

defineMock([
  {
    method: 'GET',
    url: '/plugins/workflow/defs',
    handler: () => ({
      list: [
        {
          id: '1',
          key: def.key,
          name: def.name,
          status: 'published',
          version: 3,
          definition: def,
          createdAt: ago(60 * 24 * 30),
          updatedAt: ago(60 * 24 * 2),
        },
      ],
      total: 1,
      page: 1,
      pageSize: 10,
    }),
  },
  { method: 'GET', url: '/plugins/workflow/templates', handler: () => [
    { key: 'general_two_step', name: '两级审批(或签+超时升级)', definition: def },
  ] },
  {
    method: 'GET',
    url: '/plugins/workflow/instances',
    handler: ({ query }) => {
      let rows = insts
      if (query.status) rows = rows.filter((x) => x.status === query.status)
      if (query.bizType) rows = rows.filter((x) => x.bizType === query.bizType)
      if (query.todo === '1') rows = rows.filter((x) => x.status === 'running' && x.current.includes('director'))
      if (query.done === '1') rows = rows.filter((x) => x.history.some((e) => e.actor === '1'))
      return { list: rows, total: rows.length, page: 1, pageSize: rows.length }
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
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: '1', action: 'approve', comment: String(body.comment ?? '') })
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
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: '1', action: 'reject', comment: String(body.comment ?? '') })
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
    url: '/plugins/workflow/instances/:id/escalate',
    handler: ({ params }) => {
      const x = insts.find((i) => i.id === params.id)
      if (!x) throw new Error('not found')
      x.history.push({ at: new Date().toISOString(), node: x.current[0] ?? '', actor: 'system', action: 'escalate' })
      x.updatedAt = new Date().toISOString()
      return x
    },
  },
  { method: 'POST', url: '/plugins/workflow/defs', handler: ({ body }) => body },
  { method: 'PUT', url: '/plugins/workflow/defs/:id', handler: ({ body }) => body },
  { method: 'DELETE', url: '/plugins/workflow/defs/:id', handler: () => ({ ok: true }) },
])
