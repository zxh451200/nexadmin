import { createCrudMock, defineMock, genId, mockError } from '@/framework/request/mock'
import { onLockChange, unlockAccount } from '@/framework/auth/auth.mock'
import { createMockJob } from '../exportcenter/exportcenter.mock'
import { moveToRecycle, registerRestorer } from '../recycle/recycle.mock'
import { roleIds, roleSeed } from '../role/role.mock'
import type { ImportResult, User } from './api'
import { readXlsxRows } from './mock-xlsx'

const dept = {
  hq: ['1860000000000000001', '总公司 Headquarters'],
  rd: ['1860000000000000002', '研发中心'],
  qa: ['1860000000000000008', '测试组 QA'],
  ops: ['1860000000000000004', '运营中心'],
  fin: ['1860000000000000005', '综合管理部'],
} as const

type Row = [
  idx: number,
  username: string,
  nickname: string,
  phone: string,
  email: string,
  gender: string,
  dept: readonly [string, string],
  roles: string[],
  status: string,
  region: User['region'],
  createdAt: string,
]

const bj = { province: '110000', city: '110100', district: '110105' }
const sh = { province: '310000', city: '310100', district: '310104' }
const sz = { province: '440000', city: '440300', district: '440305' }
const hz = { province: '330000', city: '330100', district: '330106' }

const rows: Row[] = [
  [1, 'admin', '系统管理员', '13800138000', 'admin@nexadmin.dev', '1', dept.hq, [roleIds.admin], '1', bj, '2026-01-05'],
  [2, 'zhangwei', '张伟', '13800138002', 'zhangwei@nexadmin.dev', '1', dept.rd, [roleIds.operator], '1', bj, '2026-01-10'],
  [3, 'lixiaoling', '李晓玲', '13900139003', 'lixl@nexadmin.dev', '2', dept.ops, [roleIds.operator], '1', sh, '2026-01-14'],
  [4, 'wangfang', '王芳', '13700137004', 'wangfang@nexadmin.dev', '2', dept.fin, [roleIds.auditor], '1', hz, '2026-01-21'],
  [5, 'chenjie', '陈杰', '13600136005', 'chenjie@nexadmin.dev', '1', dept.rd, [roleIds.editor], '0', sz, '2026-02-02'],
  [6, 'emma.wilson', 'Emma Wilson', '13500135006', 'emma.wilson@example.com', '2', dept.ops, [roleIds.operator, roleIds.editor], '1', sh, '2026-02-09'],
  [7, 'liuyang', '刘洋', '13400134007', 'liuyang@nexadmin.dev', '1', dept.qa, [roleIds.auditor], '1', bj, '2026-02-17'],
  [8, 'james.smith', 'James Smith', '13300133008', 'james.smith@example.com', '1', dept.hq, [roleIds.guest], '0', sz, '2026-02-25'],
  [9, 'zhaomin', '赵敏', '13200132009', 'zhaomin@nexadmin.dev', '2', dept.fin, [roleIds.guest], '1', hz, '2026-03-03'],
  [10, 'sunhao', '孙浩', '15800158010', 'sunhao@nexadmin.dev', '1', dept.rd, [roleIds.operator], '1', bj, '2026-03-11'],
  [11, 'zhouxin', '周欣', '15900159011', 'zhouxin@nexadmin.dev', '2', dept.qa, [roleIds.auditor, roleIds.guest], '1', sh, '2026-03-19'],
  [12, 'wuqiang', '吴强', '15700157012', 'wuqiang@nexadmin.dev', '1', dept.ops, [roleIds.editor], '0', sz, '2026-03-28'],
  [13, 'olivia.brown', 'Olivia Brown', '15600156013', 'olivia.brown@example.com', '2', dept.hq, [roleIds.guest], '1', sh, '2026-04-06'],
  [14, 'zhengtao', '郑涛', '15500155014', 'zhengtao@nexadmin.dev', '1', dept.rd, [roleIds.operator], '1', hz, '2026-04-15'],
  [15, 'mayu', '马玉', '15300153015', 'mayu@nexadmin.dev', '0', dept.fin, [roleIds.auditor], '1', bj, '2026-04-22'],
  [16, 'huangbo', '黄波', '18800188016', 'huangbo@nexadmin.dev', '1', dept.qa, [roleIds.guest], '1', sz, '2026-05-04'],
  [17, 'linna', '林娜', '18900189017', 'linna@nexadmin.dev', '2', dept.ops, [roleIds.operator], '1', sh, '2026-05-13'],
  [18, 'noah.davis', 'Noah Davis', '18700187018', 'noah.davis@example.com', '1', dept.rd, [roleIds.editor, roleIds.guest], '0', hz, '2026-05-30'],
]

