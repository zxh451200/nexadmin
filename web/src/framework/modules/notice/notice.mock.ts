import { defineMock, genId, mockError } from '@/framework/request/mock'
import { moveToRecycle, registerRestorer } from '../recycle/recycle.mock'
import type { Notice, NoticeForm, NoticeReadStats } from './api'

const nid = (n: number) => String(7412000000000000000n + BigInt(n))
const ago = (h: number) => new Date(Date.now() - h * 3600_000).toISOString()
const later = (h: number) => new Date(Date.now() + h * 3600_000).toISOString()

const base = (n: number, p: Partial<Notice> & Pick<Notice, 'title' | 'type' | 'level' | 'status' | 'content'>): Notice => ({
  id: nid(n),
  scope: 'all',
  targetIds: [],
  pinned: false,
  popup: false,
  platform: false,
  publishAt: null,
  expireAt: null,
  publishedAt: null,
  attachments: [],
  readCount: 0,
  totalCount: 0,
  createdAt: ago(24 * (30 - n)),
  updatedAt: ago(24 * (30 - n)),
  ...p,
})

let items: Notice[] = [
  base(1, {
    title: '系统升级维护通知 / Scheduled maintenance',
    type: 'notice',
    level: 'important',
    status: 'published',
    content: '<p>本周六 <strong>22:00 - 24:00</strong> 将进行系统升级，期间服务可能短暂不可用，请提前保存工作。</p><ul><li>影响范围：全部模块</li><li>预计时长：2 小时</li></ul>',
    pinned: true,
    popup: true,
    platform: true,
    publishedAt: ago(30),
    totalCount: 128,
    readCount: 71,
  }),
  base(2, {
    title: '新版本发布 / New release available',
    type: 'announcement',
    level: 'important',
    status: 'published',
    content: '<p>NexAdmin 新版本已发布,新增回收站、接口监控和登录锁定,欢迎体验。</p>',
    popup: false,
    pinned: true,
    publishedAt: ago(52),
    totalCount: 128,
    readCount: 96,
  }),
  base(3, {
    title: '2026 年度团建活动报名 / Team building sign-up',
    type: 'activity',
    level: 'normal',
    status: 'published',
    content: '<p>年度团建将于 11 月举行，欢迎在 OA 中报名。<em>Sign up before Nov 1.</em></p>',
    publishedAt: ago(100),
    expireAt: later(24 * 14),
    totalCount: 128,
    readCount: 44,
  }),
  base(4, {
    title: '研发部技术分享周',
    type: 'activity',
    level: 'normal',
    status: 'published',
    scope: 'dept',
    targetIds: ['7300000000000000101'],
    content: '<p>研发部本周五下午举办技术分享，主题：前端性能优化与可观测性。</p>',
    publishedAt: ago(140),
    totalCount: 36,
    readCount: 30,
  }),
  base(5, {
    title: '管理员须知：权限审计',
    type: 'notice',
    level: 'normal',
    status: 'published',
    scope: 'role',
    targetIds: ['7300000000000000011'],
    content: '<p>请各租户管理员在本月底前完成角色权限审计，并在系统中确认。</p>',
    publishedAt: ago(200),
    totalCount: 9,
    readCount: 9,
  }),
  base(6, {
    title: '十一假期安排',
    type: 'announcement',
    level: 'normal',
    status: 'published',
    content: '<p>国庆假期 10 月 1 日至 10 月 7 日，10 月 8 日正常上班。值班安排详见附件。</p>',
    publishedAt: ago(24 * 20),
    expireAt: ago(24 * 5),
    totalCount: 128,
    readCount: 120,
  }),
  base(7, {
    title: '新功能预告：消息中心上线',
    type: 'notice',
    level: 'normal',
    status: 'draft',
    content: '<p>消息中心将支持分类、已读未读与全部已读。</p>',
  }),
  base(8, {
    title: '客户回访计划（草稿）',
    type: 'notice',
    level: 'normal',
    status: 'draft',
    scope: 'user',
    targetIds: ['7300000000000000201', '7300000000000000202'],
    content: '<p>Q4 重点客户回访名单待确认。</p>',
  }),
  base(9, {
    title: '定时发布：年终盘点通知',
    type: 'announcement',
    level: 'important',
    status: 'draft',
    content: '<p>年终盘点将于 12 月开始，请各部门提前准备。</p>',
    publishAt: later(72),
    popup: true,
  }),
  base(10, {
    title: '旧版 API 下线公告',
    type: 'notice',
    level: 'normal',
    status: 'revoked',
    content: '<p>v1 接口计划下线，现已撤回，延期至下季度。</p>',
    publishedAt: ago(24 * 9),
    totalCount: 128,
    readCount: 35,
  }),
  base(11, {
    title: '平台公告：多租户计费规则调整',
    type: 'announcement',
    level: 'important',
    status: 'published',
    platform: true,
    content: '<p>自下月起，套餐计费将按活跃用户数结算，详情请联系平台运营。</p>',
    publishedAt: ago(60),
    totalCount: 560,
    readCount: 212,
  }),
  base(12, {
    title: 'Welcome to NexAdmin',
    type: 'notice',
    level: 'normal',
    status: 'published',
    pinned: false,
    content: '<p>Welcome aboard! Check the <a href="/profile">profile page</a> to finish your setup.</p>',
    publishedAt: ago(24 * 12),
    totalCount: 128,
    readCount: 128,
  }),
]

