import { defineMock } from '@/framework/request/mock'
import { localDate } from './format'
import type { OperLog } from './api'

const MIN = 60000
const mkId = (n: number) => (1830000000000000000n + BigInt(n)).toString()

type Row = [string, string, string, string, string, 'success' | 'failed', number, string, string, number, string?]

const rows: Row[] = [
  ['用户管理', 'create', 'admin', '10.20.1.15', '广东省深圳市', 'success', 86, 'POST', '/api/v1/users', 4],
  ['角色管理', 'grant', 'admin', '10.20.1.15', '广东省深圳市', 'success', 132, 'PUT', '/api/v1/roles/1830000000000000101/menus', 18],
  ['菜单管理', 'sort', 'zhang.wei', '10.20.3.42', '北京市', 'success', 54, 'PUT', '/api/v1/menus/sort', 35],
  ['部门管理', 'update', 'li.na', '192.168.8.21', '上海市', 'success', 71, 'PUT', '/api/v1/depts/1830000000000000203', 62],
  ['用户管理', 'export', 'admin', '10.20.1.15', '广东省深圳市', 'success', 640, 'GET', '/api/v1/users/export', 95],
  ['字典管理', 'update', 'wang.fang', '172.16.5.9', '浙江省杭州市', 'failed', 312, 'PUT', '/api/v1/dict/data/1830000000000000304', 140,
    'java.lang.IllegalStateException: duplicate dict value "1"\n  at dict.Service.Update(service.go:128)\n  at dict.Handler.UpdateData(handler.go:64)\n  at gin.(*Context).Next(context.go:174)'],
  ['参数配置', 'update', 'admin', '10.20.1.15', '广东省深圳市', 'success', 48, 'PUT', '/api/v1/configs/1830000000000000402', 190],
  ['通知公告', 'create', 'zhao.lei', '192.168.8.37', '江苏省南京市', 'success', 163, 'POST', '/api/v1/notices', 260],
  ['文件管理', 'delete', 'li.na', '192.168.8.21', '上海市', 'success', 97, 'DELETE', '/api/v1/files/1830000000000000511', 330],
  ['租户管理', 'create', 'admin', '10.20.1.15', '广东省深圳市', 'failed', 405, 'POST', '/api/v1/tenants', 420,
    'ValidationError: field "domain" must be unique\n  at tenant.Service.Create(service.go:77)\n  at tenant.Handler.Create(handler.go:41)'],
  ['岗位管理', 'sort', 'zhang.wei', '10.20.3.42', '北京市', 'success', 39, 'PUT', '/api/v1/posts/sort', 515],
  ['用户管理', 'update', 'sun.yue', '10.20.7.8', '四川省成都市', 'success', 91, 'PUT', '/api/v1/users/1830000000000000601', 640],
  ['用户管理', 'other', 'admin', '10.20.1.15', '广东省深圳市', 'success', 118, 'POST', '/api/v1/users/1830000000000000601/reset-password', 780],
  ['角色管理', 'delete', 'wang.fang', '172.16.5.9', '浙江省杭州市', 'failed', 226, 'DELETE', '/api/v1/roles/1830000000000000102', 905,
    'ConflictError: role is still assigned to 3 users\n  at role.Service.Delete(service.go:203)\n  at role.Handler.Delete(handler.go:88)'],
  ['菜单管理', 'create', 'admin', '10.20.1.15', '广东省深圳市', 'success', 77, 'POST', '/api/v1/menus', 1100],
  ['部门管理', 'create', 'li.na', '192.168.8.21', '上海市', 'success', 66, 'POST', '/api/v1/depts', 1320],
  ['字典管理', 'import', 'zhao.lei', '192.168.8.37', '江苏省南京市', 'success', 1480, 'POST', '/api/v1/dict/data/import', 1600],
  ['通知公告', 'update', 'zhao.lei', '192.168.8.37', '江苏省南京市', 'success', 109, 'PUT', '/api/v1/notices/1830000000000000702', 1900],
  ['参数配置', 'update', 'admin', '10.20.1.15', '广东省深圳市', 'success', 52, 'PUT', '/api/v1/configs/1830000000000000403', 2300],
  ['租户管理', 'update', 'admin', '10.20.1.15', '广东省深圳市', 'success', 121, 'PUT', '/api/v1/tenants/1830000000000000801', 2900],
  ['用户管理', 'delete', 'sun.yue', '10.20.7.8', '四川省成都市', 'failed', 18, 'DELETE', '/api/v1/users?ids=1830000000000000601', 3600,
    'ForbiddenError: cannot delete the built-in administrator\n  at user.Service.Delete(service.go:156)\n  at user.Handler.DeleteBatch(handler.go:112)'],
  ['文件管理', 'create', 'li.na', '192.168.8.21', '上海市', 'success', 2210, 'POST', '/api/v1/files/upload', 4500],
  ['岗位管理', 'create', 'wang.fang', '172.16.5.9', '浙江省杭州市', 'success', 58, 'POST', '/api/v1/posts', 6200],
  ['角色管理', 'create', 'admin', '10.20.1.15', '广东省深圳市', 'success', 84, 'POST', '/api/v1/roles', 8800],
  ['用户管理', 'export', 'zhang.wei', '10.20.3.42', '北京市', 'success', 702, 'GET', '/api/v1/users/export', 12500],
  ['部门管理', 'delete', 'admin', '10.20.1.15', '广东省深圳市', 'success', 63, 'DELETE', '/api/v1/depts/1830000000000000204', 20000],
]