const roleName = new Map(roleSeed.map((r) => [r.id, r.name]))

const seed: User[] = rows.map(([i, username, nickname, phone, email, gender, d, roles, status, region, created]) => ({
  id: String(1800000000000000000n + BigInt(i)),
  username,
  nickname,
  avatar: '',
  phone,
  email,
  gender,
  deptId: d[0],
  deptName: d[1],
  postIds: [],
  roleIds: roles,
  roleNames: roles.map((r) => roleName.get(r) ?? r),
  status,
  region,
  remark: i === 1 ? '系统内置管理员' : '',
  locked: false,
  lockedUntil: null,
  createdAt: `${created}T09:00:00.000Z`,
  updatedAt: `${created}T09:00:00.000Z`,
}))

/** createdFrom / createdTo 为 YYYY-MM-DD，按本地日期闭区间比较 createdAt */
function inCreatedRange(u: User, query: Record<string, unknown>): boolean {
  const from = String(query.createdFrom ?? '')
  const to = String(query.createdTo ?? '')
  const day = u.createdAt.slice(0, 10)
  if (from && day < from) return false
  if (to && day > to) return false
  return true
}

const MOCK_SYNC_LIMIT = 20

function filtered(query: Record<string, unknown>): User[] {
  let list = store.all().slice()
  const kw = String(query.keyword ?? '').trim().toLowerCase()
  if (kw) {
    list = list.filter((u) => [u.username, u.nickname, u.phone, u.email].some((f) => f.toLowerCase().includes(kw)))
  }
  if (query.status) list = list.filter((u) => u.status === String(query.status))
  if (query.deptId) list = list.filter((u) => u.deptId === String(query.deptId))
  if (query.gender) list = list.filter((u) => u.gender === String(query.gender))
  list = list.filter((u) => inCreatedRange(u, query))
  return list
}

// ---- 用户导入（.xlsx / CSV）：与后端 POST /users/import 同一契约，部分成功、失败行带原因 ----
// mock 无法生成 .xlsx：模板与 format=xlsx 的导出都返回 CSV 文本（前端按 Content-Type 决定扩展名）；导入则两种格式都能读。

const IMPORT_TEMPLATE =
  '\uFEFF账号,姓名,手机,邮箱,性别,部门,角色,岗位,备注\r\ndemo_user,演示用户,13800000000,demo@example.com,男,研发中心,运营;审计,,\r\n'

const IMPORT_COLS: Record<string, string> = {
  账号: 'username', 用户名: 'username', 姓名: 'nickname', 昵称: 'nickname', 手机: 'phone', 手机号: 'phone', 邮箱: 'email',
  性别: 'gender', 部门: 'dept', 角色: 'roles', 岗位: 'posts', 备注: 'remark',
}

/** 最小 CSV 解析：支持引号与引号内的逗号/换行 */
function parseCsv(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let cell = ''
  let quoted = false
  const src = text.replace(/^\uFEFF/, '')
  for (let i = 0; i < src.length; i++) {
    const c = src[i]
    if (quoted) {
      if (c === '"' && src[i + 1] === '"') {
        cell += '"'
        i++
      } else if (c === '"') quoted = false
      else cell += c
    } else if (c === '"') quoted = true
    else if (c === ',') {
      row.push(cell)
      cell = ''
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && src[i + 1] === '\n') i++
      row.push(cell)
      rows.push(row)
      row = []
      cell = ''
    } else cell += c
  }
  if (cell || row.length) {
    row.push(cell)
    rows.push(row)
  }
  return rows
}

