import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { IpRule, IpRuleForm, Policy, SecurityLog, TwoFactorStatus, TwoFactorUser } from './api'

// 演示规则：验证码固定为 123456（真实后端校验 RFC 6238 动态码）。
const DEMO_CODE = '123456'
const CLIENT_IP = '203.0.113.9'
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

let status: TwoFactorStatus = { enabled: false, pending: false, recoveryLeft: 0, required: false, graceEndsAt: null, canDisable: true }
let policy: Policy = { mode: 'optional', roles: [], graceDays: 7 }

let users: TwoFactorUser[] = [
  { userId: '7430000000000000001', username: 'alice', status: 'enabled', enabledAt: ago(60 * 24 * 3), graceUntil: null },
  { userId: '7430000000000000002', username: 'bob', status: 'grace', enabledAt: null, graceUntil: ago(-60 * 24 * 4) },
  { userId: '7430000000000000003', username: 'carol', status: 'expired', enabledAt: null, graceUntil: ago(60 * 24) },
  { userId: '7430000000000000004', username: 'dave', status: 'pending', enabledAt: null, graceUntil: ago(-60 * 24) },
]

let rules: IpRule[] = [
  { id: '7431000000000000001', action: 'deny', cidr: '198.51.100.0/24', scope: 'all', remark: '已知扫描源', enabled: true, createdAt: ago(600) },
  { id: '7431000000000000002', action: 'allow', cidr: '203.0.113.0/24', scope: 'super', remark: '办公网（仅超管）', enabled: true, createdAt: ago(300) },
  { id: '7431000000000000003', action: 'deny', cidr: '2001:db8::/32', scope: 'all', remark: '', enabled: false, createdAt: ago(100) },
]

let logs: SecurityLog[] = [
  { id: '7432000000000000001', operatorName: 'admin', action: 'create_ip_rule', target: '198.51.100.0/24', detail: 'deny/all enabled=true', ip: CLIENT_IP, createdAt: ago(600) },
  { id: '7432000000000000002', operatorName: 'admin', action: 'reset_2fa', target: 'carol', detail: 'userId=7430000000000000003', ip: CLIENT_IP, createdAt: ago(200) },
  { id: '7432000000000000003', operatorName: 'admin', action: 'save_policy', target: 'optional', detail: 'roles= graceDays=7', ip: CLIENT_IP, createdAt: ago(120) },
]

const log = (action: string, target: string, detail = '') => {
  logs = [{ id: genId(), operatorName: 'admin', action, target, detail, ip: CLIENT_IP, createdAt: new Date().toISOString() }, ...logs]
}

const recoveryCodes = () => Array.from({ length: 10 }, (_, i) => `mock${i}-code${i}`)