function toLog(r: Row, i: number): OperLog {
  const [module, type, operator, ip, location, status, costMs, method, url, ago, errorStack] = r
  const params =
    method === 'GET'
      ? { keyword: '', page: 1, pageSize: 10 }
      : type === 'delete'
        ? { ids: [mkId(500 + i)] }
        : type === 'sort'
          ? { parentId: null, ids: [mkId(600 + i), mkId(601 + i), mkId(602 + i)] }
          : { name: `${module}-${i + 1}`, status: '1', remark: 'demo' }
  return {
    id: mkId(i + 1),
    module,
    type,
    operator,
    ip,
    location,
    status,
    costMs,
    method,
    url,
    params,
    response: status === 'success' ? { code: 0, message: 'ok', data: null } : { code: 500, message: errorStack?.split('\n')[0] ?? 'error' },
    errorStack,
    createdAt: new Date(Date.now() - ago * MIN).toISOString(),
  }
}

let items: OperLog[] = rows.map(toLog)

function filtered(query: Record<string, unknown>): OperLog[] {
  const kw = String(query.keyword ?? '').trim().toLowerCase()
  let list = items.slice()
  if (kw) list = list.filter((r) => [r.module, r.operator, r.url].some((s) => s.toLowerCase().includes(kw)))
  if (query.status) list = list.filter((r) => r.status === query.status)
  if (query.type) list = list.filter((r) => r.type === query.type)
  if (query.startDate) list = list.filter((r) => localDate(r.createdAt) >= String(query.startDate))
  if (query.endDate) list = list.filter((r) => localDate(r.createdAt) <= String(query.endDate))
  list.sort((a, b) => b.createdAt.localeCompare(a.createdAt))
  return list
}

/** 与后端 stats 同口径的 mock 侧聚合（仅开发态使用；分位数取最近秩） */
const pctOf = (xs: number[], q: number) => {
  if (!xs.length) return 0
  const s = [...xs].sort((a, b) => a - b)
  return s[Math.min(s.length - 1, Math.max(0, Math.ceil(s.length * q) - 1))]
}
const avgOf = (xs: number[]) => (xs.length ? Math.round(xs.reduce((a, b) => a + b, 0) / xs.length) : 0)
const tally = (keys: string[]) => {
  const m = new Map<string, number>()
  keys.forEach((k) => m.set(k || '-', (m.get(k) ?? 0) + 1))
  return [...m].map(([name, value]) => ({ name, value }))
    .sort((a, b) => b.value - a.value || a.name.localeCompare(b.name))
    .slice(0, 8)
}
const hourBuckets = (stamps: string[]) => {
  const hours = Array.from({ length: 24 }, (_, hour) => ({ hour, value: 0 }))
  stamps.forEach((s) => { hours[new Date(s).getHours()].value += 1 })
  return hours
}
const dayBuckets = (stamps: string[], failed: (s: string) => boolean) => {
  const m = new Map<string, { date: string; total: number; failures: number }>()
  stamps.forEach((s) => {
    const key = localDate(s)
    const cur = m.get(key) ?? { date: key, total: 0, failures: 0 }
    cur.total += 1
    if (failed(s)) cur.failures += 1
    m.set(key, cur)
  })
  return [...m.values()].sort((a, b) => a.date.localeCompare(b.date))
}

