import { createMockJob } from '@/framework/modules/exportcenter/exportcenter.mock'
import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { Audience, DefinitionFile, FormDefinition, FormDetail, FormStatus, FormSummary, SubmissionRow, VersionInfo } from './api'
import { fieldErrorsMessage, validateDefinition, validateSubmission } from './mockEngine'

const B = '/plugins/formbuilder'
const ME = { id: '1', username: 'admin' }
const ROLES = [{ id: '7430000000000000101', name: '管理员' }, { id: '7430000000000000102', name: '运维' }, { id: '7430000000000000103', name: '普通用户' }]
const USERS = [
  { id: '1', username: 'admin', nickname: '管理员' },
  { id: '2', username: 'zhangsan', nickname: '张三' },
  { id: '3', username: 'lisi', nickname: '李四' },
  { id: '4', username: 'wangwu', nickname: '王五' },
]
const MY_ROLE_IDS = [ROLES[0].id]

const iso = (minAgo: number) => new Date(Date.now() - minAgo * 60000).toISOString()
const clone = <T,>(x: T): T => JSON.parse(JSON.stringify(x)) as T

interface Version {
  version: number
  definition: FormDefinition
  createdAt: string
}

/** 表单的服务端状态：draft 是草稿，versions 是冻结的已发布版本，rev/publishedRev 用来判断"有未发布修改" */
interface Form {
  id: string
  status: FormStatus
  draft: FormDefinition
  audience: Audience
  rev: number
  publishedRev: number
  publishedAt: string | null
  versions: Version[]
  createdAt: string
  updatedAt: string
}

const survey: FormDefinition = {
  name: '员工满意度调查',
  description: '每季度一次，匿名性不作保证，请如实填写',
  fields: [
    { key: 'dept', type: 'select', title: '所在部门', required: true, options: [{ label: '研发', value: 'rd' }, { label: '产品', value: 'pm' }, { label: '运维', value: 'ops' }] },
    { key: 'score', type: 'rating', title: '总体满意度', required: true, max: 5 },
    { key: 'likes', type: 'checkbox', title: '你最看重什么', options: [{ label: '薪酬', value: 'pay' }, { label: '成长', value: 'grow' }, { label: '氛围', value: 'team' }], max: 2 },
    { key: 'remote', type: 'switch', title: '是否希望远程办公' },
    { key: 'reason', type: 'textarea', title: '原因', maxLen: 500, showIf: { field: 'remote', op: 'eq', value: true } },
    { key: 'advice', type: 'textarea', title: '改进建议', maxLen: 1000 },
  ],
  settings: { allowRepeat: false, maxSubmissions: 0, successMessage: '感谢你的反馈，我们会认真阅读每一条建议。' },
}
const signup: FormDefinition = {
  name: '年会报名',
  description: '年会报名表，名额有限，先到先得',
  fields: [
    { key: 'name', type: 'text', title: '姓名', required: true, maxLen: 50 },
    { key: 'people', type: 'number', title: '携带人数', integer: true, min: 0, max: 3, default: 0 },
    { key: 'diet', type: 'radio', title: '饮食偏好', options: [{ label: '无', value: 'none' }, { label: '素食', value: 'veg' }, { label: '清真', value: 'halal' }] },
  ],
  settings: { allowRepeat: false, deadline: null, maxSubmissions: 100, successMessage: '报名成功' },
}
const draftOnly: FormDefinition = {
  name: '设备借用登记',
  description: '',
  fields: [{ key: 'device', type: 'text', title: '设备名称', required: true, maxLen: 100 }],
  settings: { allowRepeat: true, maxSubmissions: 0 },
}

const published = (def: FormDefinition, minAgo: number): Version => ({ version: 1, definition: clone(def), createdAt: iso(minAgo) })
let forms: Form[] = [
  { id: '7440000000000000001', status: 'published', draft: clone(survey), audience: { roles: [], users: [] }, rev: 1, publishedRev: 1, publishedAt: iso(4000), versions: [published(survey, 4000)], createdAt: iso(5000), updatedAt: iso(4000) },
  { id: '7440000000000000002', status: 'published', draft: { ...clone(signup), description: '年会报名表（草稿中调整了说明）' }, audience: { roles: [ROLES[0].id], users: [] }, rev: 2, publishedRev: 1, publishedAt: iso(2000), versions: [published(signup, 2000)], createdAt: iso(3000), updatedAt: iso(300) },
  { id: '7440000000000000003', status: 'draft', draft: clone(draftOnly), audience: { roles: [], users: [] }, rev: 1, publishedRev: 0, publishedAt: null, versions: [], createdAt: iso(600), updatedAt: iso(600) },
  { id: '7440000000000000004', status: 'offline', draft: { ...clone(survey), name: '2025 年度调研' }, audience: { roles: [], users: [] }, rev: 1, publishedRev: 1, publishedAt: iso(90000), versions: [published({ ...survey, name: '2025 年度调研' }, 90000)], createdAt: iso(95000), updatedAt: iso(80000) },
]

