import { defineMock, mockError } from '@/framework/request/mock'
import type { ColumnConfig, ColumnInfo, FieldDef, FieldKind, GenFile, GenOutput, GenRequest, Kind, TableDef, TableSchema } from './api'

// 与服务端一致的白名单与保留名（简化版）
const IDENT = /^[a-z][a-z0-9_]{0,47}$/
const MODULE = /^[a-z][a-z0-9_]{0,30}$/
const NAME = /^[a-z][a-z0-9_]{0,43}$/
const RESERVED_MODULES = new Set(['user', 'role', 'menu', 'dept', 'post', 'dict', 'config', 'core', 'pkg', 'main', 'service', 'handler', 'form', 'view'])
const FRAMEWORK = (n: string) => n.startsWith('sys_') || n.startsWith('plg_') || n === 'schema_migrations'
const SYSTEM_FIELDS = new Set(['id', 'tenant_id', 'created_at', 'updated_at', 'deleted_at'])

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
    ['deleted_at', 'datetime', 'datetime', { nullable: true }],
  ],
}

const CONTROLS: Record<string, ColumnConfig['control'][]> = {
  string: ['input', 'textarea'], text: ['textarea', 'input'], int: ['number', 'input'], float: ['number', 'input'],
  id: ['input'], bool: ['switch'], date: ['date'], datetime: ['datetime'],
}

const REQUIRABLE_KINDS: Kind[] = ['string', 'text', 'date', 'datetime']

const moduleName = (table: string) => {
  const base = table.replace(/^biz_/, '').replace(/_/g, '')
  const full = table.replace(/_/g, '')
  for (const m of [base, full]) {
    const cut = m.slice(0, 31)
    if (MODULE.test(cut)) return cut
  }
  return ''
}

function toSchema(table: string, defs: Def[]): TableSchema {
  const tenant = defs.some((d) => d[0] === 'tenant_id')
  const stamps = defs.some((d) => d[0] === 'created_at') && defs.some((d) => d[0] === 'updated_at')
  let lists = 0
  let queries = 0
  let strQueries = 0
  const columns: ColumnInfo[] = defs.map(([name, type, kind, o = {}]) => {
    const supported = kind !== ''
    const system = name === 'id' || name === 'tenant_id' || (stamps && (name === 'created_at' || name === 'updated_at')) || name === 'deleted_at'
    const cfg: ColumnConfig = { name, list: false, query: false, form: false, required: false, control: '' }
    if (supported && !system) {
      cfg.control = CONTROLS[kind][0]
      cfg.form = true
      cfg.required = REQUIRABLE_KINDS.includes(kind) && !o.nullable && !o.def
      if (kind !== 'text' && lists < 8) {
        cfg.list = true
        lists++
      }
      const stateName = ['status', 'state', 'type', 'kind'].some((s) => name === s || name.endsWith(`_${s}`))
      if (queries < 4) {
        if (kind === 'string' && strQueries < 2) {
          cfg.query = true
          strQueries++
          queries++
        } else if (kind === 'bool' || (stateName && (kind === 'int' || kind === 'string'))) {
          cfg.query = true
          queries++
        }
      }
    }
    return {
      name, type, kind, nullable: !!o.nullable, primaryKey: !!o.pk, comment: o.comment ?? '', system, supported,
      reason: supported ? undefined : 'unsupported column type', length: o.length, config: cfg,
    }
  })
  return { table, shape: tenant ? (stamps ? 'tenant' : 'plain') : stamps ? 'platform' : 'plain', tenant, suggestedModule: moduleName(table), columns }
}

function schemaOf(table: string): TableSchema {
  const defs = TABLES[table]
  if (!defs) mockError(404, 'table not found')
  return toSchema(table, defs)
}

const RAW_TYPE: Record<FieldKind, string> = {
  string: 'varchar', text: 'text', int: 'int', float: 'double', bool: 'bool', date: 'date', datetime: 'datetime',
}

