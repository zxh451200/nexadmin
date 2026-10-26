import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { BackupConfig, BackupRecord, BackupStatusInfo } from './api'

const DAY = 24 * 3600 * 1000
const hex = (seed: number) => {
  let s = ''
  for (let i = 0; i < 64; i++) s += ((seed * 7919 + i * 104729 + i * i) % 16).toString(16)
  return s
}
const nameOf = (d: Date) => {
  const p = (n: number) => String(n).padStart(2, '0')
  return `nexadmin-mysql-v0.1.0-${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}.sql.gz`
}

const records: BackupRecord[] = Array.from({ length: 7 }, (_, i) => {
  const d = new Date(Date.now() - i * DAY)
  d.setHours(3, 0, i, 0)
  const failed = i === 3
  return {
    id: `7600000000000000${String(100 + i)}`,
    fileName: nameOf(d),
    engine: 'mysql',
    version: '0.1.0',
    size: failed ? 0 : (180 + i * 3) * 1024 * 1024,
    sha256: failed ? '' : hex(i + 1),
    source: i === 5 ? 'manual' : 'schedule',
    durationMs: 18_000 + i * 700,
    status: failed ? 'failed' : 'success',
    error: failed ? 'mysqldump: Got error: 1045: Access denied for user (using password: YES)' : '',
    createdBy: i === 5 ? '1' : '0',
    createdAt: d.toISOString(),
  }
})

let config: BackupConfig = { schedule: '03:00', keepDays: 30, keepCount: 10 }

const nextRun = (schedule: string): string | null => {
  if (!schedule) return null
  const [h, m] = schedule.split(':').map(Number)
  const d = new Date()
  d.setHours(h, m, 0, 0)
  if (d.getTime() <= Date.now()) d.setDate(d.getDate() + 1)
  return d.toISOString()
}

defineMock([
  {
    method: 'GET',
    url: '/backups',
    handler: ({ query }) => {
      const page = Number(query.page) || 1
      const pageSize = Number(query.pageSize) || 10
      const sorted = [...records].sort((a, b) => b.createdAt.localeCompare(a.createdAt))
      return { list: sorted.slice((page - 1) * pageSize, page * pageSize), total: sorted.length, page, pageSize }
    },
  },
  {
    method: 'GET',
    url: '/backups/status',
    handler: (): BackupStatusInfo => {
      const last = records.filter((r) => r.status === 'success').sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0]
      return {
        available: true,
        tool: 'mysqldump',
        engine: 'mysql',
        version: '0.1.0',
        dir: './backup',
        diskFreeBytes: 38 * 1024 * 1024 * 1024,
        schedule: config.schedule,
        nextRunAt: nextRun(config.schedule),
        running: records.some((r) => r.status === 'running'),
        lastSuccessAt: last?.createdAt ?? null,
      }
    },
  },
  { method: 'GET', url: '/backups/config', handler: (): BackupConfig => config },
  {
    method: 'PUT',
    url: '/backups/config',
    handler: ({ body }): BackupConfig => {
      const b = body as BackupConfig
      if (b.schedule && !/^([01]\d|2[0-3]):[0-5]\d$/.test(b.schedule)) mockError(400, 'schedule must be HH:MM')
      config = { schedule: b.schedule ?? '', keepDays: Number(b.keepDays) || 0, keepCount: Number(b.keepCount) || 0 }
      return config
    },
  },
  {
    method: 'POST',
    url: '/backups',
    handler: (): BackupRecord => {
      if (records.some((r) => r.status === 'running')) mockError(409, 'A backup is already running')
      const now = new Date()
      const rec: BackupRecord = {
        id: genId(),
        fileName: nameOf(now),
        engine: 'mysql',
        version: '0.1.0',
        size: 0,
        sha256: '',
        source: 'manual',
        durationMs: 0,
        status: 'running',
        error: '',
        createdBy: '1',
        createdAt: now.toISOString(),
      }
      records.unshift(rec)
      // 模拟异步完成：约 6 秒后变成成功
      setTimeout(() => {
        rec.status = 'success'
        rec.size = 196 * 1024 * 1024
        rec.sha256 = hex(Math.floor(Date.now() / 1000) % 997)
        rec.durationMs = Date.now() - now.getTime()
      }, 6000)
      return rec
    },
  },
  {
    method: 'GET',
    url: '/backups/:id/download',
    handler: ({ params }) => {
      const r = records.find((x) => x.id === params.id)
      if (!r) mockError(404, 'Not found')
      if (r.status !== 'success') mockError(409, 'Backup is not ready')
      return new Blob([`-- mock backup ${r.fileName}\n`], { type: 'application/gzip' })
    },
  },
  {
    method: 'DELETE',
    url: '/backups/:id',
    handler: ({ params }) => {
      const i = records.findIndex((x) => x.id === params.id)
      if (i < 0) mockError(404, 'Not found')
      if (records[i].status === 'running') mockError(409, 'A backup is running')
      records.splice(i, 1)
      return null
    },
  },
])
