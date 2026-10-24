import { defineMock, mockError } from '@/framework/request/mock'

const day = (offset: number) => {
  const d = new Date()
  d.setDate(d.getDate() - offset)
  return d
}
const ymd = (d: Date) => `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
const ago = (hours: number) => new Date(Date.now() - hours * 3600_000).toISOString()

const logins = [312, 286, 341, 398, 367, 254, 421]
const failures = [12, 9, 17, 21, 14, 8, 19]
const requests = [18240, 17510, 20190, 22830, 21470, 15360, 24980]
const errors = [96, 81, 143, 178, 120, 64, 152]

// 工作台布局：内存存储，刷新页面后重置；校验规则与后端 PUT /dashboard/layout 一致
let layout: { order: string[]; hidden: string[] } = { order: [], hidden: [] }
const ID_RE = /^[a-z0-9][a-z0-9\-_.]*$/
const checkIds = (v: unknown): string[] => {
  if (v == null) return []
  if (!Array.isArray(v) || v.length > 100) mockError(400, 'Invalid layout')
  const seen = new Set<string>()
  for (const id of v) {
    if (typeof id !== 'string' || id.length > 64 || !ID_RE.test(id) || seen.has(id)) mockError(400, 'Invalid layout')
    seen.add(id)
  }
  return v as string[]
}

defineMock([
  { method: 'GET', url: '/dashboard/layout', handler: () => layout },
  {
    method: 'PUT',
    url: '/dashboard/layout',
    handler: ({ body }) => {
      layout = { order: checkIds(body?.order), hidden: checkIds(body?.hidden) }
      return layout
    },
  },
  {
    method: 'DELETE',
    url: '/dashboard/layout',
    handler: () => {
      layout = { order: [], hidden: [] }
      return null
    },
  },
  {
    method: 'GET',
    url: '/dashboard/stats',
    handler: () => ({
      users: { value: 12846, delta: 4.2 },
      todayLogin: { value: 421, delta: 12.8 },
      unreadMessages: { value: 7, delta: -18.5 },
      storage: { usedGb: 183.6, totalGb: 500, percent: 36.7, delta: 2.1 },
      loginTrend: Array.from({ length: 7 }, (_, i) => ({ date: ymd(day(6 - i)), logins: logins[i], failures: failures[i] })),
      requestTrend: Array.from({ length: 7 }, (_, i) => ({ date: ymd(day(6 - i)), requests: requests[i], errors: errors[i] })),
      operTypes: [
        { type: 'query', value: 5230 },
        { type: 'update', value: 1480 },
        { type: 'insert', value: 960 },
        { type: 'delete', value: 310 },
        { type: 'export', value: 215 },
      ],
      todos: [
        { id: '7301000000000000001', kind: 'message', title: '待审批：李娜提交了角色权限变更申请', time: ago(1) },
        { id: '7301000000000000002', kind: 'message', title: 'Storage usage exceeded 35% of quota', time: ago(5) },
        { id: '7301000000000000003', kind: 'notice', title: '请在本周五前确认季度合规自查结果', time: ago(20) },
        { id: '7301000000000000004', kind: 'message', title: 'Michael 申请重置登录密码', time: ago(30) },
        { id: '7301000000000000005', kind: 'notice', title: 'Please review the updated data retention policy', time: ago(48) },
      ],
      recentNotices: [
        { id: '7302000000000000001', title: '系统将于本周六凌晨 2:00 进行例行维护', type: 'system', level: 'important', publishAt: ago(3) },
        { id: '7302000000000000002', title: 'NexAdmin 2.4 版本发布：新增文件分组与批量操作', type: 'update', level: 'normal', publishAt: ago(26) },
        { id: '7302000000000000003', title: '国庆假期值班安排通知', type: 'notice', level: 'normal', publishAt: ago(72) },
        { id: '7302000000000000004', title: 'Security reminder: enable two-factor authentication', type: 'security', level: 'important', publishAt: ago(120) },
      ],
    }),
  },
])
