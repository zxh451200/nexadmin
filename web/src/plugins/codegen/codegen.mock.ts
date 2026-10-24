import { defineMock, mockError } from '@/framework/request/mock'
import type { ColumnConfig, ColumnInfo, GenFile, GenOutput, GenRequest, Kind, TableSchema } from './api'

// 与服务端一致的白名单与保留名（简化版）
const IDENT = /^[a-z][a-z0-9_]{0,47}$/
const MODULE = /^[a-z][a-z0-9]{0,30}$/
const RESERVED_MODULES = new Set(['user', 'role', 'menu', 'dept', 'post', 'dict', 'config', 'core', 'pkg', 'main', 'service', 'handler', 'form', 'view'])
const FRAMEWORK = (n: string) => n.startsWith('sys_') || n.startsWith('plg_') || n === 'schema_migrations'

type Def = [name: string, type: string, kind: Kind, opts?: { pk?: boolean; nullable?: boolean; comment?: string; length?: number; def?: boolean }]

const TABLES: Record<string, Def[]> = {
  biz_article: [
    ['id', 'bigint', 'int', { pk: true }],
    ['tenant_id', 'bigint', 'id'],
    ['title', 'varchar', 'string', { comment: '标题', length: 120 }],
    ['summary', 'text', 'text', { comment: '摘要', nullable: true }],
    ['author', 'varchar', 'string', { comment: '作者', length: 50 }],
    ['views', 'int', 'int', { comment: '浏览量' }],
    ['is_top', 'tinyint', 'bool', { comment: '是否置顶' }],
    ['status', 'varchar', 'string', { comment: '状态', length: 20, def: true }],
    ['published_at', 'datetime', 'datetime', { comment: '发布时间', nullable: true }],
    ['cover', 'blob', '', { nullable: true }],
    ['created_at', 'datetime', 'datetime'],
    ['updated_at', 'datetime', 'datetime'],
  ],
  biz_contact: [
    ['id', 'bigint', 'int', { pk: true }],
    ['name', 'varchar', 'string', { comment: '姓名', length: 60 }],
    ['phone', 'varchar', 'string', { comment: '电话', length: 30 }],
    ['birthday', 'date', 'date', { comment: '生日', nullable: true }],
    ['owner_id', 'bigint', 'id', { comment: '负责人' }],
    ['remark', 'text', 'text', { nullable: true }],
  ],
}

const CONTROLS: Record<string, ColumnConfig['control'][]> = {
  string: ['input', 'textarea'], text: ['textarea', 'input'], int: ['number', 'input'], float: ['number', 'input'],
  id: ['input'], bool: ['switch'], date: ['date'], datetime: ['datetime'],
}

function schemaOf(table: string): TableSchema {
  const defs = TABLES[table]
  if (!defs) mockError(404, 'table not found')
  const tenant = defs.some((d) => d[0] === 'tenant_id')
  const stamps = defs.some((d) => d[0] === 'created_at') && defs.some((d) => d[0] === 'updated_at')
  let lists = 0
  let queries = 0
  const columns: ColumnInfo[] = defs.map(([name, type, kind, o = {}]) => {
    const supported = kind !== ''
    const system = name === 'id' || name === 'tenant_id' || (stamps && (name === 'created_at' || name === 'updated_at'))
    const cfg: ColumnConfig = { name, list: false, query: false, form: false, required: false, control: '' }
    if (supported && !system) {
      cfg.control = CONTROLS[kind][0]
      cfg.form = true
      cfg.required = ['string', 'text', 'date', 'datetime'].includes(kind) && !o.nullable && !o.def
      if (kind !== 'text' && lists < 8) {
        cfg.list = true
        lists++
      }
      if (queries < 4 && (kind === 'bool' || (kind === 'string' && queries < 2) || name === 'status')) {
        cfg.query = true
        queries++
      }
    }
    return {
      name, type, kind, nullable: !!o.nullable, primaryKey: !!o.pk, comment: o.comment ?? '', system, supported,
      reason: supported ? undefined : 'unsupported column type', length: o.length, config: cfg,
    }
  })
  const suggested = table.replace(/_/g, '').slice(0, 31)
  return { table, shape: tenant ? (stamps ? 'tenant' : 'plain') : stamps ? 'platform' : 'plain', tenant, suggestedModule: MODULE.test(suggested) ? suggested : '', columns }
}

const pascal = (s: string) => s.split('_').map((p) => p.charAt(0).toUpperCase() + p.slice(1)).join('')
const camel = (s: string) => {
  const p = pascal(s)
  return p.charAt(0).toLowerCase() + p.slice(1)
}
const goType = (k: Kind) =>
  ({ int: 'int64', float: 'float64', bool: 'bool', id: 'idgen.ID', date: '*time.Time', datetime: '*time.Time' })[k as string] ?? 'string'