function importRows(rows: string[][]): ImportResult {
  if (!rows.length) mockError(400, '文件为空')
  const col: Record<string, number> = {}
  rows[0].forEach((h, i) => {
    const k = IMPORT_COLS[h.trim()]
    if (k && !(k in col)) col[k] = i
  })
  if (!('username' in col)) mockError(400, '缺少“账号”列，请使用导入模板')
  const depts = new Map<string, readonly [string, string]>(Object.values(dept).map((d) => [d[1], d]))
  const roles = new Map(roleSeed.map((r) => [r.name, r.id]))
  const seen = new Map<string, number>()
  const result: ImportResult = { success: 0, failed: [] }
  rows.slice(1).forEach((rec, idx) => {
    const line = idx + 2
    const get = (k: string) => (k in col ? (rec[col[k]] ?? '').trim() : '')
    if (rec.every((c) => !c.trim())) return
    const username = get('username')
    const fail = (reason: string) => result.failed.push({ row: line, reason })
    if (!username) return fail('账号不能为空')
    if (!/^[A-Za-z0-9_.@-]{2,64}$/.test(username)) return fail('账号为 2-64 位字母、数字或 _ . @ -')
    if (seen.has(username.toLowerCase())) return fail(`账号与第 ${seen.get(username.toLowerCase())} 行重复`)
    if (store.all().some((u) => u.username.toLowerCase() === username.toLowerCase())) return fail('账号已存在')
    const phone = get('phone')
    if (phone && !/^\+?[0-9][0-9\- ]{3,19}$/.test(phone)) return fail('手机号格式不正确')
    const email = get('email')
    if (email && !/^[^@\s]+@[^@\s]+$/.test(email)) return fail('邮箱格式不正确')
    const gender = { '': '0', 未知: '0', 男: '1', 女: '2', 0: '0', 1: '1', 2: '2' }[get('gender') as '']
    if (gender === undefined) return fail('性别应为 男/女/未知')
    const d = get('dept') ? depts.get(get('dept')) : undefined
    if (get('dept') && !d) return fail(`部门“${get('dept')}”不存在`)
    const roleNames = get('roles').split(/[;,；，、|]/).map((s) => s.trim()).filter(Boolean)
    const missing = roleNames.find((n) => !roles.has(n))
    if (missing) return fail(`角色“${missing}”不存在`)
    seen.set(username.toLowerCase(), line)
    const now = new Date().toISOString()
    store.insert({
      id: genId(), username, nickname: get('nickname') || username, avatar: '', phone, email, gender,
      deptId: d?.[0] ?? '', deptName: d?.[1] ?? '', postIds: [], roleIds: roleNames.map((n) => roles.get(n)!), roleNames,
      status: '1', region: { province: '', city: '', district: '' }, remark: get('remark'), locked: false, lockedUntil: null,
      createdAt: now, updatedAt: now,
    })
    result.success++
  })
  return result
}

