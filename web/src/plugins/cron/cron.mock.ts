import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { CronHandler, CronJob, CronJobForm, CronLog } from './api'
import { parseCron } from './cron'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const HANDLERS: CronHandler[] = [
  { name: 'clean_logs', description: 'Delete operation/login logs older than N days' },
  { name: 'http', description: 'Call an HTTP(S) URL (GET/POST, public addresses only)' },
  { name: 'log', description: 'Write a log line (demo)' },
]

const nextOf = (j: Pick<CronJob, 'cronExpr' | 'enabled'>) => {
  if (!j.enabled) return null
  const r = parseCron(j.cronExpr)
  return r.ok ? (r.schedule.next(new Date())?.toISOString() ?? null) : null
}

const mkJob = (n: number, p: Partial<CronJob> & Pick<CronJob, 'name' | 'cronExpr' | 'handler'>): CronJob => {
  const job: CronJob = {
    id: String(7420000000000000000n + BigInt(n)),
    params: '',
    enabled: true,
    retryCount: 0,
    lastRunAt: null,
    lastStatus: '',
    nextRunAt: null,
    createdAt: ago(60 * 24 * (10 - n)),
    updatedAt: ago(60 * 24 * (10 - n)),
    ...p,
  }
  job.nextRunAt = nextOf(job)
  return job
}

let jobs: CronJob[] = [
  mkJob(1, { name: '清理 30 天前的日志', cronExpr: '0 3 * * *', handler: 'clean_logs', params: '{"days":30}', lastRunAt: ago(300), lastStatus: 'success' }),
  mkJob(2, { name: '健康检查心跳', cronExpr: '*/5 * * * *', handler: 'http', params: '{"method":"GET","url":"https://example.com/health","timeoutSec":10}', retryCount: 2, lastRunAt: ago(3), lastStatus: 'failed' }),
  mkJob(3, { name: '演示：写一条日志', cronExpr: '0 9 * * 1-5', handler: 'log', params: '{"message":"good morning"}', lastRunAt: ago(1500), lastStatus: 'success' }),
  mkJob(4, { name: '每周一汇总（已停用）', cronExpr: '0 0 * * 1', handler: 'log', enabled: false }),
]

const mkLog = (n: number, job: CronJob, p: Partial<CronLog>): CronLog => ({
  id: String(7421000000000000000n + BigInt(n)),
  jobId: job.id,
  jobName: job.name,
  handler: job.handler,
  trigger: 'schedule',
  status: 'success',
  attempts: 1,
  startedAt: ago(n * 17),
  finishedAt: ago(n * 17 - 0.02),
  costMs: 120 + n * 13,
  output: '',
  error: '',
  ...p,
})

let logs: CronLog[] = Array.from({ length: 30 }, (_, i) => {
  const job = jobs[i % 3]
  const failed = job.handler === 'http' && i % 2 === 0
  return mkLog(i + 1, job, {
    status: failed ? 'failed' : 'success',
    attempts: failed ? 3 : 1,
    trigger: i % 7 === 0 ? 'manual' : 'schedule',
    output:
      job.handler === 'clean_logs'
        ? 'sys_oper_log: deleted 128 rows older than 30 days\nsys_login_log: deleted 42 rows older than 30 days'
        : job.handler === 'http'
          ? 'GET https://example.com/health -> ' + (failed ? '503' : '200') + '\nok'
          : 'good morning',
    error: failed ? 'unexpected status 503' : '',
  })
})

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

const findJob = (id: string) => jobs.find((j) => j.id === id) ?? mockError(404, 'Not found')

function validate(f: CronJobForm) {
  if (!f.name?.trim()) mockError(400, 'name is required')
  const r = parseCron(f.cronExpr ?? '')
  if (!r.ok) mockError(400, `invalid cron expression (${r.code})`)
  if (!HANDLERS.some((h) => h.name === f.handler)) mockError(400, `unknown handler "${f.handler}"`)
  if (f.params?.trim()) {
    try {
      const v = JSON.parse(f.params)
      if (!v || typeof v !== 'object' || Array.isArray(v)) throw new Error()
    } catch {
      mockError(400, 'params must be a JSON object')
    }
  }
}

