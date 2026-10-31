import { defineMock } from '@/framework/request/mock'
import type { CannedReply, Ticket, TicketReply } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

/** 工单 n 的创建时间距现在的分钟数：按天错开，让 /tickets/stats 的按天趋势有多于一个点 */
const bornMin = (n: number) => 24 * 60 * (n - 1) + 9 * 60
/** 工单 n 创建后第 min 分钟 */
const at = (n: number, min: number) => ago(bornMin(n) - min)
/** 本地日号 YYYY-MM-DD：与后端按应用时区分桶保持一致 */
const dayKey = (iso: string) => {
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

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
  creatorName: '',
  assigneeId: '0',
  assigneeName: '',
  firstReplyAt: null,
  lastReplyAt: at(n, 20 * n),
  lastReplierType: 'reporter',
  lastPreview: '请帮我处理这个问题',
  overdue: false,
  rating: null,
  ratingComment: '',
  createdAt: at(n, 0),
  ...p,
})

const tickets: Ticket[] = [
  mkTicket(1, {
    status: 'pending',
    priority: 'high',
    creatorName: 'admin',
    assigneeId: '7',
    assigneeName: 'admin',
    firstReplyAt: at(1, 50),
    lastPreview: '已收到，正在处理',
    slaPausedAt: at(1, 50),
  }),
  mkTicket(2, { priority: 'urgent', overdue: true, lastPreview: '急：商品破损' }),
  mkTicket(3, { status: 'resolved', lastPreview: '问题已解决，请确认', resolvedAt: at(3, 95), rating: 5, ratingComment: '处理很快，感谢' }),
  mkTicket(4, { status: 'closed', priority: 'low', lastPreview: '感谢反馈', resolvedAt: at(4, 120), rating: 4 }),
  mkTicket(5, { bizType: '', bizNo: '', title: '咨询发票开具' }),
  mkTicket(6, { status: 'processing', priority: 'normal', assigneeId: '7', assigneeName: 'admin', lastPreview: '正在核实订单' }),
]

const replies: Record<string, TicketReply[]> = {
  TK743000000000000001: [
    { id: '11', seq: 1, replierType: 'reporter', replierId: 'anon-1', replierName: '微信用户 8127', content: '订单里有一件商品破损，附照片了。', createdAt: at(1, 15) },
    { id: '12', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '已收到，正在处理，会尽快给您补发。', createdAt: at(1, 50) },
    { id: '13', seq: 3, replierType: 'agent', replierId: '7', replierName: 'admin', content: '内部备注：仓库反馈缺货，预计周四补发。', internal: true, createdAt: at(1, 55) },
  ],
  TK743000000000000002: [
    { id: '21', seq: 1, replierType: 'reporter', replierId: 'anon-2', replierName: '游客 3391', content: '急：商品破损，麻烦尽快处理！', createdAt: at(2, 10) },
  ],
  TK743000000000000003: [
    { id: '31', seq: 1, replierType: 'reporter', replierId: 'anon-3', replierName: '会员 Alice', content: '退款金额不对。', createdAt: at(3, 25) },
    { id: '32', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '已重算并提交退款，问题已解决，请确认。', createdAt: at(3, 95) },
  ],
  TK743000000000000004: [
    { id: '41', seq: 1, replierType: 'reporter', replierId: 'anon-4', replierName: '游客 1101', content: '包装能不能换成环保材料？', createdAt: at(4, 30) },
    { id: '42', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '感谢反馈，已记录给产品团队。', createdAt: at(4, 120) },
  ],
  TK743000000000000005: [
    { id: '51', seq: 1, replierType: 'reporter', replierId: 'anon-5', replierName: '会员 Bob', content: '咨询发票开具。', createdAt: at(5, 12) },
  ],
  TK743000000000000006: [
    { id: '61', seq: 1, replierType: 'reporter', replierId: 'anon-6', replierName: '会员 Carol', content: '订单一直没发货。', createdAt: at(6, 20) },
    { id: '62', seq: 2, replierType: 'agent', replierId: '7', replierName: 'admin', content: '正在核实仓库发货记录。', createdAt: at(6, 40) },
  ],
}

const canned: CannedReply[] = [
  { id: '801', title: '已受理', content: '已收到您的问题，我们正在处理，请稍候。', sort: 0 },
  { id: '802', title: '需要更多信息', content: '麻烦提供一下订单号和问题截图，方便我们定位。', sort: 1 },
]

