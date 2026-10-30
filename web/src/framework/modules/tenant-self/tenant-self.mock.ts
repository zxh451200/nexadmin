import { defineMock } from '@/framework/request/mock'
import type { TenantSelf } from './api'

const day = 86400000
const iso = (offsetDays: number) => new Date(Date.now() + offsetDays * day).toISOString()
const dayKey = (offsetDays: number) => new Date(Date.now() + offsetDays * day).toISOString().slice(0, 10)

const trend = Array.from({ length: 14 }, (_, i) => ({
  day: dayKey(i - 13),
  users: 30 + i,
  roles: 4 + Math.floor(i / 4),
  storageMB: 320 + i * 12,
}))

let self: TenantSelf = {
  name: '星河科技 Galaxy Tech',
  domain: 'galaxy.nexadmin.dev',
  status: 'active',
  packageName: '企业版 Enterprise',
  expireAt: iso(300),
  daysLeft: 300,
  companyName: '星河科技',
  contactName: '李明',
  contactPhone: '13800000000',
  contactEmail: 'admin@galaxy.dev',
  logoUrl: '',
  aiEnabled: true,
  aiDailyQuota: 1000,
  usage: { users: 43, maxUsers: 1000, storageMB: 476, maxStorageMB: 204800, roles: 7, maxRoles: 200, expireAt: iso(300), daysLeft: 300 },
  trend,
}

// 演示租户 1 即 demo 账号所属租户；未登录/平台账号在 mock 里也返回它，便于页面预览。
defineMock([
  { method: 'GET', url: '/tenant-self', handler: () => self },
  {
    method: 'PUT',
    url: '/tenant-self',
    handler: ({ body }) => {
      self = { ...self, ...body }
      return self
    },
  },
])
