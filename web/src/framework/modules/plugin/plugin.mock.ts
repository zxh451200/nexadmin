import { defineMock, mockError } from '@/framework/request/mock'
import type { MarketEntry, MarketJob, PluginInfo } from './api'

const DAY = 86_400_000
const inDays = (n: number) => new Date(Date.now() + n * DAY).toISOString()

const plugins: PluginInfo[] = [
  {
    id: 'cron',
    name: '定时任务',
    version: '1.0.0',
    edition: 'free',
    description: 'Free scheduled jobs: cron + execution logs + retry',
    status: 'active',
    customer: '',
    expiresAt: null,
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

  // ---- 插件市场（mock：目录静态、任务每次轮询推进一段日志） ----
  {
    method: 'GET',
    url: '/plugins/market',
    handler: (): MarketEntry[] =>
      catalog.map((e) => {
        const installed = plugins.some((p) => p.id === e.id)
        const p = plugins.find((x) => x.id === e.id)
        return { ...e, installed, installedVersion: installed ? p?.version : undefined, status: installed ? p?.status : undefined }
      }),
  },
  {
    method: 'POST',
    url: '/plugins/market/install',
    handler: ({ body }): { jobId: string } => startMockJob(String(body?.id ?? ''), 'install'),
  },
  {
    method: 'POST',
    url: '/plugins/market/remove',
    handler: ({ body }): { jobId: string } => startMockJob(String(body?.id ?? ''), 'remove'),
  },
  {
    method: 'POST',
    url: '/plugins/market/rebuild',
    handler: (): { jobId: string } => startMockJob('', 'rebuild'),
  },
  { method: 'GET', url: '/plugins/market/jobs/:jobId', handler: ({ params }): MarketJob => pollMockJob(String(params.jobId)) },
])

const catalog: Omit<MarketEntry, 'installed'>[] = [
  {
    id: 'cron',
    name: '定时任务',
    description: '免费定时任务插件：cron 调度、立即执行、失败重试与执行日志',
    category: 'devops',
    edition: 'free',
    version: '1.0.0',
    price: '免费',
    repo: 'acme/nexadmin-plugin-cron',
  },
  {
    id: 'hello',
    name: '示例插件',
    description: 'market demo entry (mock)',
    category: 'business',
    edition: 'free',
    version: '0.3.1',
    repo: 'acme/nexadmin-plugin-hello',
  },
  {
    id: 'payment',
    name: '支付中心',
    description: '支付渠道、订单、退款与回调管理',
    category: 'business',
    edition: 'pro',
    version: '0.1.0',
    repo: 'acme/nexadmin-plugin-payment',
  },
  {
    id: 'totp',
    name: '两步验证',
    description: '免费基础 TOTP 两步验证；插件需按需安装',
    category: 'security',
    edition: 'free',
    version: '0.1.0',
    price: '免费',
    repo: 'acme/nexadmin-plugin-totp',
  },
]

const mockJobs = new Map<string, MarketJob>()
let mockJobSeq = 0

const MOCK_STEPS: Record<MarketJob['kind'], (id: string) => string[]> = {
  install: (id) => [
    `从 GitHub acme/nexadmin-plugin-${id} 拉取插件包…`,
    '下载完成，开始安装…',
    `插件 ${id} 已装入源码树，开始重建…`,
    '$ 构建前端',
    '$ go mod tidy',
    '$ go build',
    '二进制已替换',
    '2 秒后重启服务…',
  ],
  remove: (id) => [`移除插件 ${id}…`, '已移除源码文件', '$ go mod tidy', '$ go build', '2 秒后重启服务…'],
  rebuild: () => ['$ 构建前端', '$ go mod tidy', '$ go build', '二进制已替换', '2 秒后重启服务…'],
}

function startMockJob(id: string, kind: MarketJob['kind']): { jobId: string } {
  mockJobSeq += 1
  const job: MarketJob = { id: `${kind}-${mockJobSeq}-${id}`, kind, state: 'running', log: [] }
  mockJobs.set(job.id, job)
  return { jobId: job.id }
}

function pollMockJob(jobId: string): MarketJob {
  const job = mockJobs.get(jobId) ?? mockError(404, 'Not found')
  const steps = MOCK_STEPS[job.kind](jobId.split('-').pop() ?? '')
  // 每次轮询吐 2 行，吐完进入 restarting → done
  const next = Math.min(job.log.length + 2, steps.length)
  job.log = steps.slice(0, next)
  if (next < steps.length) {
    job.state = 'running'
  } else if (job.state === 'running') {
    job.state = 'restarting'
  } else {
    job.state = 'done'
  }
  return { ...job, log: [...job.log] }
}
