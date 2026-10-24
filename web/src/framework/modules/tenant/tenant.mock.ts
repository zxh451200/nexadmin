import { defineMock, genId, mockError } from '@/framework/request/mock'
import { allPackages } from '../tenant-package/tenantPkg.mock'
import type { TenantRow, TenantUsage } from './api'

const tid = (n: number) => `75000000000${String(10000000 + n)}`
const pid = (n: number) => `74000000000${String(10000000 + n)}`
/** 演示账号 demo 所属的租户（auth.mock 里的 demo 用户使用它） */
export const DEMO_TENANT_ID = tid(1)
const day = 86400000
const iso = (offsetDays: number) => new Date(Date.now() + offsetDays * day).toISOString()

const t = (
  n: number,
  name: string,
  domain: string,
  pkg: number,
  expireInDays: number,
  status: TenantRow['status'],
  adminUsername: string,
): TenantRow => ({
  id: tid(n),
  name,
  domain,
  packageId: pid(pkg),
  expireAt: iso(expireInDays),
  status,
  adminUsername,
  aiEnabled: true,
  createdAt: iso(-300 + n * 15),
})

let items: TenantRow[] = [
  t(1, '星河科技 Galaxy Tech', 'galaxy.nexadmin.dev', 5, 300, 'active', 'galaxy_admin'),
  t(2, '云帆网络', 'yunfan.nexadmin.dev', 4, 180, 'active', 'yunfan'),
  t(3, 'Acme Corporation', 'acme.nexadmin.dev', 6, 540, 'active', 'acme_admin'),
  t(4, '北辰教育', 'beichen.nexadmin.dev', 7, 90, 'active', 'beichen_root'),
  t(5, 'Blue Harbor Ltd.', 'blueharbor.nexadmin.dev', 3, 45, 'active', 'bh_admin'),
  t(6, '澄明医疗', 'chengming.nexadmin.dev', 4, 20, 'active', 'cm_admin'),
  t(7, 'Northwind Traders', 'northwind.nexadmin.dev', 2, -12, 'expired', 'nw_admin'),
  t(8, '远山设计', 'yuanshan.nexadmin.dev', 2, -40, 'expired', 'ys_admin'),
  t(9, 'Globex Inc.', 'globex.nexadmin.dev', 5, 210, 'suspended', 'globex_admin'),
  t(10, '拾光公益', 'shiguang.nexadmin.dev', 8, 365, 'active', 'sg_admin'),
  t(11, 'Initech', 'initech.nexadmin.dev', 1, 7, 'active', 'initech_admin'),
  t(12, '启明物流', 'qiming.nexadmin.dev', 9, 120, 'suspended', 'qm_admin'),
  t(13, 'Umbrella Labs', 'umbrella.nexadmin.dev', 10, 700, 'active', 'umbrella_admin'),
]

const toIso = (v: unknown): string => {
  const s = String(v ?? '')
  // 日期选择器输出 yyyy-MM-dd；取正午 UTC 避免时区造成日期偏移
  return /^\d{4}-\d{2}-\d{2}$/.test(s) ? `${s}T12:00:00.000Z` : s
}

/** 由租户 ID 派生一个稳定的 0~1 小数，让不同租户的用量占比各不相同（含接近或超过额度的） */
const ratio = (id: string, salt: number) => ((Number(id.slice(-4)) * 37 + salt * 101) % 120) / 100

const quotaOf = (row: TenantRow) => {
  const pkg = allPackages().find((p) => p.id === row.packageId)
  return {
    maxUsers: row.maxUsers ?? pkg?.maxUsers ?? 0,
    maxRoles: row.maxRoles ?? pkg?.maxRoles ?? 0,
    maxStorageMB: row.maxStorageMb ?? pkg?.maxStorageMb ?? 0,
  }
}

const usageOf = (row: TenantRow): TenantUsage => {
  const q = quotaOf(row)
  const used = (max: number, salt: number, fallback: number) => (max > 0 ? Math.round(max * ratio(row.id, salt)) : fallback)
  return {
    users: used(q.maxUsers, 1, 42),
    maxUsers: q.maxUsers,
    storageMB: used(q.maxStorageMB, 2, 1536),
    maxStorageMB: q.maxStorageMB,
    roles: used(q.maxRoles, 3, 6),
    maxRoles: q.maxRoles,
    expireAt: row.expireAt,
    daysLeft: Math.ceil((new Date(row.expireAt).getTime() - Date.now()) / day),
  }
}