defineMock([
  { method: 'GET', url: '/plugins/cron/handlers', handler: () => HANDLERS },
  {
    method: 'GET',
    url: '/plugins/cron/stats',
    handler: () => ({
      total: jobs.length,
      enabled: jobs.filter((j) => j.enabled).length,
      failed24h: logs.filter((l) => l.status === 'failed' && Date.now() - new Date(l.startedAt).getTime() < 24 * 3600_000).length,
    }),
  },
  {
    method: 'GET',
    url: '/plugins/cron/jobs',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = jobs.filter((j) => !kw || j.name.toLowerCase().includes(kw))
      if (query.handler) rows = rows.filter((j) => j.handler === query.handler)
      if (query.enabled === '1' || query.enabled === 'true') rows = rows.filter((j) => j.enabled)
      if (query.enabled === '0' || query.enabled === 'false') rows = rows.filter((j) => !j.enabled)
      return paginate([...rows].reverse(), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/cron/jobs',
    handler: ({ body }) => {
      validate(body)
      const now = new Date().toISOString()
      const job: CronJob = { ...body, id: genId(), lastRunAt: null, lastStatus: '', nextRunAt: null, createdAt: now, updatedAt: now }
      job.nextRunAt = nextOf(job)
      jobs = [...jobs, job]
      return job
    },
  },
  {
    method: 'PUT',
    url: '/plugins/cron/jobs/:id',
    handler: ({ params, body }) => {
      validate(body)
      const old = findJob(params.id)
      const job = { ...old, ...body, updatedAt: new Date().toISOString() } as CronJob
      job.nextRunAt = nextOf(job)
      jobs = jobs.map((j) => (j.id === old.id ? job : j))
      return job
    },
  },
  {
    method: 'PUT',
    url: '/plugins/cron/jobs/:id/enabled',
    handler: ({ params, body }) => {
      const old = findJob(params.id)
      const job = { ...old, enabled: !!body?.enabled } as CronJob
      job.nextRunAt = nextOf(job)
      jobs = jobs.map((j) => (j.id === old.id ? job : j))
      return job
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/cron/jobs/:id',
    handler: ({ params }) => {
      findJob(params.id)
      jobs = jobs.filter((j) => j.id !== params.id)
      return null
    },
  },
  {
    method: 'POST',
    url: '/plugins/cron/jobs/:id/run',
    handler: ({ params }) => {
      const job = findJob(params.id)
      if (logs.some((l) => l.jobId === job.id && l.status === 'running')) mockError(409, 'job is already running')
      const row: CronLog = {
        id: genId(), jobId: job.id, jobName: job.name, handler: job.handler, trigger: 'manual', status: 'running',
        attempts: 0, startedAt: new Date().toISOString(), finishedAt: null, costMs: 0, output: '', error: '',
      }
      logs = [row, ...logs]
      // 模拟异步执行：2 秒后结束，http 任务演示失败
      setTimeout(() => {
        const failed = job.handler === 'http'
        logs = logs.map((l) =>
          l.id === row.id
            ? { ...l, status: failed ? 'failed' : 'success', attempts: failed ? job.retryCount + 1 : 1, finishedAt: new Date().toISOString(), costMs: 2000,
                output: failed ? `GET mock -> 503` : 'done', error: failed ? 'unexpected status 503' : '' }
            : l,
        )
        jobs = jobs.map((j) => (j.id === job.id ? { ...j, lastRunAt: row.startedAt, lastStatus: failed ? 'failed' : 'success' } : j))
      }, 2000)
      return row
    },
  },
  {
    method: 'GET',
    url: '/plugins/cron/logs',
    handler: ({ query }) => {
      let rows = logs
      if (query.jobId) rows = rows.filter((l) => l.jobId === query.jobId)
      if (query.status) rows = rows.filter((l) => l.status === query.status)
      rows = [...rows].sort((a, b) => b.startedAt.localeCompare(a.startedAt))
      const p = paginate(rows, query)
      return { ...p, list: p.list.map((l) => ({ ...l, output: l.output.slice(0, 500), error: l.error.slice(0, 500) })) }
    },
  },
  {
    method: 'GET',
    url: '/plugins/cron/logs/:id',
    handler: ({ params }) => logs.find((l) => l.id === params.id) ?? mockError(404, 'Not found'),
  },
  {
    method: 'DELETE',
    url: '/plugins/cron/logs',
    handler: ({ query }) => {
      const days = Number(query.days) || 0
      const cutoff = Date.now() - days * 86_400_000
      const keep = (l: CronLog) =>
        l.status === 'running' || (query.jobId && l.jobId !== query.jobId) || (days > 0 && new Date(l.startedAt).getTime() >= cutoff)
      const before = logs.length
      logs = logs.filter(keep)
      return { deleted: before - logs.length }
    },
  },
])