/** 当前登录用户已读的公告 id（mock 内存态，供 message.mock 共用） */
export const readByMe = new Set<string>([nid(3), nid(4), nid(6), nid(12)])

const NAMES: [string, string, string][] = [
  ['zhangwei', '张伟', '研发部'],
  ['liuna', '刘娜', '市场部'],
  ['wangqiang', '王强', '运维部'],
  ['alice', 'Alice Chen', 'Product'],
  ['chenjing', '陈静', '财务部'],
  ['bob', 'Bob Smith', 'Sales'],
  ['zhaomin', '赵敏', '人事部'],
  ['lixiang', '李想', '研发部'],
  ['sunyu', '孙宇', '客服部'],
  ['yangfan', '杨帆', '研发部'],
  ['emma', 'Emma Wang', 'Design'],
  ['zhoutao', '周涛', '法务部'],
]

/** 对当前用户可见：已发布、已到发布时间、未过期 */
export function isVisible(n: Notice): boolean {
  if (n.status !== 'published') return false
  const now = Date.now()
  if (n.publishAt && new Date(n.publishAt).getTime() > now) return false
  if (n.expireAt && new Date(n.expireAt).getTime() <= now) return false
  return true
}

/** 当前用户「清空我的公告」的时间点（mock 内存态） */
let clearedAt: string | null = null

export const visibleNotices = () =>
  items
    .filter(isVisible)
    .filter((n) => !clearedAt || new Date(n.publishedAt ?? n.createdAt).getTime() > new Date(clearedAt).getTime())
export const unreadNoticeCount = () => visibleNotices().filter((n) => !readByMe.has(n.id)).length

const byTime = (a: Notice, b: Notice) =>
  Number(b.pinned) - Number(a.pinned) ||
  new Date(b.publishedAt ?? b.createdAt).getTime() - new Date(a.publishedAt ?? a.createdAt).getTime()

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

const find = (id: string) => items.find((x) => x.id === id) ?? mockError(404, 'Not found')