const subs: Record<string, SubmissionRow[]> = { '7440000000000000001': [], '7440000000000000002': [], '7440000000000000004': [] }
USERS.slice(1).forEach((u, i) => {
  subs['7440000000000000001'].push({ id: genId(), version: 1, userId: u.id, username: u.username, createdAt: iso(3000 - i * 400), data: { dept: ['rd', 'pm', 'ops'][i], score: 5 - i, likes: ['pay', 'grow'], remote: i === 0, ...(i === 0 ? { reason: '通勤时间太长' } : {}), advice: i === 2 ? '希望增加团建' : '' } })
})
subs['7440000000000000002'].push({ id: genId(), version: 1, userId: '2', username: 'zhangsan', createdAt: iso(1500), data: { name: '张三', people: 1, diet: 'none' } })
const submittedBy = new Set<string>(['7440000000000000004:1'])

const need = (id: string): Form => forms.find((f) => f.id === id) ?? mockError(404, '记录不存在')
const latest = (f: Form): Version | undefined => f.versions[f.versions.length - 1]
const pub = (f: Form): FormDefinition | undefined => latest(f)?.definition
const countOf = (f: Form) => (subs[f.id] ?? []).length
const dl = (d?: FormDefinition) => d?.settings.deadline ?? null

function summary(f: Form): FormSummary {
  const p = pub(f)
  return {
    id: f.id, name: f.draft.name, description: f.draft.description ?? '', status: f.status, publishedVersion: latest(f)?.version ?? 0,
    publishedAt: f.publishedAt, hasUnpublished: f.publishedRev !== f.rev, allowRepeat: p?.settings.allowRepeat ?? f.draft.settings.allowRepeat,
    deadlineAt: dl(p), maxSubmissions: p?.settings.maxSubmissions ?? 0, submissionCount: countOf(f), createdAt: f.createdAt, updatedAt: f.updatedAt,
  }
}
const detail = (f: Form): FormDetail => ({ ...summary(f), definition: clone(f.draft), audience: clone(f.audience) })
const touch = (f: Form) => void (f.updatedAt = new Date().toISOString())

/** 与后端 closedReason 一致：截止 > 名额已满 > 已提交且不允许重复 */
function closedOf(f: Form, submitted: boolean) {
  const p = pub(f)
  const max = p?.settings.maxSubmissions ?? 0
  const deadline = dl(p)
  if (deadline && Date.now() >= Date.parse(deadline)) return 'expired' as const
  if (max > 0 && countOf(f) >= max) return 'full' as const
  if (submitted && !p?.settings.allowRepeat) return 'submitted' as const
  return '' as const
}
const didSubmit = (f: Form) => submittedBy.has(`${f.id}:${ME.id}`) || (subs[f.id] ?? []).some((s) => s.userId === ME.id)

/** 填写范围：未限制则所有登录用户可填；否则命中名单用户或角色 */
const visibleToMe = (f: Form) => {
  const a = f.audience
  return (a.roles.length === 0 && a.users.length === 0) || a.users.includes(ME.id) || a.roles.some((r) => MY_ROLE_IDS.includes(r))
}
function fillItem(f: Form) {
  const p = pub(f)
  const submitted = didSubmit(f)
  return {
    id: f.id, name: p?.name ?? f.draft.name, description: p?.description ?? '', publishedVersion: latest(f)?.version ?? 0, deadlineAt: dl(p),
    maxSubmissions: p?.settings.maxSubmissions ?? 0, submissionCount: countOf(f), allowRepeat: p?.settings.allowRepeat ?? false, submitted, closed: closedOf(f, submitted),
  }
}
function fillable(id: string): Form {
  const f = need(id)
  if (f.status !== 'published' || !latest(f)) mockError(404, '记录不存在')
  if (!visibleToMe(f)) mockError(403, '您不在该表单的填写范围内')
  return f
}

