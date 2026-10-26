import { defineMock } from '@/framework/request/mock'
import type { Ticket, TicketReply } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const mkTicket = (n: number, p: Partial<Ticket>): Ticket => ({
  id: String(7430000000000000000n + BigInt(n)),
  ticketNo: `TK74300000000000000${n}`,
  bizType: 'trade_group',
  bizNo: n % 2 === 0 ? `G-2025060100${n}` : '',
  title: `工单示例 ${n}`,
  status: 'open',
  priority: 'normal',
  reporterType: 'reporter',
  reporterId: 'anon-1',
  reporterName: '微信用户 8127',
  assigneeId: '0',
  assigneeName: '',
  firstReplyAt: null,
  lastReplyAt: ago(n * 20),
  lastReplierType: 'reporter',
  lastPreview: '请帮我处理这个问题',
  overdue: false,
  createdAt: ago(n * 20 + 5),
  ...p,
})

const tickets: Ticket[] = [
  mkTicket(1, { status: 'processing', priority: 'high', assigneeId: '7', assigneeName: 'admin', firstReplyAt: ago(35), lastPreview: '已收到，正在处理' }),
  mkTicket(2, { priority: 'urgent', lastPreview: '急：商品破损' }),
  mkTicket(3, { status: 'resolved', lastPreview: '问题已解决，请确认' }),
  mkTicket(4, { status: 'closed', priority: 'low', lastPreview: '感谢反馈' }),
  mkTicket(5, { bizType: '', bizNo: '', title: '咨询发票开具' }),
]

const replies: Record<string, TicketReply[]> = {
  TK743000000000000001: [
    { id: '11', seq: 1, replierType: 'reporter', replierId: 'anon-1', replierName: '微信用户 8127', content: '订单里有一件商品破损，附照片了。', createdAt: ago(48) },
    { id: '12', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '已收到，正在处理，会尽快给您补发。', createdAt: ago(35) },
  ],
  TK743000000000000002: [
    { id: '21', seq: 1, replierType: 'reporter', replierId: 'anon-2', replierName: '游客 3391', content: '急：商品破损，麻烦尽快处理！', createdAt: ago(20) },
  ],
  TK743000000000000003: [
    { id: '31', seq: 1, replierType: 'reporter', replierId: 'anon-3', replierName: '会员 Alice', content: '退款金额不对。', createdAt: ago(90) },
    { id: '32', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '已重算并提交退款，问题已解决，请确认。', createdAt: ago(60) },
  ],
  TK743000000000000004: [
    { id: '41', seq: 1, replierType: 'reporter', replierId: 'anon-4', replierName: '游客 1101', content: '包装能不能换成环保材料？', createdAt: ago(60 * 30) },
    { id: '42', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '感谢反馈，已记录给产品团队。', createdAt: ago(60 * 28) },
  ],
  TK743000000000000005: [
    { id: '51', seq: 1, replierType: 'reporter', replierId: 'anon-5', replierName: '会员 Bob', content: '咨询发票开具。', createdAt: ago(15) },
  ],
}

const find = (no: string) => tickets.find((x) => x.ticketNo === no)

defineMock([
  {
    method: 'GET',
    url: '/plugins/ticket/tickets',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = tickets.filter(
        (tk) => !kw || tk.ticketNo.toLowerCase().includes(kw) || tk.title.toLowerCase().includes(kw) || tk.lastPreview.toLowerCase().includes(kw),
      )
      if (query.status) rows = rows.filter((tk) => tk.status === query.status)
      if (query.priority) rows = rows.filter((tk) => tk.priority === query.priority)
      if (query.bizType) rows = rows.filter((tk) => tk.bizType === query.bizType)
      return { list: rows, total: rows.length, page: 1, pageSize: rows.length }
    },
  },
  {
    method: 'GET',
    url: '/plugins/ticket/tickets/:no/replies',
    handler: ({ params }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      return { ticket: tk, replies: replies[params.no] ?? [] }
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/replies',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      const seq = tk.lastReplierType === '_' ? 1 : (replies[params.no]?.length ?? 0) + 1
      const r: TicketReply = {
        id: String(7431000000000000000n + BigInt(seq)),
        seq,
        replierType: 'agent',
        replierId: '7',
        replierName: 'admin',
        content: String(body.content ?? ''),
        createdAt: new Date().toISOString(),
      }
      replies[params.no] = [...(replies[params.no] ?? []), r]
      tk.lastPreview = r.content
      tk.lastReplyAt = r.createdAt
      tk.lastReplierType = 'agent'
      if (tk.status === 'open' || tk.status === 'resolved') tk.status = 'processing'
      if (tk.assigneeId === '0') {
        tk.assigneeId = '7'
        tk.assigneeName = 'admin'
      }
      if (tk.firstReplyAt == null) tk.firstReplyAt = r.createdAt
      return { reply: r, ticket: tk }
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/status',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      tk.status = String(body.status ?? 'open') as Ticket['status']
      return tk
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/assign',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      tk.assigneeId = String(body.assigneeId ?? '7')
      tk.assigneeName = String(body.assigneeName ?? 'admin')
      return tk
    },
  },
])