/** AI 网关 mock 用：租户的 AI 开关与有效每日配额（租户覆盖优先，其次套餐）。找不到租户 = 平台租户，不受限 */
export function aiPolicyOf(tenantId: string): { disabled: boolean; dailyQuota: number } | null {
  const row = items.find((x) => x.id === tenantId)
  if (!row) return null
  const pkg = allPackages().find((p) => p.id === row.packageId)
  return { disabled: row.aiEnabled === false, dailyQuota: row.aiDailyQuota ?? pkg?.aiDailyQuota ?? 0 }
}

const find = (id: string) => items.find((x) => x.id === id) ?? mockError(404, 'Not found')

defineMock([
  {
    method: 'GET',
    url: '/tenants',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = items.slice()
      if (kw) {
        rows = rows.filter((r) => [r.name, r.domain, r.adminUsername].some((f) => f.toLowerCase().includes(kw)))
      }
      if (query.status) rows = rows.filter((r) => r.status === query.status)
      if (query.packageId) rows = rows.filter((r) => r.packageId === query.packageId)
      return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
    },
  },
  { method: 'GET', url: '/tenants/:id/usage', handler: ({ params }) => usageOf(find(params.id)) },
  { method: 'GET', url: '/tenants/:id', handler: ({ params }) => find(params.id) },
  {
    method: 'POST',
    url: '/tenants',
    handler: ({ body }) => {
      const domain = String(body?.domain ?? '').trim().toLowerCase()
      if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(domain) || /^[0-9.]+$/.test(domain)) {
        mockError(400, 'Invalid domain name')
      }
      if (domain === 'localhost' || /\.(localhost|local|internal|test|invalid|example|lan)$/.test(domain) || /(^|\.)example\.(com|net|org)$/.test(domain)) {
        mockError(400, 'This domain is reserved and cannot be bound')
      }
      if (items.some((x) => x.domain.toLowerCase() === domain)) mockError(409, 'Domain already exists')
      const now = new Date().toISOString()
      // adminPassword / adminEmail / createAdmin 只用于创建管理员账号，不落库、不回显
      const { adminPassword: _pwd, adminEmail: _mail, createAdmin, ...rest } = body ?? {}
      void _pwd
      void _mail
      const row: TenantRow = {
        ...rest,
        domain,
        adminUsername: createAdmin ? rest.adminUsername : '',
        expireAt: toIso(rest.expireAt),
        id: genId(),
        createdAt: now,
      }
      items = [row, ...items]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/tenants/:id/renew',
    handler: ({ params, body }) => {
      const row = find(params.id)
      row.expireAt = toIso(body?.expireAt)
      if (row.status === 'expired') row.status = 'active'
      return row
    },
  },
  {
    method: 'PUT',
    url: '/tenants/:id/status',
    handler: ({ params, body }) => {
      const row = find(params.id)
      row.status = body?.status
      return row
    },
  },
  {
    method: 'PUT',
    url: '/tenants/:id',
    handler: ({ params, body }) => {
      const row = find(params.id)
      const { adminPassword: _pwd, adminEmail: _mail, createAdmin: _ca, id: _id, createdAt: _c, ...rest } = body ?? {}
      void _pwd
      void _mail
      void _ca
      void _id
      void _c
      if (rest.domain) {
        const domain = String(rest.domain).trim().toLowerCase()
        if (items.some((x) => x.id !== row.id && x.domain.toLowerCase() === domain)) mockError(409, 'Domain already exists')
        rest.domain = domain
      }
      if (!rest.adminUsername) rest.adminUsername = row.adminUsername
      Object.assign(row, rest, rest.expireAt ? { expireAt: toIso(rest.expireAt) } : {})
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/tenants/:id',
    handler: ({ params }) => {
      items = items.filter((x) => x.id !== params.id)
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/tenants',
    handler: ({ query }) => {
      const ids = String(query.ids ?? '').split(',').filter(Boolean)
      items = items.filter((x) => !ids.includes(x.id))
      return null
    },
  },
])