// ---- 极简 IPv4 CIDR 匹配，仅用于演示防自锁；IPv6 与完整语义由后端负责 ----
const v4 = (s: string) => s.split('.').reduce((n, p) => n * 256 + Number(p), 0)
const isV4 = (s: string) => /^\d{1,3}(\.\d{1,3}){3}$/.test(s)
function matches(cidr: string, ip: string): boolean {
  const [base, bits = '32'] = cidr.split('/')
  if (!isV4(base) || !isV4(ip)) return base === ip
  const shift = 32 - Number(bits)
  return Math.floor(v4(base) / 2 ** shift) === Math.floor(v4(ip) / 2 ** shift)
}
function allowed(list: IpRule[], scope: 'all' | 'super', ip: string): boolean {
  const on = list.filter((r) => r.enabled && r.scope === scope)
  if (on.some((r) => r.action === 'deny' && matches(r.cidr, ip))) return false
  const allows = on.filter((r) => r.action === 'allow')
  return allows.length === 0 || allows.some((r) => matches(r.cidr, ip))
}
function guard(next: IpRule[]) {
  if (allowed(rules, 'all', CLIENT_IP) && !allowed(next, 'all', CLIENT_IP)) mockError(409, `this change would lock out your current IP (${CLIENT_IP})`, 'SECURITY_SELF_LOCKOUT')
}
function validateRule(f: IpRuleForm) {
  if (f.action !== 'allow' && f.action !== 'deny') mockError(400, 'action must be allow or deny')
  if (f.scope !== 'all' && f.scope !== 'super') mockError(400, 'scope must be all or super')
  if (!/^[0-9a-fA-F:.]+(\/\d{1,3})?$/.test(f.cidr?.trim() ?? '')) mockError(400, 'invalid IP or CIDR')
}
const findRule = (id: string) => rules.find((r) => r.id === id) ?? mockError(404, 'Not found')

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  { method: 'GET', url: '/plugins/security/2fa/status', handler: () => status },
  {
    method: 'POST',
    url: '/plugins/security/2fa/setup',
    handler: () => {
      if (status.enabled) mockError(409, 'two-factor authentication is already enabled')
      status = { ...status, pending: true }
      return { secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP', uri: 'otpauth://totp/NexAdmin:admin?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=NexAdmin&algorithm=SHA1&digits=6&period=30' }
    },
  },
  {
    method: 'POST',
    url: '/plugins/security/2fa/enable',
    handler: ({ body }) => {
      if (!status.pending) mockError(400, 'no pending setup, start setup first')
      if (body?.code !== DEMO_CODE) mockError(400, 'invalid verification code')
      status = { ...status, enabled: true, pending: false, recoveryLeft: 10 }
      log('enable_2fa', 'admin')
      return { recoveryCodes: recoveryCodes() }
    },
  },
  {
    method: 'POST',
    url: '/plugins/security/2fa/disable',
    handler: ({ body }) => {
      if (!status.canDisable) mockError(403, 'two-factor authentication is required by policy')
      if (body?.code !== DEMO_CODE) mockError(400, 'invalid verification code')
      status = { ...status, enabled: false, pending: false, recoveryLeft: 0 }
      log('disable_2fa', 'admin')
      return null
    },
  },
  {
    method: 'POST',
    url: '/plugins/security/2fa/recovery-codes',
    handler: ({ body }) => {
      if (body?.code !== DEMO_CODE) mockError(400, 'invalid verification code')
      status = { ...status, recoveryLeft: 10 }
      return { recoveryCodes: recoveryCodes() }
    },
  },
  { method: 'GET', url: '/plugins/security/policy', handler: () => policy },
  {
    method: 'PUT',
    url: '/plugins/security/policy',
    handler: ({ body }) => {
      const p = body as Policy
      if (!['optional', 'all', 'roles'].includes(p?.mode)) mockError(400, 'invalid mode')
      if (!(p.graceDays >= 1 && p.graceDays <= 90)) mockError(400, 'graceDays must be between 1 and 90')
      if (p.mode === 'roles' && !(p.roles?.length > 0)) mockError(400, 'select at least one role')
      policy = { mode: p.mode, roles: p.mode === 'roles' ? p.roles : [], graceDays: p.graceDays }
      status = { ...status, required: p.mode !== 'optional', canDisable: p.mode === 'optional' }
      log('save_policy', p.mode, `roles=${policy.roles.join(',')} graceDays=${p.graceDays}`)
      return policy
    },
  },
  {
    method: 'GET',
    url: '/plugins/security/2fa/users',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      return paginate(users.filter((u) => !kw || u.username.toLowerCase().includes(kw)), query)
    },
  },
  {
    method: 'POST',
    url: '/plugins/security/2fa/users/:id/reset',
    handler: ({ params }) => {
      const u = users.find((x) => x.userId === params.id) ?? mockError(404, 'Not found')
      users = users.filter((x) => x.userId !== params.id)
      log('reset_2fa', u.username, `userId=${u.userId}`)
      return null
    },
  },
  { method: 'GET', url: '/plugins/security/ip-rules', handler: () => ({ list: rules, clientIp: CLIENT_IP }) },
  {
    method: 'POST',
    url: '/plugins/security/ip-rules',
    handler: ({ body }) => {
      validateRule(body)
      const rule: IpRule = { ...body, cidr: body.cidr.trim(), id: genId(), createdAt: new Date().toISOString() }
      guard([...rules, rule])
      rules = [...rules, rule]
      log('create_ip_rule', rule.cidr, `${rule.action}/${rule.scope} enabled=${rule.enabled}`)
      return rule
    },
  },
  {
    method: 'PUT',
    url: '/plugins/security/ip-rules/:id',
    handler: ({ params, body }) => {
      validateRule(body)
      const old = findRule(params.id)
      const rule: IpRule = { ...old, ...body, cidr: body.cidr.trim() }
      guard(rules.map((r) => (r.id === old.id ? rule : r)))
      rules = rules.map((r) => (r.id === old.id ? rule : r))
      log('update_ip_rule', rule.cidr, `${rule.action}/${rule.scope} enabled=${rule.enabled}`)
      return rule
    },
  },
  {
    method: 'PUT',
    url: '/plugins/security/ip-rules/:id/enabled',
    handler: ({ params, body }) => {
      const old = findRule(params.id)
      const rule: IpRule = { ...old, enabled: !!body?.enabled }
      guard(rules.map((r) => (r.id === old.id ? rule : r)))
      rules = rules.map((r) => (r.id === old.id ? rule : r))
      log('update_ip_rule', rule.cidr, `enabled=${rule.enabled}`)
      return rule
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/security/ip-rules/:id',
    handler: ({ params }) => {
      const old = findRule(params.id)
      guard(rules.filter((r) => r.id !== old.id))
      rules = rules.filter((r) => r.id !== old.id)
      log('delete_ip_rule', old.cidr, `${old.action}/${old.scope}`)
      return null
    },
  },
  { method: 'GET', url: '/plugins/security/logs', handler: ({ query }) => paginate(logs, query) },
])
