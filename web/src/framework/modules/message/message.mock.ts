import { defineMock, mockError } from '@/framework/request/mock'
import { unreadNoticeCount } from '../notice/notice.mock'
import type { Message } from './api'

const mid = (n: number) => String(7420000000000000000n + BigInt(n))
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

type Seed = [category: string, title: string, content: string, link: string | undefined, minutesAgo: number, read: boolean]

const SEED: Seed[] = [
  ['security', '检测到异地登录', '您的账号于 10 分钟前在 上海 (IP 203.0.113.24) 登录。如非本人操作，请立即修改密码。', '/profile', 10, false],
  ['todo', '待审批：张伟的请假申请', '张伟提交了 3 天年假申请（10/20 - 10/22），等待您审批。', '/dashboard', 35, false],
  ['business', '订单 #20261017-0098 已发货', 'Order #20261017-0098 has been shipped via SF Express. Tracking no. SF1029384756.', undefined, 80, false],
  ['system', '系统维护通知', '系统将于本周六 22:00 - 24:00 进行升级维护，期间部分功能不可用。', '/content/notice', 150, false],
  ['todo', '待办提醒：季度报表待提交', '财务季度报表将于明日 18:00 截止提交，请尽快完成。', undefined, 260, false],
  ['business', '客户 Acme Corp 已续费', 'Acme Corp renewed the Enterprise plan for 12 months. Amount: ¥128,000.', undefined, 480, false],
  ['security', '密码即将过期', '您的密码将在 5 天后过期，请前往个人中心修改。', '/profile', 700, false],
  ['system', '导出任务已完成', '您发起的用户列表导出任务已完成，文件将保留 7 天。', undefined, 1000, true],
  ['business', '新工单分配给您', '工单 #T-5521「接口 502 报警」已分配给您，优先级：高。', undefined, 1500, false],
  ['todo', '会议提醒：产品评审 15:00', 'Product review starts at 15:00 in Room 3B. Please prepare your slides.', undefined, 1800, true],
  ['system', '角色权限已更新', '管理员调整了您的角色权限，重新登录后生效。', undefined, 2400, true],
  ['security', '账号已绑定新设备', '您的账号已在 iPhone 15 (Safari) 上登录并绑定。', '/profile', 3000, true],
  ['business', '合同 CT-2026-044 待签署', '合同 CT-2026-044 已完成法务审核，等待您签署。', undefined, 4000, false],
  ['system', 'Welcome to NexAdmin', 'Your account has been activated. Explore the dashboard to get started.', '/dashboard', 5000, true],
  ['todo', '待办提醒：本周周报', '请于本周五前提交周报。', undefined, 6200, true],
  ['business', '库存预警：SKU-8841', 'SKU-8841 库存低于安全线（剩余 12 件），建议补货。', undefined, 7400, true],
  ['system', '数据备份成功', '每日 03:00 自动备份已完成，大小 2.4 GB。', undefined, 8600, true],
  ['security', '登录失败次数过多', '您的账号在 5 分钟内连续登录失败 4 次，已临时提醒。', '/profile', 10000, true],
  ['business', '发票已开具', '发票 INV-20261009-17 已开具并发送至您的邮箱。', undefined, 12000, true],
  ['system', '版本更新：v1.3.0', '新增消息中心与公告已读统计，详情见更新日志。', '/content/notice', 14400, true],
]

let items: Message[] = SEED.map(([category, title, content, link, m, read], i) => ({
  id: mid(i + 1),
  category,
  title,
  content,
  link,
  readAt: read ? ago(m - 5) : null,
  createdAt: ago(m),
}))

const unreadMessages = () => items.filter((m) => !m.readAt).length

defineMock([
  {
    method: 'GET',
    url: '/messages/unread-count',
    handler: () => ({ messages: unreadMessages(), notices: unreadNoticeCount() }),
  },
  {
    method: 'GET',
    url: '/messages',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      let rows = items.slice()
      if (query.unread === '1' || query.unread === 'true') rows = rows.filter((m) => !m.readAt)
      if (query.category) rows = rows.filter((m) => m.category === query.category)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      if (kw) rows = rows.filter((m) => `${m.title} ${m.content}`.toLowerCase().includes(kw))
      // 未读优先，其后按时间倒序
      rows.sort(
        (a, b) =>
          Number(!!a.readAt) - Number(!!b.readAt) || new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
      )
      return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
    },
  },
  {
    method: 'PUT',
    url: '/messages/read-all',
    handler: () => {
      const now = new Date().toISOString()
      items = items.map((m) => (m.readAt ? m : { ...m, readAt: now }))
      return null
    },
  },
  {
    method: 'PUT',
    url: '/messages/:id/read',
    handler: ({ params }) => {
      if (!items.some((m) => m.id === params.id)) mockError(404, 'Not found')
      const now = new Date().toISOString()
      items = items.map((m) => (m.id === params.id && !m.readAt ? { ...m, readAt: now } : m))
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/messages',
    handler: () => {
      items = []
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/messages/:id',
    handler: ({ params }) => {
      items = items.filter((m) => m.id !== params.id)
      return null
    },
  },
])