function schemaOfDef(def: TableDef): TableSchema {
  const base = (def.name ?? '').trim().replace(/^biz_/, '')
  if (!NAME.test(base)) mockError(400, 'invalid table name')
  const fields = def.fields ?? []
  if (fields.length === 0) mockError(400, 'no fields')
  const seen = new Set<string>()
  const defs: Def[] = [['id', 'bigint', 'int', { pk: true }]]
  if (def.tenant) defs.push(['tenant_id', 'bigint', 'id'])
  for (const f of fields) {
    if (!IDENT.test(f.name) || SYSTEM_FIELDS.has(f.name) || seen.has(f.name)) mockError(400, `invalid field ${f.name}`)
    seen.add(f.name)
    if (!(f.kind in RAW_TYPE)) mockError(400, `invalid kind ${f.kind}`)
    const requirable = REQUIRABLE_KINDS.includes(f.kind)
    if (f.required && !requirable) mockError(400, `field ${f.name} cannot be required`)
    defs.push([f.name, RAW_TYPE[f.kind], f.kind, {
      nullable: requirable ? !f.required : false,
      def: !requirable,
      comment: f.comment,
      length: f.kind === 'string' ? f.length || 255 : undefined,
    }])
  }
  defs.push(['created_at', 'datetime', 'datetime'], ['updated_at', 'datetime', 'datetime'])
  if (def.softDelete) defs.push(['deleted_at', 'datetime', 'datetime', { nullable: true }])
  return toSchema(`biz_${base}`, defs)
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

function migrationNumber(raw?: string): string {
  const n = (raw ?? '').trim() || '001'
  if (!/^\d{1,4}$/.test(n) || Number(n) === 0) mockError(400, 'invalid migration number')
  return String(Number(n)).padStart(3, '0')
}

function sqlType(f: FieldDef, engine: 'mysql' | 'mariadb' | 'postgres'): string {
  const pg = engine === 'postgres'
  switch (f.kind) {
    case 'string':
      return `VARCHAR(${f.length || 255}) ${f.required ? 'NOT NULL' : 'NULL'}`
    case 'text':
      return `TEXT ${f.required ? 'NOT NULL' : 'NULL'}`
    case 'int':
      return pg ? 'INTEGER NOT NULL DEFAULT 0' : 'INT NOT NULL DEFAULT 0'
    case 'float':
      return pg ? 'DOUBLE PRECISION NOT NULL DEFAULT 0' : 'DOUBLE NOT NULL DEFAULT 0'
    case 'bool':
      return pg ? 'BOOLEAN NOT NULL DEFAULT FALSE' : 'TINYINT(1) NOT NULL DEFAULT 0'
    case 'date':
      return f.required ? 'DATE NOT NULL' : 'DATE NULL'
    default:
      return `${pg ? 'TIMESTAMP(3)' : 'DATETIME(3)'} ${f.required ? 'NOT NULL' : 'NULL'}`
  }
}

function migrations(def: TableDef, table: string): GenFile[] {
  const num = migrationNumber(def.migration)
  return (['mysql', 'mariadb', 'postgres'] as const).map((engine) => {
    const ts = engine === 'postgres' ? 'TIMESTAMP(3)' : 'DATETIME(3)'
    const lines = [
      `-- 业务表 ${table}（codegen 生成）；迁移里只建表，不写数据。`,
      `CREATE TABLE ${table} (`,
      '  id BIGINT NOT NULL,',
      ...(def.tenant ? ['  tenant_id BIGINT NOT NULL,'] : []),
      ...def.fields.map((f) => `  ${f.name} ${sqlType(f, engine)},`),
      `  created_at ${ts} NOT NULL,`,
      `  updated_at ${ts} NOT NULL,`,
      ...(def.softDelete ? [`  deleted_at ${ts} NULL,`] : []),
      '  PRIMARY KEY (id)',
      ');',
      ...(def.tenant ? [`CREATE INDEX idx_${table}_tenant ON ${table} (tenant_id);`] : []),
    ]
    return { path: `migrations/business/${engine}/${num}_${table}.sql`, lang: 'sql', content: `${lines.join('\n')}\n` }
  })
}

function generate(req: GenRequest): GenOutput {
  let sc: TableSchema
  if (req.define) {
    sc = schemaOfDef(req.define)
  } else {
    const table = req.table ?? ''
    if (!IDENT.test(table) || FRAMEWORK(table)) mockError(400, 'invalid table')
    sc = schemaOf(table)
  }
  if (!MODULE.test(req.module) || RESERVED_MODULES.has(req.module)) mockError(400, 'invalid module name')
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
      content: `package ${req.module}\n\ntype ${P} struct {\n\tdatabase.TenantModel\n${req.define?.softDelete ? '\tDeletedAt gorm.DeletedAt\n' : ''}${cols.map((c) => `\t${pascal(c.name)} ${goType(c.kind)} \`gorm:"column:${c.name}"\``).join('\n')}\n}\n\nfunc (${P}) TableName() string { return "${sc.table}" }\n`,
    },
    { path: `${be}/service.go`, lang: 'go', content: `package ${req.module}\n\n// Service: list / get / save / delete for ${sc.table}\n` },
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
  if (req.define) files.push(...migrations(req.define, sc.table))
  files.sort((a, b) => a.path.localeCompare(b.path))
  const notes: GenOutput['notes'] = [
    { code: 'import_line', value: `_ "${req.goModule || 'nexadmin'}/internal/modules/${req.module}"` },
    { code: 'api_prefix', value: `/api/v1/${req.module}s` },
  ]
  if (req.define) {
    notes.push({ code: 'migration', value: `migrations/business/mysql/${migrationNumber(req.define.migration)}_${sc.table}.sql` })
  } else {
    notes.push({ code: 'null_zero', value: '' })
  }
  return { files, notes }
}

defineMock([
  {
    method: 'GET',
    url: '/plugins/codegen/tables',
    handler: () => Object.keys(TABLES).sort().map((name) => ({ name })),
  },
  { method: 'GET', url: '/plugins/codegen/tables/:name/columns', handler: ({ params }) => schemaOf(params.name) },
  {
    method: 'POST',
    url: '/plugins/codegen/schema',
    handler: ({ body }) => {
      const req = body as { table?: string; define?: TableDef }
      if (req.define) return schemaOfDef(req.define)
      if (!req.table || !IDENT.test(req.table) || FRAMEWORK(req.table)) mockError(400, 'invalid table')
      return schemaOf(req.table)
    },
  },
  { method: 'POST', url: '/plugins/codegen/preview', handler: ({ body }) => generate(body as GenRequest) },
  { method: 'POST', url: '/plugins/codegen/download', handler: ({ body }) => generate(body as GenRequest) },
])