const page = <T,>(rows: T[], q: Record<string, unknown>) => {
  const p = Math.max(1, Number(q.page) || 1)
  const size = Math.max(1, Number(q.pageSize) || 10)
  return { list: rows.slice((p - 1) * size, p * size), total: rows.length, page: p, pageSize: size }
}
/** 提交列表筛选：版本、提交人、时间范围，以及按字段值的包含匹配；新的在前 */
function filterSubs(f: Form, q: Record<string, unknown>): SubmissionRow[] {
  const ver = Number(q.version) || 0
  const user = String(q.username ?? '').trim().toLowerCase()
  const field = String(q.field ?? '')
  const value = String(q.value ?? '').trim().toLowerCase()
  if (field && !/^[A-Za-z][A-Za-z0-9_]{0,31}$/.test(field)) mockError(400, '筛选字段不合法')
  const from = q.from ? Date.parse(String(q.from)) : NaN
  const to = q.to ? Date.parse(String(q.to)) + (/^\d{4}-\d{2}-\d{2}$/.test(String(q.to)) ? 86400000 - 1 : 0) : NaN
  const hit = (v: unknown): boolean => (Array.isArray(v) ? v.some(hit) : String(v ?? '').toLowerCase().includes(value))
  return (subs[f.id] ?? [])
    .filter((s) => (!ver || s.version === ver) && (!user || s.username.toLowerCase().includes(user)))
    .filter((s) => (Number.isNaN(from) || Date.parse(s.createdAt) >= from) && (Number.isNaN(to) || Date.parse(s.createdAt) <= to))
    .filter((s) => !field || !value || hit(s.data[field]))
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
}

function csvOf(def: FormDefinition, rows: SubmissionRow[]): string {
  const cols = def.fields.filter((x) => x.type !== 'section')
  const cell = (v: unknown) => `"${(Array.isArray(v) ? v.join(' / ') : String(v ?? '')).replace(/"/g, '""')}"`
  const head = ['提交人', '提交时间', '版本', ...cols.map((c) => c.title)].map(cell).join(',')
  return [head, ...rows.map((r) => [r.username, r.createdAt, `v${r.version}`, ...cols.map((c) => r.data[c.key])].map(cell).join(','))].join('\n')
}

/** 发布时把绑定字典的字段选项快照进版本（mock 没有字典服务，给一组演示选项） */
function snapshotDicts(def: FormDefinition) {
  for (const f of def.fields) {
    if (f.dictType && (f.type === 'radio' || f.type === 'checkbox' || f.type === 'select')) {
      f.options = [{ label: '选项一', value: '1' }, { label: '选项二', value: '2' }, { label: '选项三', value: '3' }]
    }
  }
}

