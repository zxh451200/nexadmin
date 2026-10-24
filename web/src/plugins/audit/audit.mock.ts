import { defineMock, mockError } from '@/framework/request/mock'
import type { AuditAction, AuditConfig, AuditLog, AuditLogDetail, FieldChange } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

// 与后端 Excluded 规则一致：不含 plg_audit_* 与日志类大表。
const AVAILABLE = ['sys_config', 'sys_dept', 'sys_dict', 'sys_menu', 'sys_notice', 'sys_post', 'sys_role', 'sys_tenant', 'sys_user', 'plg_cron_job']

let config: AuditConfig = { tables: ['sys_user', 'sys_role'], available: AVAILABLE }

interface Row {
  log: AuditLog
  changes: Record<string, FieldChange>
}

const mk = (n: number, p: { table: string; pk: string; action: AuditAction; op: string; opId: string; changes: Record<string, FieldChange> }): Row => ({
  log: {
    id: String(7430000000000000000n + BigInt(n)),
    table: p.table,
    rowPk: p.pk,
    action: p.action,
    operatorId: p.opId,
    operatorName: p.op,
    tenantId: '1',
    fields: Object.keys(p.changes).sort().join(','),
    createdAt: ago(n * 37),
  },
  changes: p.changes,
})

let rows: Row[] = [
  mk(1, { table: 'sys_user', pk: '7410000000000000101', action: 'update', op: 'admin', opId: '1', changes: { nickname: { old: '小王', new: '王工' }, email: { old: 'wang@example.com', new: 'wang@corp.example.com' }, password: { old: '***', new: '***' } } }),
  mk(2, { table: 'sys_role', pk: '7410000000000000201', action: 'create', op: 'admin', opId: '1', changes: { name: { old: null, new: '审计员' }, code: { old: null, new: 'auditor' }, status: { old: null, new: 1 } } }),
  mk(3, { table: 'sys_user', pk: '7410000000000000102', action: 'delete', op: 'system', opId: '0', changes: { username: { old: 'temp01', new: null }, nickname: { old: '临时账号', new: null }, api_token: { old: '***', new: null } } }),
  mk(4, { table: 'sys_user', pk: '7410000000000000103', action: 'update', op: 'zhangsan', opId: '2', changes: { status: { old: 1, new: 0 } } }),
  mk(5, { table: 'sys_role', pk: '7410000000000000202', action: 'update', op: 'admin', opId: '1', changes: { remark: { old: '', new: '只读角色' }, sort: { old: 10, new: 20 } } }),
]
for (let i = 6; i <= 30; i++) {
  rows.push(mk(i * 3, { table: i % 2 ? 'sys_user' : 'sys_role', pk: String(7410000000000000300n + BigInt(i)), action: 'update', op: i % 3 ? 'admin' : 'zhangsan', opId: i % 3 ? '1' : '2', changes: { remark: { old: `v${i - 1}`, new: `v${i}` } } }))
}

const paginate = <T,>(list: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: list.slice((page - 1) * pageSize, page * pageSize), total: list.length, page, pageSize }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/audit/logs',
    handler: ({ query }) => {
      let list = rows.map((r) => r.log)
      if (query.table) list = list.filter((l) => l.table === query.table)
      if (query.action) list = list.filter((l) => l.action === query.action)
      if (query.operator) {
        const kw = String(query.operator).toLowerCase()
        list = list.filter((l) => l.operatorName.toLowerCase().includes(kw))
      }
      if (query.from) list = list.filter((l) => l.createdAt.slice(0, 10) >= String(query.from))
      if (query.to) list = list.filter((l) => l.createdAt.slice(0, 10) <= String(query.to))
      list = [...list].sort((a, b) => b.createdAt.localeCompare(a.createdAt))
      return paginate(list, query)
    },
  },
  {
    method: 'GET',
    url: '/plugins/audit/logs/:id',
    handler: ({ params }): AuditLogDetail => {
      const r = rows.find((x) => x.log.id === params.id) ?? mockError(404, 'Not found')
      return { ...r.log, changes: r.changes }
    },
  },
  {
    method: 'GET',
    url: '/plugins/audit/log-tables',
    handler: () => [...new Set(rows.map((r) => r.log.table))].sort(),
  },
  { method: 'GET', url: '/plugins/audit/config', handler: () => config },
  {
    method: 'PUT',
    url: '/plugins/audit/config',
    handler: ({ body }) => {
      const tables: string[] = Array.isArray(body?.tables) ? body.tables : []
      const bad = tables.filter((t) => !AVAILABLE.includes(t))
      if (bad.length) mockError(400, `tables not auditable: ${bad.join(',')}`)
      config = { tables: [...new Set(tables)].sort(), available: AVAILABLE }
      return config
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/audit/logs',
    handler: ({ query }) => {
      const days = Number(query.days)
      if (!Number.isInteger(days) || days < 1 || days > 3650) mockError(400, 'days must be 1-3650')
      const cutoff = Date.now() - days * 86_400_000
      const before = rows.length
      rows = rows.filter((r) => new Date(r.log.createdAt).getTime() >= cutoff)
      return { deleted: before - rows.length }
    },
  },
])
