import { defineMock, mockError } from '@/framework/request/mock'
import type { PluginInfo } from './api'

const DAY = 86_400_000
const inDays = (n: number) => new Date(Date.now() + n * DAY).toISOString()

const plugins: PluginInfo[] = [
  {
    id: 'cron',
    name: '定时任务',
    version: '1.0.0',
    edition: 'pro',
    description: 'cron + execution logs + retry',
    status: 'active',
    customer: 'Acme Corp',
    expiresAt: inDays(12),
    reason: '',
  },
  {
    id: 'audit',
    name: '数据变更审计',
    version: '1.2.0',
    edition: 'pro',
    description: 'field-level before/after audit',
    status: 'unlicensed',
    customer: '',
    expiresAt: null,
    reason: 'license file not found: data/licenses/audit.lic',
  },
  {
    id: 'report',
    name: '报表中心',
    version: '2.1.0',
    edition: 'pro',
    description: 'scheduled reports and dashboards',
    status: 'revoked',
    customer: 'Globex Ltd',
    serial: 'a1b2c3d4e5f6',
    expiresAt: inDays(200),
    reason: 'license revoked by issuer (serial a1b2c3d4e5f6): refunded',
  },
  {
    id: 'hello',
    name: '示例插件',
    version: '0.3.1',
    edition: 'free',
    description: 'demo free plugin',
    status: 'active',
    customer: '',
    expiresAt: null,
    reason: '',
  },
]

defineMock([
  { method: 'GET', url: '/plugins', handler: (): PluginInfo[] => plugins.map((p) => ({ ...p })) },
  {
    method: 'POST',
    url: '/plugins/:id/license',
    handler: ({ params, body }): { needRestart: boolean } => {
      const p = plugins.find((x) => x.id === params.id) ?? mockError(404, 'Not found')
      const content = typeof body?.content === 'string' ? body.content.trim() : ''
      if (!content) mockError(400, 'license content is empty')
      // mock：非空即视为有效，授权立即生效、无需重启
      p.status = 'active'
      p.customer = p.customer || 'Demo Customer'
      p.expiresAt = inDays(365)
      p.serial = Math.random().toString(16).slice(2, 14).padEnd(12, '0') // 新授权带新序列号
      p.reason = ''
      return { needRestart: false }
    },
  },
])