const tsType = (k: Kind) =>
  ({ int: 'number', float: 'number', bool: 'boolean', date: 'string | null', datetime: 'string | null' })[k as string] ?? 'string'

function generate(req: GenRequest): GenOutput {
  if (!IDENT.test(req.table) || FRAMEWORK(req.table)) mockError(400, 'invalid table')
  if (!MODULE.test(req.module) || RESERVED_MODULES.has(req.module)) mockError(400, 'invalid module name')
  const sc = schemaOf(req.table)
  const cfgBy = new Map(req.columns.map((c) => [c.name, c]))
  for (const n of cfgBy.keys()) if (!sc.columns.some((c) => c.name === n)) mockError(400, `column ${n} does not exist`)
  const cols = sc.columns
    .filter((c) => c.supported && !c.system)
    .map((c) => ({ ...c, config: cfgBy.get(c.name) ?? c.config }))
  const P = pascal(req.module)
  const be = `internal/modules/${req.module}`
  const fe = `web/src/pages/${req.module}`
  const form = cols.filter((c) => c.config.form)
  const files: GenFile[] = [
    {
      path: `${be}/model.go`, lang: 'go',
      content: `package ${req.module}\n\ntype ${P} struct {\n\tdatabase.TenantModel\n${cols.map((c) => `\t${pascal(c.name)} ${goType(c.kind)} \`gorm:"column:${c.name}"\``).join('\n')}\n}\n\nfunc (${P}) TableName() string { return "${req.table}" }\n`,
    },
    { path: `${be}/service.go`, lang: 'go', content: `package ${req.module}\n\n// Service: list / get / save / delete for ${req.table}\n` },
    { path: `${be}/handler.go`, lang: 'go', content: `package ${req.module}\n\n// Handler: HTTP layer for /${req.module}s\n` },
    {
      path: `${be}/module.go`, lang: 'go',
      content: `package ${req.module}\n\nfunc init() { core.RegisterBuiltin(module{}) }\n\n// routes guarded by core.Perm("${req.module}:list|edit|delete")\n`,
    },
    { path: `${be}/seed.go`, lang: 'go', content: `package ${req.module}\n\n// seedMenus: menu ${req.module}:list + buttons edit / delete\n` },
    {
      path: `${fe}/route.tsx`, lang: 'tsx',
      content: `export const routes = [{ path: '${req.module}', perm: '${req.module}:list' }]\n`,
    },
    {
      path: `${fe}/api.ts`, lang: 'ts',
      content: `export interface ${P} {\n  id: string\n${cols.map((c) => `  ${camel(c.name)}: ${tsType(c.kind)}`).join('\n')}\n}\n`,
    },
    { path: `${fe}/${req.module}.mock.ts`, lang: 'ts', content: `// mock data for /${req.module}s\n` },
    { path: `${fe}/${P}Page.tsx`, lang: 'tsx', content: `// list page: DataTable + FilterBar (${cols.filter((c) => c.config.list).length} columns, ${cols.filter((c) => c.config.query).length} filters)\n` },
    { path: `${fe}/${P}FormDialog.tsx`, lang: 'tsx', content: `// form dialog: ResponsiveDialog + FormLayout (${form.length} fields)\n` },
    {
      path: `${fe}/locales/zh-CN.json`, lang: 'json',
      content: JSON.stringify({ [req.module]: { menu: req.module, field: Object.fromEntries(cols.map((c) => [camel(c.name), c.comment || c.name])) } }, null, 2) + '\n',
    },
    {
      path: `${fe}/locales/en-US.json`, lang: 'json',
      content: JSON.stringify({ [req.module]: { menu: req.module, field: Object.fromEntries(cols.map((c) => [camel(c.name), pascal(c.name)])) } }, null, 2) + '\n',
    },
  ]
  files.sort((a, b) => a.path.localeCompare(b.path))
  return {
    files,
    notes: [
      { code: 'import_line', value: `_ "${req.goModule || 'nexadmin'}/internal/modules/${req.module}"` },
      { code: 'api_prefix', value: `/api/v1/${req.module}s` },
      { code: 'null_zero', value: '' },
    ],
  }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/codegen/tables',
    handler: () => Object.keys(TABLES).sort().map((name) => ({ name })),
  },
  { method: 'GET', url: '/plugins/codegen/tables/:name/columns', handler: ({ params }) => schemaOf(params.name) },
  { method: 'POST', url: '/plugins/codegen/preview', handler: ({ body }) => generate(body as GenRequest) },
  { method: 'POST', url: '/plugins/codegen/download', handler: ({ body }) => generate(body as GenRequest) },
])