const csvCell = (v: string) => (/[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)

defineMock([
  {
    method: 'GET',
    url: '/oper-logs',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const list = filtered(query)
      return { list: list.slice((page - 1) * pageSize, page * pageSize), total: list.length, page, pageSize }
    },
  },
  {
    // 与真实 /oper-logs/stats 同构：分桶与分位在服务端算，mock 也照同样口径复现一遍
    method: 'GET',
    url: '/oper-logs/stats',
    handler: ({ query }) => {
      const list = filtered(query)
      const stamps = list.map((r) => r.createdAt)
      const costs = list.map((r) => r.costMs)
      const byUrl = new Map<string, { url: string; method: string; sum: number; count: number }>()
      list.forEach((r) => {
        const cur = byUrl.get(r.url) ?? { url: r.url, method: r.method, sum: 0, count: 0 }
        cur.sum += r.costMs
        cur.count += 1
        byUrl.set(r.url, cur)
      })
      return {
        from: String(query.startDate ?? ''),
        to: String(query.endDate ?? ''),
        total: list.length,
        failures: list.filter((r) => r.status !== 'success').length,
        sampled: false,
        days: dayBuckets(stamps, (s) => list.some((r) => r.createdAt === s && r.status !== 'success')),
        hours: hourBuckets(stamps),
        topModules: tally(list.map((r) => r.module)),
        topTypes: tally(list.map((r) => r.type)),
        methods: tally(list.map((r) => r.method)),
        topOperators: tally(list.map((r) => r.operator)),
        latency: { avg: avgOf(costs), p50: pctOf(costs, 0.5), p95: pctOf(costs, 0.95), p99: pctOf(costs, 0.99), max: costs.length ? Math.max(...costs) : 0 },
        slowest: [...byUrl.values()]
          .map((u) => ({ url: u.url, method: u.method, avgMs: Math.round(u.sum / u.count), count: u.count }))
          .sort((a, b) => b.avgMs - a.avgMs || a.url.localeCompare(b.url))
          .slice(0, 8),
      }
    },
  },
  {
    // 真实后端返回 xlsx 文件（列为服务端白名单，不含请求参数 / 响应 / 异常）；mock 返回同样列的 CSV 文本
    method: 'GET',
    url: '/oper-logs/export',
    handler: ({ query }) => {
      const head = ['模块', '操作类型', '操作人', 'IP', '归属地', '状态', '耗时(ms)', '请求方法', '请求地址', '时间']
      const lines = filtered(query).map((r) =>
        [r.module, r.type, r.operator, r.ip, r.location ?? '', r.status === 'success' ? '成功' : '失败', String(r.costMs), r.method, r.url, r.createdAt]
          .map(csvCell)
          .join(','),
      )
      return `\uFEFF${[head.join(','), ...lines].join('\r\n')}\r\n`
    },
  },
  {
    method: 'DELETE',
    url: '/oper-logs',
    handler: ({ query }) => {
      const ids = String(query.ids ?? '').split(',').filter(Boolean)
      items = items.filter((r) => !ids.includes(r.id))
      return null
    },
  },
  {
    method: 'POST',
    url: '/oper-logs/clean',
    handler: ({ body }) => {
      const days = Number(body?.days) || 0
      const before = items.length
      const limit = Date.now() - days * 24 * 60 * MIN
      items = days === 0 ? [] : items.filter((r) => new Date(r.createdAt).getTime() >= limit)
      return { removed: before - items.length }
    },
  },
])
