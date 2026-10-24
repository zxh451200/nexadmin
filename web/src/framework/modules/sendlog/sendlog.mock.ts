import { defineMock, mockError } from '@/framework/request/mock'
import { localDate } from '../oper-log/format'
import type { SendChannel, SendLog, SendStatus } from './api'

const MIN = 60000
const mkId = (n: number) => (1860000000000000000n + BigInt(n)).toString()

// 渠道、场景、收件人（已脱敏）、标题 / 模板名、状态、失败原因、耗时 ms、多少分钟前
type Row = [SendChannel, string, string, string, SendStatus, string, number, number]

const rows: Row[] = [
  ['sms', 'verify_code', '138****5678', 'login', 'success', '', 420, 2],
  ['email', 'verify_code', 'z***@example.com', 'forgot_code', 'success', '', 860, 9],
  ['sms', 'bind_phone', '139****0011', 'login', 'success', '', 390, 15],
  ['sms', 'verify_code', '186****9024', 'login', 'fail', 'aliyun: isv.BUSINESS_LIMIT_CONTROL, hit the hourly limit for this number', 1210, 28],
  ['robot', 'notice', 'robot:ops', '部署完成', 'success', '', 310, 41],
  ['email', 'notice', 'l***@example.org', '本周周报已生成', 'success', '', 1480, 55],
  ['email', 'account', 'w***@example.com', 'account_created', 'success', '', 920, 70],
  ['robot', 'notice', 'robot:ops', '磁盘使用率超过 90%', 'fail', 'Post "https://oapi.dingtalk.com/<redacted>": dial tcp: i/o timeout', 30005, 95],
  ['email', 'security', 's***@example.com', 'security_alert', 'success', '', 1010, 130],
  ['email', 'notice', '', '新公告：系统维护通知', 'skipped', '3 recipient(s) have no email on profile', 4, 160],
  ['sms', 'notice', '', '订单已发货', 'skipped', 'sms is not configured', 2, 210],
  ['email', 'test', 'a***@example.com', 'SMTP test', 'success', '', 760, 260],
  ['robot', 'test', 'robot:dev', 'Test', 'fail', 'robot "dev" is not configured', 3, 330],
  ['sms', 'verify_code', '137****6620', 'login', 'success', '', 450, 480],
  ['email', 'verify_code', 'h***@example.net', 'forgot_code', 'fail', '535 authentication failed password=[REDACTED]', 2300, 700],
  ['email', 'notice', 'c***@example.com', '审批待处理', 'success', '', 1180, 1500],
  ['sms', 'verify_code', '135****3308', 'login', 'success', '', 410, 2900],
  ['robot', 'notice', 'robot:ops', '备份完成', 'success', '', 295, 4300],
]

let items: SendLog[] = rows.map(([channel, scene, recipient, subject, status, error, durationMs, ago], i) => ({
  id: mkId(i + 1),
  channel,
  scene,
  recipient,
  subject,
  status,
  error: error || undefined,
  durationMs,
  createdAt: new Date(Date.now() - ago * MIN).toISOString(),
}))

defineMock([
  {
    method: 'GET',
    url: '/sendlogs',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const kw = String(query.recipient ?? '').trim().toLowerCase()
      let list = items.slice()
      if (kw) list = list.filter((r) => r.recipient.toLowerCase().includes(kw))
      if (query.channel) list = list.filter((r) => r.channel === query.channel)
      if (query.status) list = list.filter((r) => r.status === query.status)
      if (query.startDate) list = list.filter((r) => localDate(r.createdAt) >= String(query.startDate))
      if (query.endDate) list = list.filter((r) => localDate(r.createdAt) <= String(query.endDate))
      list.sort((a, b) => b.createdAt.localeCompare(a.createdAt))
      return { list: list.slice((page - 1) * pageSize, page * pageSize), total: list.length, page, pageSize }
    },
  },
  {
    method: 'DELETE',
    url: '/sendlogs',
    handler: ({ query }) => {
      const ids = String(query.ids ?? '').split(',').filter(Boolean)
      if (!ids.length) mockError(400, 'Bad request')
      const before = items.length
      items = items.filter((r) => !ids.includes(r.id))
      return { removed: before - items.length }
    },
  },
  {
    method: 'POST',
    url: '/sendlogs/clean',
    handler: ({ body }) => {
      const days = Number(body?.days)
      if (!Number.isInteger(days) || days < 1) mockError(400, 'Bad request')
      const cutoff = Date.now() - days * 24 * 60 * MIN
      const before = items.length
      items = items.filter((r) => new Date(r.createdAt).getTime() >= cutoff)
      return { removed: before - items.length }
    },
  },
  {
    method: 'GET',
    url: '/sendlogs/:id',
    handler: ({ params }) => items.find((r) => r.id === params.id) ?? mockError(404, 'Not found'),
  },
])