// 固定路径必须先于 createCrudMock 的 /users/:id 注册
defineMock([
  // 删除先进回收站（覆盖 createCrudMock 自带的删除）
  {
    method: 'DELETE',
    url: '/users/:id',
    handler: ({ params }) => {
      recycleUser(params.id)
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/users',
    handler: ({ query }) => {
      String(query.ids ?? '')
        .split(',')
        .filter(Boolean)
        .forEach(recycleUser)
      return null
    },
  },
  // 解除登录锁定（见 auth.mock 的连续密码错误锁定）
  {
    method: 'POST',
    url: '/users/:id/unlock',
    handler: ({ params }) => {
      const u = store.get(params.id)
      if (!u) mockError(404, 'Not found')
      unlockAccount(u.username)
      return null
    },
  },
  {
    method: 'GET',
    url: '/users/export',
    handler: ({ query }) => {
      const list = filtered(query)
      if (query.format !== 'xlsx') return list
      const cell = (v: string) => (/[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)
      const head = ['账号', '姓名', '手机', '邮箱', '性别', '部门', '角色', '备注', '状态', '创建时间']
      const gender: Record<string, string> = { '1': '男', '2': '女' }
      const lines = list.map((u) =>
        [u.username, u.nickname, u.phone, u.email, gender[u.gender] ?? '未知', u.deptName, u.roleNames.join(';'), u.remark,
          u.status === '1' ? '启用' : '停用', u.createdAt].map(cell).join(','),
      )
      return `\uFEFF${[head.join(','), ...lines].join('\r\n')}\r\n`
    },
  },
  {
    // 同步 / 异步分流：真实后端读 export.sync_limit（默认 5000），mock 用 20 让演示数据就能走到异步分支
    method: 'GET',
    url: '/users/export-plan',
    handler: ({ query }) => {
      const total = filtered(query).length
      return { total, syncLimit: MOCK_SYNC_LIMIT, async: total > MOCK_SYNC_LIMIT }
    },
  },
  {
    method: 'POST',
    url: '/users/export-async',
    handler: ({ query, body }) => {
      const list = filtered({ ...query, ...(body ?? {}) })
      const gender: Record<string, string> = { '1': '男', '2': '女' }
      const lines = list.map((u) =>
        [u.username, u.nickname, u.phone, u.email, gender[u.gender] ?? '未知', u.deptName, u.roleNames.join(';'), u.remark,
          u.status === '1' ? '启用' : '停用', u.createdAt].join(','),
      )
      const content = `\uFEFF账号,姓名,手机,邮箱,性别,部门,角色,备注,状态,创建时间\r\n${lines.join('\r\n')}\r\n`
      const day = new Date().toISOString().slice(0, 10).replace(/-/g, '')
      const j = createMockJob('user.export', `users-${day}.csv`, list.length, content)
      return { id: j.id, type: j.type, status: j.status }
    },
  },
  {
    method: 'GET',
    url: '/users/import-template',
    handler: () => IMPORT_TEMPLATE,
  },
  {
    method: 'POST',
    url: '/users/import',
    handler: async ({ body }): Promise<ImportResult> => {
      const file = body instanceof FormData ? body.get('file') : null
      if (!(file instanceof File)) mockError(400, '请选择要导入的文件')
      const ext = /\.([^.]+)$/.exec(file.name)?.[1]?.toLowerCase()
      if (ext === 'xls') mockError(400, '不支持旧版 .xls，请另存为 .xlsx 后再导入')
      if (ext !== 'csv' && ext !== 'xlsx') mockError(400, '仅支持 .xlsx 或 .csv 文件')
      if (ext === 'csv') return importRows(parseCsv(await file.text()))
      let rows: string[][]
      try {
        rows = await readXlsxRows(file)
      } catch {
        mockError(400, '无法解析 Excel 文件，请使用导入模板（.xlsx）')
      }
      return importRows(rows)
    },
  },
  {
    method: 'POST',
    url: '/users/:id/reset-password',
    handler: ({ params, body }) => {
      if (!store.get(params.id)) mockError(404, 'Not found')
      const pw = String(body?.password ?? '')
      if (!pw || new TextEncoder().encode(pw).length > 72) mockError(400, 'Password must be 1-72 bytes') // 无强度规则
      return null
    },
  },
  {
    method: 'PUT',
    url: '/users/:id/status',
    handler: ({ params, body }) => {
      const u = store.get(params.id)
      if (!u) mockError(404, 'Not found')
      if (u.username === 'admin') mockError(400, 'Built-in admin cannot be disabled')
      u.status = String(body?.status) === '1' ? '1' : '0'
      u.updatedAt = new Date().toISOString()
      return null
    },
  },
])

const store = createCrudMock<User>({
  base: '/users',
  seed,
  searchFields: ['username', 'nickname', 'phone', 'email'],
  filterFields: ['status', 'deptId', 'gender'],
  extraFilter: inCreatedRange,
})

function recycleUser(id: string) {
  const row = store.remove(id)
  if (row) moveToRecycle('user', row.nickname || row.username, row)
}

// 登录锁定时同步用户的 locked / lockedUntil（解锁与登录成功时清除）
onLockChange((username, until) => {
  const u = store.all().find((x) => x.username === username)
  if (!u) return
  u.locked = until !== null
  u.lockedUntil = until !== null ? new Date(until).toISOString() : null
})

registerRestorer('user', (row) => store.insert(row as User))