const find = (no: string) => tickets.find((x) => x.ticketNo === no)
const nextId = () => String(7432000000000000000n + BigInt(Date.now() % 1_000_000))

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
      if (String(query.overdue ?? '') === '1') rows = rows.filter((tk) => tk.overdue)
      if (String(query.unassigned ?? '') === '1') rows = rows.filter((tk) => !tk.assigneeId || tk.assigneeId === '0')
      else if (String(query.mine ?? '') === '1') rows = rows.filter((tk) => tk.assigneeId === '7')
      else if (query.assigneeId) rows = rows.filter((tk) => tk.assigneeId === String(query.assigneeId))
      const page = Number(query.page ?? 1) || 1
      const pageSize = Number(query.pageSize ?? 20) || 20
      const start = (page - 1) * pageSize
      return { list: rows.slice(start, start + pageSize), total: rows.length, page, pageSize }
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets',
    handler: ({ body }) => {
      const n = tickets.length + 1
      const tk = mkTicket(n, {
        title: String(body.title ?? '新工单'),
        priority: (body.priority as Ticket['priority']) ?? 'normal',
        bizType: String(body.bizType ?? ''),
        bizNo: String(body.bizNo ?? ''),
        reporterName: String(body.reporterName ?? ''),
        reporterId: String(body.reporterId ?? ''),
        creatorName: 'admin',
        lastPreview: String(body.content ?? ''),
      })
      tickets.unshift(tk)
      replies[tk.ticketNo] = [
        {
          id: nextId(),
          seq: 1,
          replierType: 'reporter',
          replierId: tk.reporterId,
          replierName: tk.reporterName,
          content: String(body.content ?? ''),
          createdAt: tk.createdAt,
        },
      ]
      return tk
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/bulk',
    handler: ({ body }) => {
      const nos: string[] = Array.isArray(body.ticketNos) ? body.ticketNos.map(String) : []
      const action = String(body.action ?? '')
      let updated = 0
      let skipped = 0
      for (const no of nos) {
        const tk = find(no)
        if (!tk) {
          skipped += 1
          continue
        }
        if (action === 'assign') {
          tk.assigneeId = String(body.assigneeId ?? '7')
          tk.assigneeName = String(body.assigneeName ?? 'admin')
        } else if (action === 'status') {
          tk.status = String(body.status ?? tk.status) as Ticket['status']
          if (tk.status === 'resolved') tk.resolvedAt = new Date().toISOString()
          if (tk.status === 'closed') tk.closedAt = new Date().toISOString()
        } else if (action === 'priority') {
          tk.priority = String(body.priority ?? tk.priority) as Ticket['priority']
        } else {
          skipped += 1
          continue
        }
        updated += 1
      }
      return { updated, skipped }
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
      const seq = (replies[params.no]?.length ?? 0) + 1
      const internal = body.internal === true
      const r: TicketReply = {
        id: nextId(),
        seq,
        replierType: 'agent',
        replierId: '7',
        replierName: 'admin',
        content: String(body.content ?? ''),
        internal,
        createdAt: new Date().toISOString(),
      }
      replies[params.no] = [...(replies[params.no] ?? []), r]
      tk.lastReplyAt = r.createdAt
      if (!internal) {
        tk.lastPreview = r.content
        tk.lastReplierType = 'agent'
        tk.status = 'pending'
        tk.slaPausedAt = r.createdAt
        if (tk.assigneeId === '0') {
          tk.assigneeId = '7'
          tk.assigneeName = 'admin'
        }
        if (tk.firstReplyAt == null) tk.firstReplyAt = r.createdAt
      }
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
      if (tk.status === 'pending') tk.slaPausedAt = new Date().toISOString()
      if (tk.status === 'processing') {
        tk.slaPausedAt = null
        tk.resolvedAt = null
        tk.closedAt = null
      }
      if (tk.status === 'resolved') tk.resolvedAt = new Date().toISOString()
      if (tk.status === 'closed') tk.closedAt = new Date().toISOString()
      return tk
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/assign',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      const id = String(body.assigneeId ?? '7')
      tk.assigneeId = id === '0' ? '7' : id
      tk.assigneeName = String(body.assigneeName ?? '') || (tk.assigneeId === '7' ? 'admin' : `用户 ${tk.assigneeId}`)
      return tk
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/rating',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      if (tk.status !== 'resolved' && tk.status !== 'closed') throw new Error('rating not allowed')
      if (tk.rating) throw new Error('already rated')
      const score = Number(body.score)
      if (!(score >= 1 && score <= 5)) throw new Error('bad score')
      tk.rating = score
      tk.ratingComment = String(body.comment ?? '')
      tk.ratedAt = new Date().toISOString()
      return tk
    },
  },
  {
    method: 'GET',
    url: '/plugins/ticket/canned',
    handler: () => canned,
  },
  {
    method: 'POST',
    url: '/plugins/ticket/canned',
    handler: ({ body }) => {
      const row: CannedReply = {
        id: nextId(),
        title: String(body.title ?? ''),
        content: String(body.content ?? ''),
        sort: Number(body.sort ?? canned.length),
      }
      canned.push(row)
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/ticket/canned/:id',
    handler: ({ params, body }) => {
      const row = canned.find((c) => c.id === params.id)
      if (!row) throw new Error('not found')
      row.title = String(body.title ?? row.title)
      row.content = String(body.content ?? row.content)
      row.sort = Number(body.sort ?? row.sort)
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/ticket/canned/:id',
    handler: ({ params }) => {
      const idx = canned.findIndex((c) => c.id === params.id)
      if (idx >= 0) canned.splice(idx, 1)
      return { id: params.id }
    },
  },
  {
    // 与真实 /tickets/stats 同口径：队列结构、首响/解决分位、按优先级 SLA 违约
    method: 'GET',
    url: '/plugins/ticket/tickets/stats',
    handler: () => {
      const tally = (keys: (string | undefined)[]) => {
        const m = new Map<string, number>()
        keys.forEach((k) => { const key = k && k.trim() ? k : '-'; m.set(key, (m.get(key) ?? 0) + 1) })
        return [...m].map(([name, value]) => ({ name, value })).sort((a, b) => b.value - a.value || a.name.localeCompare(b.name)).slice(0, 8)
      }
      const pick = (xs: number[], q: number) => {
        if (!xs.length) return 0
        const s = [...xs].sort((a, b) => a - b)
        return s[Math.min(s.length - 1, Math.max(0, Math.ceil(s.length * q) - 1))]
      }
      const avg = (xs: number[]) => (xs.length ? Math.round(xs.reduce((a, b) => a + b, 0) / xs.length) : 0)
      const now = Date.now()
      const slaMin: Record<string, number> = { urgent: 120, high: 480, normal: 1440, low: 4320 }
      const resp = tickets.filter((x) => x.firstReplyAt).map((x) => Math.round((new Date(x.firstReplyAt as string).getTime() - new Date(x.createdAt).getTime()) / 60000))
      const resolution = tickets
        .filter((x) => x.resolvedAt)
        .map((x) => Math.round((new Date(x.resolvedAt as string).getTime() - new Date(x.createdAt).getTime()) / 60000))
      const rated = tickets.filter((x) => typeof x.rating === 'number')
      const satisfaction = {
        rated: rated.length,
        avgScore: rated.length ? Math.round((rated.reduce((a, b) => a + (b.rating || 0), 0) / rated.length) * 10) / 10 : 0,
        byScore: [1, 2, 3, 4, 5].map((n) => ({ name: String(n), value: rated.filter((x) => x.rating === n).length })),
      }
      const prio = new Map<string, { total: number; breached: number }>()
      tickets.forEach((x) => {
        const cur = prio.get(x.priority) ?? { total: 0, breached: 0 }
        cur.total += 1
        const expired = x.overdue || (!x.firstReplyAt && !!x.dueAt && new Date(x.dueAt).getTime() < now)
        if (expired) cur.breached += 1
        prio.set(x.priority, cur)
      })
      const dayMap = new Map<string, { date: string; created: number; resolved: number }>()
      tickets.forEach((x) => {
        const key = dayKey(x.createdAt)
        const cur = dayMap.get(key) ?? { date: key, created: 0, resolved: 0 }
        cur.created += 1
        dayMap.set(key, cur)
        if (x.resolvedAt) {
          const rk = dayKey(x.resolvedAt)
          const rc = dayMap.get(rk) ?? { date: rk, created: 0, resolved: 0 }
          rc.resolved += 1
          dayMap.set(rk, rc)
        }
      })
      const count = (f: (t: (typeof tickets)[number]) => boolean) => tickets.filter(f).length
      // 与后端同口径：窗口取最近 30 天，缺数据的日期补 0，保证横轴连续
      const series: { date: string; created: number; resolved: number }[] = []
      for (let i = 30; i >= 0; i--) {
        const d = new Date()
        d.setHours(12, 0, 0, 0)
        d.setDate(d.getDate() - i)
        const key = dayKey(d.toISOString())
        series.push(dayMap.get(key) ?? { date: key, created: 0, resolved: 0 })
      }
      return {
        from: series[0].date, to: series[series.length - 1].date,
        total: tickets.length,
        open: count((t) => t.status === 'open'),
        processing: count((t) => t.status === 'processing'),
        pending: count((t) => t.status === 'pending'),
        resolved: count((t) => t.status === 'resolved'),
        closed: count((t) => t.status === 'closed'),
        unassigned: count((t) => !t.assigneeId || t.assigneeId === '0'),
        overdue: count((t) => t.overdue),
        sampled: false,
        days: series,
        byStatus: tally(tickets.map((t) => t.status)),
        byPriority: tally(tickets.map((t) => t.priority)),
        byBizType: tally(tickets.map((t) => t.bizType)),
        byAssignee: tally(tickets.map((t) => t.assigneeName)),
        firstResponse: {
          responded: resp.length,
          noResponse: tickets.length - resp.length,
          avgMinutes: avg(resp),
          p50Minutes: pick(resp, 0.5),
          p90Minutes: pick(resp, 0.9),
        },
        resolution: {
          resolved: resolution.length,
          avgMinutes: avg(resolution),
          p50Minutes: pick(resolution, 0.5),
          p90Minutes: pick(resolution, 0.9),
        },
        satisfaction,
        slaByPriority: [...prio.entries()]
          .sort((a, b) => a[0].localeCompare(b[0]))
          .map(([priority, v]) => ({
            priority,
            targetMinutes: slaMin[priority] ?? slaMin.normal,
            total: v.total,
            breached: v.breached,
            breachRate: v.total ? Math.round((v.breached / v.total) * 1000) / 10 : 0,
          })),
      }
    },
  },
])