defineMock([
  // ---- 设计 ----
  { method: 'GET', url: `${B}/roles`, handler: () => ROLES },
  {
    method: 'GET',
    url: `${B}/users`,
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      return USERS.filter((u) => !kw || u.username.toLowerCase().includes(kw) || u.nickname.toLowerCase().includes(kw))
    },
  },
  {
    method: 'GET',
    url: `${B}/forms`,
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      const st = String(query.status ?? '')
      const rows = [...forms].reverse().filter((f) => (!kw || f.draft.name.toLowerCase().includes(kw)) && (!st || f.status === st))
      return page(rows.map(summary), query)
    },
  },
  {
    method: 'POST',
    url: `${B}/forms`,
    handler: ({ body }) => {
      const def: FormDefinition = { name: String(body?.name ?? '').trim(), description: body?.description ?? '', fields: [], settings: { allowRepeat: false, maxSubmissions: 0 } }
      validateDefinition(def)
      const now = new Date().toISOString()
      const f: Form = { id: genId(), status: 'draft', draft: def, audience: { roles: [], users: [] }, rev: 1, publishedRev: 0, publishedAt: null, versions: [], createdAt: now, updatedAt: now }
      forms = [...forms, f]
      subs[f.id] = []
      return detail(f)
    },
  },
  {
    // 导入：请求体 = 导出的定义文件；与手工保存草稿走同一套校验
    method: 'POST',
    url: `${B}/forms/import`,
    handler: ({ body }) => {
      const file = body as Partial<DefinitionFile> | undefined
      if (!file || typeof file !== 'object' || !file.definition) mockError(400, '表单定义不是合法的 JSON')
      if (file.format !== 'nexadmin.formbuilder') mockError(400, '不是 NexAdmin 表单定义文件')
      if (file.schemaVersion !== 1) mockError(400, `不支持的文件版本 ${file.schemaVersion}`)
      const def = clone(file.definition as FormDefinition)
      def.fields = def.fields ?? []
      validateDefinition(def)
      const now = new Date().toISOString()
      const f: Form = { id: genId(), status: 'draft', draft: def, audience: { roles: [], users: [] }, rev: 1, publishedRev: 0, publishedAt: null, versions: [], createdAt: now, updatedAt: now }
      forms = [...forms, f]
      subs[f.id] = []
      return detail(f)
    },
  },
  { method: 'GET', url: `${B}/forms/:id`, handler: ({ params }) => detail(need(params.id)) },
  {
    method: 'PUT',
    url: `${B}/forms/:id`,
    handler: ({ params, body }) => {
      const f = need(params.id)
      const def = clone(body?.definition as FormDefinition)
      validateDefinition(def)
      f.draft = def
      f.rev += 1
      if (body?.audience) {
        const a = body.audience as Audience
        if ((a.roles?.length ?? 0) > 200 || (a.users?.length ?? 0) > 200) mockError(400, '可见范围最多 200 个')
        f.audience = { roles: [...(a.roles ?? [])], users: [...(a.users ?? [])] }
      }
      touch(f)
      return detail(f)
    },
  },
  {
    method: 'DELETE',
    url: `${B}/forms/:id`,
    handler: ({ params }) => {
      const f = need(params.id)
      forms = forms.filter((x) => x.id !== f.id)
      delete subs[f.id]
      return null
    },
  },
  {
    method: 'GET',
    url: `${B}/forms/:id/definition`,
    handler: ({ params, query }): DefinitionFile => {
      const f = need(params.id)
      const ver = Number(query.version) || 0
      const def = ver > 0 ? (f.versions.find((v) => v.version === ver) ?? mockError(404, '记录不存在')).definition : f.draft
      return { format: 'nexadmin.formbuilder', schemaVersion: 1, exportedAt: new Date().toISOString(), definition: clone(def) }
    },
  },
  {
    method: 'GET',
    url: `${B}/forms/:id/versions`,
    handler: ({ params }): VersionInfo[] =>
      need(params.id).versions.map((v) => ({ version: v.version, fieldCount: v.definition.fields.length, createdAt: v.createdAt, createdBy: ME.id })).reverse(),
  },
  {
    method: 'GET',
    url: `${B}/forms/:id/versions/:v`,
    handler: ({ params }) => clone((need(params.id).versions.find((v) => v.version === Number(params.v)) ?? mockError(404, '记录不存在')).definition),
  },
  // ---- 发布 ----
  {
    method: 'POST',
    url: `${B}/forms/:id/publish`,
    handler: ({ params }) => {
      const f = need(params.id)
      if (f.versions.length >= 50) mockError(409, '发布版本数已达上限 50')
      const def = clone(f.draft)
      snapshotDicts(def)
      if (def.fields.length === 0) mockError(400, '表单至少需要一个字段')
      validateDefinition(def)
      f.versions.push({ version: (latest(f)?.version ?? 0) + 1, definition: def, createdAt: new Date().toISOString() })
      f.status = 'published'
      f.publishedRev = f.rev
      f.publishedAt = new Date().toISOString()
      touch(f)
      return detail(f)
    },
  },
  {
    method: 'POST',
    url: `${B}/forms/:id/offline`,
    handler: ({ params }) => {
      const f = need(params.id)
      if (f.status !== 'published') mockError(409, '当前状态不允许此操作')
      f.status = 'offline'
      touch(f)
      return detail(f)
    },
  },
  {
    method: 'POST',
    url: `${B}/forms/:id/online`,
    handler: ({ params }) => {
      const f = need(params.id)
      if (f.status !== 'offline') mockError(409, '当前状态不允许此操作')
      f.status = 'published'
      touch(f)
      return detail(f)
    },
  },
  // ---- 填写 ----
  { method: 'GET', url: `${B}/fill/forms`, handler: () => forms.filter((f) => f.status === 'published' && latest(f) && visibleToMe(f)).map(fillItem) },
  {
    method: 'GET',
    url: `${B}/fill/stats`,
    handler: () => ({ pending: forms.filter((f) => f.status === 'published' && latest(f) && visibleToMe(f)).map(fillItem).filter((i) => !i.closed && !i.submitted).length }),
  },
  { method: 'GET', url: `${B}/fill/forms/:id`, handler: ({ params }) => { const f = fillable(params.id); return { ...fillItem(f), definition: clone(pub(f)) } } },
  {
    method: 'POST',
    url: `${B}/fill/forms/:id/submit`,
    handler: ({ params, body }) => {
      const idem = String(body?.idemKey ?? '')
      if (idem && !/^[A-Za-z0-9_-]{8,64}$/.test(idem)) mockError(400, '幂等 key 不合法')
      const f = fillable(params.id)
      const ver = latest(f)!
      const def = ver.definition
      const deadline = dl(def)
      if (deadline && Date.now() >= Date.parse(deadline)) mockError(409, '表单已过截止时间')
      const r = validateSubmission(def, body?.data ?? {})
      if (r.errors) mockError(400, fieldErrorsMessage(def, r.errors))
      const list = (subs[f.id] ??= [])
      // 同一幂等 key 的重放不产生新记录
      const old = idem ? list.find((s) => s.userId === ME.id && (s as SubmissionRow & { idem?: string }).idem === idem) : undefined
      if (old) return { id: old.id, version: old.version, message: def.settings.successMessage ?? '', replayed: true }
      if (def.settings.maxSubmissions > 0 && list.length >= def.settings.maxSubmissions) mockError(409, '提交名额已满')
      if (!def.settings.allowRepeat && didSubmit(f)) mockError(409, '您已提交过该表单')
      const row: SubmissionRow & { idem?: string } = { id: genId(), version: ver.version, userId: ME.id, username: ME.username, createdAt: new Date().toISOString(), data: r.clean ?? {}, ...(idem ? { idem } : {}) }
      list.push(row)
      submittedBy.add(`${f.id}:${ME.id}`)
      return { id: row.id, version: ver.version, message: def.settings.successMessage ?? '', replayed: false }
    },
  },
  // ---- 收集 ----
  {
    method: 'GET',
    url: `${B}/forms/:id/submission-meta`,
    handler: ({ params }) => {
      const f = need(params.id)
      const v = latest(f)
      return { id: f.id, name: f.draft.name, version: v?.version ?? 0, versions: f.versions.map((x) => x.version), definition: clone(v?.definition ?? { name: '', fields: [], settings: { allowRepeat: false, maxSubmissions: 0 } }) }
    },
  },
  {
    method: 'GET',
    url: `${B}/forms/:id/submissions`,
    handler: ({ params, query }) => {
      const f = need(params.id)
      const rows = filterSubs(f, query)
      const scanLimit = 5000
      return { ...page(rows, query), truncated: Boolean(query.field) && rows.length >= scanLimit }
    },
  },
  {
    method: 'GET',
    url: `${B}/forms/:id/submissions/:sid`,
    handler: ({ params }) => {
      const f = need(params.id)
      const s = (subs[f.id] ?? []).find((x) => x.id === params.sid) ?? mockError(404, '记录不存在')
      const ver = f.versions.find((v) => v.version === s.version) ?? mockError(404, '记录不存在')
      return { ...s, definition: clone(ver.definition) }
    },
  },
  {
    method: 'DELETE',
    url: `${B}/forms/:id/submissions/:sid`,
    handler: ({ params }) => {
      const f = need(params.id)
      const list = subs[f.id] ?? []
      if (!list.some((x) => x.id === params.sid)) mockError(404, '记录不存在')
      subs[f.id] = list.filter((x) => x.id !== params.sid)
      return null
    },
  },
  {
    // 同步导出：真实后端返回 xlsx 二进制；mock 返回 JSON，由 downloadXlsx 落成 .mock.json
    method: 'GET',
    url: `${B}/forms/:id/export`,
    handler: ({ params, query }) => {
      const f = need(params.id)
      const rows = filterSubs(f, query)
      if (rows.length > 10000) mockError(400, '导出的行数超过上限，请缩小筛选范围')
      return { name: f.draft.name, total: rows.length, csv: csvOf(latest(f)?.definition ?? f.draft, rows) }
    },
  },
  {
    method: 'POST',
    url: `${B}/forms/:id/export-job`,
    handler: ({ params, query }) => {
      const f = need(params.id)
      const rows = filterSubs(f, query)
      const j = createMockJob('formbuilder.export', `formbuilder-${f.id}.csv`, rows.length, csvOf(latest(f)?.definition ?? f.draft, rows))
      return { id: j.id, type: j.type, status: j.status }
    },
  },
])