defineMock([
  {
    method: 'GET',
    url: '/notices/popup',
    handler: () =>
      visibleNotices()
        .filter((n) => n.level === 'important' && !readByMe.has(n.id))
        .sort(byTime)
        .slice(0, 2)
        .map(({ id, title, content, publishedAt, type, level }) => ({ id, title, content, publishedAt, type, level })),
  },
  {
    method: 'GET',
    url: '/notices/mine',
    handler: ({ query }) => {
      let rows = visibleNotices()
      if (query.unread === '1' || query.unread === 'true') rows = rows.filter((n) => !readByMe.has(n.id))
      if (query.type) rows = rows.filter((n) => n.type === query.type)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      if (kw) rows = rows.filter((n) => n.title.toLowerCase().includes(kw))
      rows = rows.sort(byTime)
      const p = paginate(rows, query)
      return { ...p, list: p.list.map((n) => ({ ...n, read: readByMe.has(n.id) })) }
    },
  },
  {
    method: 'GET',
    url: '/notices',
    handler: ({ query }) => {
      let rows = items.slice()
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      if (kw) rows = rows.filter((n) => n.title.toLowerCase().includes(kw))
      for (const f of ['type', 'level', 'status'] as const) {
        if (query[f]) rows = rows.filter((n) => n[f] === query[f])
      }
      return paginate(rows.sort(byTime), query)
    },
  },
  { method: 'GET', url: '/notices/:id', handler: ({ params }) => find(params.id) },
  {
    method: 'GET',
    url: '/notices/:id/read-stats',
    handler: ({ params }): NoticeReadStats => {
      const n = find(params.id)
      const unread = Math.max(0, n.totalCount - n.readCount)
      const unreadUsers = Array.from({ length: Math.min(unread, 40) }, (_, i) => {
        const [username, nickname, deptName] = NAMES[(i + Number(n.id.slice(-2))) % NAMES.length]
        return { id: String(7300000000000000300n + BigInt(i)), username: `${username}${i >= NAMES.length ? i : ''}`, nickname, deptName }
      })
      return { readCount: n.readCount, unreadCount: unread, unreadUsers }
    },
  },
  {
    method: 'POST',
    url: '/notices',
    handler: ({ body }) => {
      const now = new Date().toISOString()
      const f = body as NoticeForm
      const row: Notice = { ...f, id: genId(), status: 'draft', publishedAt: null, readCount: 0, totalCount: 0, createdAt: now, updatedAt: now }
      items = [row, ...items]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/notices/:id',
    handler: ({ params, body }) => {
      const cur = find(params.id)
      const next: Notice = { ...cur, ...(body as NoticeForm), id: cur.id, updatedAt: new Date().toISOString() }
      items = items.map((x) => (x.id === cur.id ? next : x))
      return next
    },
  },
  {
    method: 'POST',
    url: '/notices/:id/publish',
    handler: ({ params }) => {
      const cur = find(params.id)
      const total = cur.scope === 'all' ? 128 : cur.scope === 'dept' ? 36 : cur.scope === 'role' ? 12 : Math.max(cur.targetIds.length, 1)
      const next: Notice = { ...cur, status: 'published', publishedAt: new Date().toISOString(), totalCount: total, readCount: 0 }
      readByMe.delete(cur.id)
      items = items.map((x) => (x.id === cur.id ? next : x))
      return next
    },
  },
  {
    method: 'POST',
    url: '/notices/:id/revoke',
    handler: ({ params }) => {
      const cur = find(params.id)
      const next: Notice = { ...cur, status: 'revoked' }
      items = items.map((x) => (x.id === cur.id ? next : x))
      return next
    },
  },
  {
    method: 'POST',
    url: '/notices/:id/read',
    handler: ({ params }) => {
      const cur = find(params.id)
      if (!readByMe.has(cur.id)) {
        readByMe.add(cur.id)
        items = items.map((x) => (x.id === cur.id ? { ...x, readCount: Math.min(x.totalCount, x.readCount + 1) } : x))
      }
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/notices/mine',
    handler: () => {
      clearedAt = new Date().toISOString()
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/notices/:id',
    handler: ({ params }) => {
      recycleNotice(params.id)
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/notices',
    handler: ({ query }) => {
      String(query.ids ?? '').split(',').filter(Boolean).forEach(recycleNotice)
      return null
    },
  },
])

function recycleNotice(id: string) {
  const row = items.find((x) => x.id === id)
  if (!row) return
  items = items.filter((x) => x.id !== id)
  moveToRecycle('notice', row.title, row)
}

registerRestorer('notice', (row) => {
  items = [row as Notice, ...items]
})
