import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { CasConfig, IdentitySource, LdapConfig, SourceForm, TestResult } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const ldapDemo: LdapConfig = {
  url: 'ldaps://ldap.example.com:636',
  startTls: false,
  bindDn: 'cn=svc-nexadmin,ou=services,dc=example,dc=com',
  hasBindPassword: true, // 与真实接口一致：口令永远不回显
  baseDn: 'ou=people,dc=example,dc=com',
  userFilter: '(&(objectClass=person)(uid={username}))',
  attrUsername: 'uid',
  attrNickname: 'cn',
  attrEmail: 'mail',
  attrPhone: 'mobile',
  timeoutSec: 10,
  matchExisting: true,
  autoCreate: false,
}

const casDemo: CasConfig = {
  serverUrl: 'https://cas.example.com/cas',
  version: '3',
  serviceBaseUrl: 'https://admin.example.com',
  attrNickname: 'displayName',
  timeoutSec: 10,
  matchExisting: true,
  autoCreate: false,
}

const entry = (id: string) => `https://admin.example.com/api/v1/plugins/identity/cas/login?source=${id}`
const logout = (id: string) => `https://admin.example.com/api/v1/plugins/identity/cas/logout?source=${id}`

const ID_LDAP = '7510000000000000001'
const ID_CAS = '7510000000000000002'

let sources: IdentitySource[] = [
  { id: ID_LDAP, type: 'ldap', name: '公司 AD', enabled: true, config: ldapDemo, createdAt: ago(60 * 24 * 12), updatedAt: ago(60 * 24 * 2) },
  {
    id: ID_CAS, type: 'cas', name: '统一认证 CAS', enabled: false, config: casDemo,
    entryUrl: entry(ID_CAS), logoutUrl: logout(ID_CAS), createdAt: ago(60 * 24 * 5), updatedAt: ago(60 * 24),
  },
]

const find = (id: string) => sources.find((s) => s.id === id) ?? mockError(404, 'Not found')

/** 与服务端一致的口令处理：绝不回显 bindPassword；更新时留空沿用旧值。 */
function sanitize(type: SourceForm['type'], cfg: SourceForm['config'], old?: IdentitySource['config']) {
  if (type === 'ldap') {
    const { bindPassword, ...rest } = cfg as LdapConfig
    const prev = old as LdapConfig | null | undefined
    const has = !!rest.bindDn && (!!bindPassword || !!prev?.hasBindPassword)
    return { ...rest, hasBindPassword: has } as LdapConfig
  }
  return cfg as CasConfig
}

function validate(f: SourceForm, editing = false) {
  if (!f.name?.trim()) mockError(400, 'name is required')
  if (f.type !== 'ldap' && f.type !== 'cas') mockError(400, 'type must be ldap or cas')
  if (f.type === 'ldap') {
    const c = f.config as LdapConfig
    if (!/^ldaps?:\/\/[^/?#@\s]+$/i.test(c.url ?? '')) mockError(400, 'LDAP address must be ldap:// or ldaps://host[:port]')
    if (!c.baseDn?.trim()) mockError(400, 'baseDn is required')
    if (!c.userFilter?.includes('{username}')) mockError(400, 'userFilter must contain {username}')
    if (c.bindDn && !c.bindPassword && !editing) mockError(400, 'bind password is required when bindDn is set')
  } else {
    const c = f.config as CasConfig
    if (!/^https?:\/\/[^/?#@\s]+(\/[^?#]*)?$/i.test(c.serverUrl ?? '')) mockError(400, 'serverUrl must be an http(s) URL')
    if (!/^https?:\/\/[^/?#@\s]+$/i.test(c.serviceBaseUrl ?? '')) mockError(400, 'serviceBaseUrl must be an http(s) origin')
  }
}

const withLinks = (s: IdentitySource): IdentitySource => {
  if (s.type !== 'cas') return s
  const base = (s.config as CasConfig).serviceBaseUrl.replace(/\/$/, '')
  return {
    ...s,
    entryUrl: `${base}/api/v1/plugins/identity/cas/login?source=${s.id}`,
    logoutUrl: `${base}/api/v1/plugins/identity/cas/logout?source=${s.id}`,
  }
}

defineMock([
  { method: 'GET', url: '/plugins/identity/sources', handler: () => sources },
  { method: 'GET', url: '/plugins/identity/sources/:id', handler: ({ params }) => find(params.id) },
  {
    method: 'POST',
    url: '/plugins/identity/sources',
    handler: ({ body }) => {
      validate(body)
      const now = new Date().toISOString()
      const row = withLinks({
        id: genId(), type: body.type, name: String(body.name).trim(), enabled: !!body.enabled,
        config: sanitize(body.type, body.config), createdAt: now, updatedAt: now,
      })
      sources = [...sources, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/identity/sources/:id',
    handler: ({ params, body }) => {
      const old = find(params.id)
      validate({ ...body, type: old.type }, true)
      const row = withLinks({
        ...old, name: String(body.name).trim(), enabled: !!body.enabled,
        config: sanitize(old.type, body.config, old.config), updatedAt: new Date().toISOString(),
      })
      sources = sources.map((s) => (s.id === old.id ? row : s))
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/identity/sources/:id/enabled',
    handler: ({ params, body }) => {
      const row = { ...find(params.id), enabled: !!body?.enabled }
      sources = sources.map((s) => (s.id === row.id ? row : s))
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/identity/sources/:id',
    handler: ({ params }) => {
      find(params.id)
      sources = sources.filter((s) => s.id !== params.id)
      return null
    },
  },
  {
    method: 'POST',
    url: '/plugins/identity/sources/:id/test-connection',
    handler: ({ params }): TestResult => {
      find(params.id)
      return { ok: true }
    },
  },
  {
    method: 'POST',
    url: '/plugins/identity/sources/:id/test-login',
    handler: ({ params, body }): TestResult => {
      const s = find(params.id)
      if (s.type !== 'ldap') mockError(400, 'only LDAP sources support user login test')
      if (!body?.username || !body?.password) mockError(400, 'username and password are required')
      if (body.password !== 'demo') return { ok: false, stage: 'login', message: 'invalid credentials' }
      return {
        ok: true, stage: 'login', dn: `uid=${body.username},ou=people,dc=example,dc=com`,
        username: body.username, nickname: 'Demo User', email: `${body.username}@example.com`, phone: '13800000000',
      }
    },
  },
])
