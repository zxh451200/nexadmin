import { defineMock } from '@/framework/request/mock'
import type {
  CannedReply,
  SLASetting,
  Ticket,
  TicketActivity,
  TicketAttachment,
  TicketGroup,
  TicketReply,
  TicketRule,
} from './api'

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
  groupId: '',
  groupName: '',
  assigneeId: '0',
  assigneeName: '',
  firstReplyAt: null,
  lastReplyAt: at(n, 20 * n),
  lastReplierType: 'reporter',
  lastPreview: '请帮我处理这个问题',
  overdue: false,
  rating: null,
  ratingComment: '',
  tags: [],
  resolutionOverdue: false,
  createdAt: at(n, 0),
  ...p,
})

const tickets: Ticket[] = [
  mkTicket(1, {
    status: 'pending',
    priority: 'high',
    creatorName: 'admin',
    groupId: '901',
    groupName: '售后一组',
    assigneeId: '7',
    assigneeName: 'admin',
    firstReplyAt: at(1, 50),
    lastPreview: '已收到，正在处理',
    slaPausedAt: at(1, 50),
    tags: ['vip', '售后'],
  }),
  mkTicket(2, { priority: 'urgent', overdue: true, lastPreview: '急：商品破损', groupId: '902', groupName: '退款组', tags: ['refund'] }),
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

const attachments: Record<string, TicketAttachment[]> = {
  TK743000000000000001: [
    {
      id: 'a1',
      token: 'att-public-1',
      uploaderType: 'reporter',
      uploaderName: '微信用户 8127',
      internal: false,
      name: 'damage.jpg',
      mime: 'image/jpeg',
      size: 182734,
      createdAt: at(1, 15),
    },
    {
      id: 'a2',
      token: 'att-internal-1',
      uploaderType: 'agent',
      uploaderName: 'admin',
      internal: true,
      name: 'warehouse.txt',
      mime: 'text/plain',
      size: 128,
      createdAt: at(1, 55),
    },
  ],
}

// 动态轨迹：与后端同一套动作码。变更类接口往里追加，时间轴才有东西可渲染。
const activities: TicketActivity[] = tickets.map((tk, i) => ({
  id: `act-created-${i}`,
  ticketNo: tk.ticketNo,
  actorType: tk.creatorName ? 'agent' : 'reporter',
  actorName: tk.creatorName || tk.reporterName || 'visitor',
  action: 'created',
  fromValue: '',
  toValue: '',
  detail: '',
  createdAt: tk.createdAt,
}))

const pushActivity = (tk: Ticket, patch: Partial<TicketActivity> & { action: TicketActivity['action'] }) => {
  activities.push({
    id: `act-${Date.now()}-${activities.length}`,
    ticketNo: tk.ticketNo,
    actorType: 'agent',
    actorName: 'admin',
    fromValue: '',
    toValue: '',
    detail: '',
    createdAt: new Date().toISOString(),
    ...patch,
  })
}

const sla: SLASetting = {
  workdays: [1, 2, 3, 4, 5, 6, 7],
  startMinute: 0,
  endMinute: 1440,
  targets: {
    urgent: { firstMinutes: 120, resolutionMinutes: 480 },
    high: { firstMinutes: 480, resolutionMinutes: 1440 },
    normal: { firstMinutes: 1440, resolutionMinutes: 4320 },
    low: { firstMinutes: 4320, resolutionMinutes: 10080 },
  },
}

const groups: TicketGroup[] = [
  {
    id: '901',
    name: '售后一组',
    description: '白天班',
    autoAssign: true,
    enabled: true,
    sort: 0,
    members: [
      { groupId: '901', userId: '7', userName: 'admin' },
      { groupId: '901', userId: '8', userName: '李工' },
    ],
  },
  { id: '902', name: '退款组', description: '', autoAssign: false, enabled: true, sort: 1, members: [] },
]

const rules: TicketRule[] = [
  {
    id: '701',
    name: '紧急单进退款组',
    enabled: true,
    sort: 0,
    matchPriority: 'urgent',
    matchBizType: '',
    matchKeyword: '',
    setPriority: '',
    setGroupId: '902',
    note: '',
  },
]

const find = (no: string) => tickets.find((x) => x.ticketNo === no)
// 规则只能指向启用的分组：与后端 checkRuleGroup 一致，停用或不存在的组都拒绝写入
const assertRuleGroup = (gid: string) => {
  if (!gid || gid === '0') return
  const group = groups.find((g) => g.id === gid)
  if (!group || !group.enabled) throw new Error('group unavailable')
}
const nextId = () => String(7432000000000000000n + BigInt(Date.now() % 1_000_000))

// 列表与导出共用同一套筛选，mock 里也要保持「看到的 = 导出的」。
function mockFilterTickets(raw: Record<string, unknown>): Ticket[] {
  const q = (k: string) => String(raw[k] ?? '')
  const kw = q('keyword').trim().toLowerCase()
  let rows = tickets.filter(
    (tk) => !kw || tk.ticketNo.toLowerCase().includes(kw) || tk.title.toLowerCase().includes(kw) || tk.lastPreview.toLowerCase().includes(kw),
  )
  if (q('status')) rows = rows.filter((tk) => tk.status === q('status'))
  if (q('priority')) rows = rows.filter((tk) => tk.priority === q('priority'))
  if (q('bizType')) rows = rows.filter((tk) => tk.bizType === q('bizType'))
  if (q('overdue') === '1') rows = rows.filter((tk) => tk.overdue)
  if (q('unassigned') === '1') rows = rows.filter((tk) => !tk.assigneeId || tk.assigneeId === '0')
  else if (q('mine') === '1') rows = rows.filter((tk) => tk.assigneeId === '7')
  else if (q('assigneeId')) rows = rows.filter((tk) => tk.assigneeId === q('assigneeId'))
  if (q('groupId')) rows = rows.filter((tk) => tk.groupId === q('groupId'))
  if (q('tag')) rows = rows.filter((tk) => (tk.tags ?? []).includes(q('tag')))
  return rows
}

function csvCell(v: unknown): string {
  const s = v === null || v === undefined ? '' : String(v)
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/ticket/tickets',
    handler: ({ query }) => {
      const rows = mockFilterTickets(query)
      const page = Number(query.page ?? 1) || 1
      const pageSize = Number(query.pageSize ?? 20) || 20
      const start = (page - 1) * pageSize
      return { list: rows.slice(start, start + pageSize), total: rows.length, page, pageSize }
    },
  },
  {
    // 真后端返回 xlsx；mock 没有二进制，退回同样筛选的 CSV 文本。
    method: 'GET',
    url: '/plugins/ticket/tickets-export',
    handler: ({ query }) => {
      const head = ['工单号', '标题', '状态', '优先级', '分组', '处理人', '报告人', '创建人', '业务对象', '标签',
        '满意度', '响应超时', '解决超时', '提交时间', '首次响应', '解决时间', '关闭时间', '最后回复']
      const lines = [head.join(',')].concat(
        mockFilterTickets(query).map((tk) =>
          [
            tk.ticketNo, tk.title, tk.status, tk.priority, tk.groupName ?? '', tk.assigneeName,
            tk.reporterName || tk.reporterId, tk.creatorName ?? '',
            tk.bizType && tk.bizNo ? `${tk.bizType}: ${tk.bizNo}` : `${tk.bizType}${tk.bizNo}`,
            (tk.tags ?? []).join(','), tk.rating ?? '', tk.overdue ? 'TRUE' : 'FALSE',
            tk.resolutionOverdue ? 'TRUE' : 'FALSE',
            tk.createdAt, tk.firstReplyAt ?? '', tk.resolvedAt ?? '', tk.closedAt ?? '', tk.lastReplyAt ?? '',
          ].map(csvCell).join(','),
        ),
      )
      return lines.join('\r\n')
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
      // 自动化规则先跑：改优先级/转组/加内部备注
      const notes: string[] = []
      rules
        .filter((r) => r.enabled)
        .forEach((r) => {
          const hit =
            (!r.matchPriority || r.matchPriority === tk.priority) &&
            (!r.matchBizType || r.matchBizType === tk.bizType) &&
            (!r.matchKeyword || tk.title.toLowerCase().includes(r.matchKeyword.toLowerCase()))
          if (!hit) return
          if (r.setPriority) tk.priority = r.setPriority as Ticket['priority']
          if (r.setGroupId && r.setGroupId !== '0') {
            const group = groups.find((g) => g.id === r.setGroupId)
            if (group && group.enabled) {
              tk.groupId = group.id
              tk.groupName = group.name
              if (group.autoAssign && (!tk.assigneeId || tk.assigneeId === '0')) {
                const member = group.members?.[0]
                if (member) {
                  tk.assigneeId = member.userId
                  tk.assigneeName = member.userName
                }
              }
            }
          }
          if (r.note) notes.push(r.note)
          pushActivity(tk, {
            action: 'rule_applied',
            actorType: 'system',
            actorName: '自动化规则',
            toValue: r.name,
            createdAt: tk.createdAt,
          })
        })
      // 规则没分到组时，再走默认自动分派组
      if (!tk.groupId) {
        const auto = groups.find((g) => g.enabled && g.autoAssign)
        if (auto) {
          tk.groupId = auto.id
          tk.groupName = auto.name
          const member = auto.members?.[0]
          if (member) {
            tk.assigneeId = member.userId
            tk.assigneeName = member.userName
          }
        }
      }
      tickets.unshift(tk)
      pushActivity(tk, { action: 'created', createdAt: tk.createdAt })
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
      notes.forEach((note, idx) => {
        replies[tk.ticketNo] = [
          ...(replies[tk.ticketNo] ?? []),
          {
            id: nextId(),
            seq: 2 + idx,
            replierType: 'system',
            replierId: '',
            replierName: 'rule',
            content: note,
            internal: true,
            createdAt: new Date().toISOString(),
          },
        ]
      })
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
          const from = tk.assigneeName
          tk.assigneeId = String(body.assigneeId ?? '7')
          tk.assigneeName = String(body.assigneeName ?? 'admin')
          if (from !== tk.assigneeName) pushActivity(tk, { action: 'assignee_changed', fromValue: from, toValue: tk.assigneeName })
        } else if (action === 'status') {
          const from = tk.status
          tk.status = String(body.status ?? tk.status) as Ticket['status']
          if (from !== tk.status) pushActivity(tk, { action: 'status_changed', fromValue: from, toValue: tk.status })
          if (tk.status === 'resolved') tk.resolvedAt = new Date().toISOString()
          if (tk.status === 'closed') tk.closedAt = new Date().toISOString()
        } else if (action === 'priority') {
          const from = tk.priority
          tk.priority = String(body.priority ?? tk.priority) as Ticket['priority']
          if (from !== tk.priority) pushActivity(tk, { action: 'priority_changed', fromValue: from, toValue: tk.priority })
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
      return {
        ticket: tk,
        replies: replies[params.no] ?? [],
        attachments: attachments[params.no] ?? [],
        activities: activities.filter((a) => a.ticketNo === params.no),
      }
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/attachments',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      const row: TicketAttachment = {
        id: nextId(),
        token: `mock-att-${Date.now()}`,
        uploaderType: 'agent',
        uploaderName: 'admin',
        internal: body?.internal === '1' || body?.internal === true,
        name: String(body?.fileName ?? body?.name ?? 'attachment.png'),
        mime: 'image/png',
        size: Number(body?.size ?? 1024),
        createdAt: new Date().toISOString(),
      }
      attachments[params.no] = [...(attachments[params.no] ?? []), row]
      pushActivity(tk, { action: 'attachment_added', detail: row.name })
      return row
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
        const from = tk.status
        tk.lastPreview = r.content
        tk.lastReplierType = 'agent'
        if (tk.status === 'resolved' || tk.status === 'closed') {
          tk.rating = null
          tk.ratingComment = ''
          tk.ratedAt = null
        }
        tk.status = 'pending'
        tk.slaPausedAt = r.createdAt
        pushActivity(tk, { action: 'status_changed', fromValue: from, toValue: 'pending' })
        if (tk.assigneeId === '0') {
          tk.assigneeId = '7'
          tk.assigneeName = 'admin'
          pushActivity(tk, { action: 'assignee_changed', fromValue: '', toValue: 'admin' })
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
      const from = tk.status
      tk.status = String(body.status ?? 'open') as Ticket['status']
      if (from !== tk.status) pushActivity(tk, { action: 'status_changed', fromValue: from, toValue: tk.status })
      if (tk.status === 'pending') tk.slaPausedAt = new Date().toISOString()
      if (tk.status === 'processing') {
        tk.slaPausedAt = null
        tk.resolvedAt = null
        tk.closedAt = null
        tk.rating = null
        tk.ratingComment = ''
        tk.ratedAt = null
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
      const from = tk.assigneeName
      const id = String(body.assigneeId ?? '7')
      tk.assigneeId = id === '0' ? '7' : id
      tk.assigneeName = String(body.assigneeName ?? '') || (tk.assigneeId === '7' ? 'admin' : `用户 ${tk.assigneeId}`)
      if (from !== tk.assigneeName) pushActivity(tk, { action: 'assignee_changed', fromValue: from, toValue: tk.assigneeName })
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
      pushActivity(tk, {
        action: 'rating_added',
        actorType: 'reporter',
        actorName: tk.reporterName || 'visitor',
        toValue: String(score),
        detail: tk.ratingComment,
      })
      return tk
    },
  },
  {
    method: 'GET',
    url: '/plugins/ticket/canned',
    handler: () => canned,
  },
  {
    method: 'GET',
    url: '/plugins/ticket/tags',
    handler: () => {
      const counts = new Map<string, number>()
      tickets.forEach((tk) => (tk.tags ?? []).forEach((tag) => counts.set(tag, (counts.get(tag) ?? 0) + 1)))
      return [...counts.entries()]
        .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
        .map(([name]) => name)
    },
  },
  {
    method: 'PUT',
    url: '/plugins/ticket/tickets/:no/tags',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      const tags: string[] = []
      const seen = new Set<string>()
      if (Array.isArray(body.tags)) {
        for (const raw of body.tags as unknown[]) {
          const tag = String(raw).trim()
          if (tag && !seen.has(tag)) {
            seen.add(tag)
            tags.push(tag)
          }
        }
      }
      const from = (tk.tags ?? []).join(',')
      tk.tags = tags.slice(0, 10)
      if (from !== tk.tags.join(',')) pushActivity(tk, { action: 'tags_changed', fromValue: from, toValue: tk.tags.join(',') })
      return tk
    },
  },
  {
    method: 'GET',
    url: '/plugins/ticket/rules',
    handler: () => rules,
  },
  {
    method: 'GET',
    url: '/plugins/ticket/biz-types',
    handler: () =>
      [...new Set([...tickets.map((tk) => tk.bizType), ...rules.map((r) => r.matchBizType)])]
        .filter((v) => v)
        .sort(),
  },
  {
    method: 'POST',
    url: '/plugins/ticket/rules',
    handler: ({ body }) => {
      const setGroupId = String(body.setGroupId ?? '0')
      assertRuleGroup(setGroupId)
      const row: TicketRule = {
        id: nextId(),
        name: String(body.name ?? ''),
        enabled: body.enabled !== false,
        sort: Number(body.sort ?? rules.length),
        matchPriority: String(body.matchPriority ?? ''),
        matchBizType: String(body.matchBizType ?? ''),
        matchKeyword: String(body.matchKeyword ?? ''),
        setPriority: String(body.setPriority ?? ''),
        setGroupId,
        note: String(body.note ?? ''),
      }
      rules.push(row)
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/ticket/rules/:id',
    handler: ({ params, body }) => {
      const row = rules.find((r) => r.id === params.id)
      if (!row) throw new Error('not found')
      const setGroupId = String(body.setGroupId ?? '0')
      assertRuleGroup(setGroupId)
      row.name = String(body.name ?? row.name)
      row.enabled = body.enabled !== false
      row.sort = Number(body.sort ?? row.sort)
      row.matchPriority = String(body.matchPriority ?? '')
      row.matchBizType = String(body.matchBizType ?? '')
      row.matchKeyword = String(body.matchKeyword ?? '')
      row.setPriority = String(body.setPriority ?? '')
      row.setGroupId = setGroupId
      row.note = String(body.note ?? '')
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/ticket/rules/:id',
    handler: ({ params }) => {
      const idx = rules.findIndex((r) => r.id === params.id)
      if (idx >= 0) rules.splice(idx, 1)
      return { id: params.id }
    },
  },
  {
    method: 'GET',
    url: '/plugins/ticket/groups',
    handler: () => groups,
  },
  {
    method: 'POST',
    url: '/plugins/ticket/groups',
    handler: ({ body }) => {
      const row: TicketGroup = {
        id: nextId(),
        name: String(body.name ?? ''),
        description: String(body.description ?? ''),
        autoAssign: body.autoAssign === true,
        enabled: body.enabled !== false,
        sort: Number(body.sort ?? groups.length),
        members: [],
      }
      groups.push(row)
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/ticket/groups/:id',
    handler: ({ params, body }) => {
      const row = groups.find((g) => g.id === params.id)
      if (!row) throw new Error('not found')
      row.name = String(body.name ?? row.name)
      row.description = String(body.description ?? row.description)
      row.autoAssign = body.autoAssign === true
      row.enabled = body.enabled !== false
      row.sort = Number(body.sort ?? row.sort)
      // 改名同步工单上的冗余组名（与后端一致）
      tickets.forEach((tk) => {
        if (tk.groupId === row.id) tk.groupName = row.name
      })
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/ticket/groups/:id',
    handler: ({ params }) => {
      const idx = groups.findIndex((g) => g.id === params.id)
      if (idx >= 0) groups.splice(idx, 1)
      tickets.forEach((tk) => {
        if (tk.groupId === params.id) {
          tk.groupId = ''
          tk.groupName = ''
        }
      })
      // 规则里对该组的引用一并清掉；只剩转组这一个动作的规则顺带停用（与后端一致）
      rules.forEach((r) => {
        if (r.setGroupId !== params.id) return
        r.setGroupId = '0'
        if (!r.setPriority && !r.note) r.enabled = false
      })
      return { id: params.id }
    },
  },
  {
    method: 'PUT',
    url: '/plugins/ticket/groups/:id/members',
    handler: ({ params, body }) => {
      const row = groups.find((g) => g.id === params.id)
      if (!row) throw new Error('not found')
      const ids: string[] = Array.isArray(body.userIds) ? body.userIds.map(String) : []
      row.members = ids.map((uid) => ({
        groupId: row.id,
        userId: uid,
        userName: uid === '7' ? 'admin' : `用户 ${uid}`,
      }))
      return row
    },
  },
  {
    method: 'POST',
    url: '/plugins/ticket/tickets/:no/group',
    handler: ({ params, body }) => {
      const tk = find(params.no)
      if (!tk) throw new Error('not found')
      const gid = String(body.groupId ?? '0')
      const group = groups.find((g) => g.id === gid)
      if (group && !group.enabled) throw new Error('group disabled')
      const fromGroup = tk.groupName
      const fromAssignee = tk.assigneeName
      tk.groupId = group ? group.id : ''
      tk.groupName = group ? group.name : ''
      if (fromGroup !== tk.groupName) pushActivity(tk, { action: 'group_changed', fromValue: fromGroup, toValue: tk.groupName })
      if (group?.autoAssign && (!tk.assigneeId || tk.assigneeId === '0')) {
        const member = group.members?.[0]
        if (member) {
          tk.assigneeId = member.userId
          tk.assigneeName = member.userName
        }
      }
      if (fromAssignee !== tk.assigneeName) {
        pushActivity(tk, {
          action: 'assignee_changed',
          actorType: 'system',
          actorName: '自动分派',
          fromValue: fromAssignee,
          toValue: tk.assigneeName,
        })
      }
      return tk
    },
  },
  {
    method: 'GET',
    url: '/plugins/ticket/sla',
    handler: () => sla,
  },
  {
    method: 'PUT',
    url: '/plugins/ticket/sla',
    handler: ({ body }) => {
      sla.workdays = Array.isArray(body.workdays) ? [...body.workdays] : sla.workdays
      sla.startMinute = Number(body.startMinute ?? sla.startMinute)
      sla.endMinute = Number(body.endMinute ?? sla.endMinute)
      sla.targets = body.targets ?? sla.targets
      return sla
    },
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
            targetMinutes: sla.targets[priority]?.firstMinutes ?? slaMin[priority] ?? slaMin.normal,
            total: v.total,
            breached: v.breached,
            breachRate: v.total ? Math.round((v.breached / v.total) * 1000) / 10 : 0,
            resolutionTargetMinutes: sla.targets[priority]?.resolutionMinutes ?? 1440,
            resolutionBreached: 0,
            resolutionBreachRate: 0,
          })),
      }
    },
  },
])
