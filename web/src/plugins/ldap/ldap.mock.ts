import { defineMock, mockError } from '@/framework/request/mock'

// LDAP/AD 登录插件的开发态假接口，语义与 plugins/ldap 服务端一致：
// 口令永不回显（只回 hasBindPassword）；保存/测试时口令留空沿用旧值；
// 测试带表单时测「当前填写值」不落库，不带时测已保存配置。

interface Row {
  enabled: boolean
  host: string
  port: number
  useTLS: boolean
  startTLS: boolean
  bindDN: string
  bindPassword: string
  baseDN: string
  userFilter: string
  attrUsername: string
  attrEmail: string
  attrNickname: string
  matchExisting: boolean
  autoCreate: boolean
  defaultRole: string
  timeoutMs: number
  caCert: string
}

const row: Row = {
  enabled: false,
  host: '',
  port: 389,
  useTLS: false,
  startTLS: false,
  bindDN: '',
  bindPassword: '',
  baseDN: '',
  userFilter: '(uid=%s)',
  attrUsername: 'uid',
  attrEmail: 'mail',
  attrNickname: 'cn',
  matchExisting: false,
  autoCreate: false,
  defaultRole: '',
  timeoutMs: 5000,
  caCert: '',
}

const view = () => {
  const { bindPassword, ...rest } = row
  return { ...rest, hasBindPassword: bindPassword !== '' }
}

function apply(f: Partial<Row>) {
  const next: Row = { ...row, ...f }
  if (next.enabled && (!next.host.trim() || !next.baseDN.trim())) mockError(400, '请填写 LDAP 地址和 Base DN')
  if (next.enabled && !next.userFilter.includes('%s')) mockError(400, '过滤器必须包含 %s 占位符')
  if (f.bindPassword !== undefined && f.bindPassword !== '') next.bindPassword = f.bindPassword // 留空沿用旧值
  return next
}

// 与后端 configForTest 同一套默认值兜底（测试不做 enabled 校验、不落库）。
function forTest(f: Partial<Row>): Row {
  const c = { ...row, ...f }
  return {
    ...c,
    host: (c.host ?? '').trim(),
    baseDN: (c.baseDN ?? '').trim(),
    userFilter: (c.userFilter ?? '').trim() || '(uid=%s)',
    port: c.port && c.port > 0 ? c.port : 389,
    timeoutMs: c.timeoutMs && c.timeoutMs > 0 ? c.timeoutMs : 5000,
  }
}

defineMock([
  { method: 'GET', url: '/plugins/ldap/config', handler: () => view() },
  {
    method: 'PUT',
    url: '/plugins/ldap/config',
    handler: (ctx) => {
      Object.assign(row, apply(ctx.body as Partial<Row>))
      return view()
    },
  },
  {
    method: 'POST',
    url: '/plugins/ldap/test',
    handler: (ctx) => {
      const f = ctx.body ? forTest(ctx.body as Partial<Row>) : row
      if (!f.host || !f.baseDN) mockError(400, '请填写 LDAP 地址和 Base DN')
      if (f.useTLS && f.startTLS) mockError(400, '连接测试失败，请检查地址、账号与加密设置')
      // 空跑：假接口永远「连通」，真实拨号在后端
      return { ok: true }
    },
  },
])
