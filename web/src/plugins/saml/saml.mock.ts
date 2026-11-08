import { defineMock, genId, mockError } from '@/framework/request/mock'
import { mockIssueSession } from '@/framework/auth/auth.mock'
import type { GroupRoleMapping, IdPInfo, SamlSource, SourceForm, SPInfo, TestResult } from './api'

/**
 * 登录页的 SAML 入口在真后端是匿名路由（plugins/saml/plugin.go 用 d.Public.Group("/auth/saml")
 * 注册 sources / exchange / metadata / login / acs / slo）。mock 侧要同时满足两件事：
 * 路径在 PUBLIC_PREFIXES 白名单里，且有对应规则。只补白名单不补规则会退成
 * 「Mock not found」404，面板与交换流程靠各自 .catch 静默失败——这块 UI 在开发态永远跑不到。
 *
 * 交换码模拟的是「IdP 已认定身份、ACS 回调带码回来」这一步：映射到 mock 的 demo 账号，
 * 令牌与用户表都由 auth.mock 统一签发（不在此抄第二份结构）。
 */
defineMock([
  {
    method: 'GET',
    url: '/auth/saml/sources',
    handler: () => [{ id: 'idp-demo', name: '企业单点登录（示例）' }],
  },
  {
    method: 'POST',
    url: '/auth/saml/exchange',
    handler: ({ body }) => {
      const code = String(body?.code ?? '')
      // 一次性码：真后端用它换一次登录态，空码与重复消费都该拒绝而不是发令牌。
      if (code.length < 6) return mockError(400, 'invalid exchange code')
      const session = mockIssueSession('demo')
      if (!session) return mockError(401, 'session unavailable')
      return { ...session, samlSourceId: 'idp-demo' }
    },
  },
])

const ORIGIN = 'https://admin.example.com'
const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()
const inDays = (days: number) => new Date(Date.now() + days * 24 * 60 * 60_000).toISOString()

const IDP_METADATA = `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
        <ds:X509Data><ds:X509Certificate>MIIBszCCAVmgAwIBAgIUeE4Q1xwZ3J2d0u0UxW1w7qKkKnMwCgYIKoZIzj0EAwIw</ds:X509Certificate></ds:X509Data>
      </ds:KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/sso"/>
    <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.com/slo"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

const IDP: IdPInfo = {
  entityId: 'https://idp.example.com/metadata',
  ssoUrl: 'https://idp.example.com/sso',
  ssoBinding: 'redirect',
  sloUrl: 'https://idp.example.com/slo',
  certNotAfter: inDays(240),
  certCount: 1,
  certExpired: false,
  certExpiringSoon: false,
}

const sp = (id: string): SPInfo => ({
  entityId: `${ORIGIN}/api/v1/auth/saml/${id}/metadata`,
  acsUrl: `${ORIGIN}/api/v1/auth/saml/${id}/acs`,
  sloUrl: `${ORIGIN}/api/v1/auth/saml/${id}/slo`,
  metadataUrl: `${ORIGIN}/api/v1/auth/saml/${id}/metadata`,
  loginUrl: `${ORIGIN}/api/v1/auth/saml/${id}/login`,
  certNotAfter: inDays(3600),
  certExpired: false,
  certExpiringSoon: false,
})

const ID_OKTA = '7610000000000000001'
const ID_AZURE = '7610000000000000002'

const okta: SamlSource = {
  id: ID_OKTA,
  name: 'Okta（示例）',
  enabled: true,
  idpMetadata: IDP_METADATA,
  idpMetadataUrl: 'https://idp.example.com/metadata',
  attrUsername: 'uid',
  attrEmail: 'mail',
  attrNickname: 'displayName',
  attrGroups: 'groups',
  attrPhone: '',
  autoCreate: true,
  matchExisting: false,
  defaultRole: 'guest',
  groupRoleMappings: [{ externalGroup: 'cn=ops,ou=groups', roleCodes: ['operator'] }],
  requireMappedGroup: false,
  allowIdpInitiated: true,
  signAuthnRequest: false,
  nameIdFormat: '',
  idp: IDP,
  sp: sp(ID_OKTA),
  createdAt: ago(60 * 24 * 20),
  updatedAt: ago(60 * 24 * 2),
}

const azure: SamlSource = {
  ...okta,
  id: ID_AZURE,
  name: 'Azure AD（示例）',
  enabled: false,
  idpMetadataUrl: '',
  attrUsername: 'http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name',
  attrEmail: 'http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress',
  attrNickname: 'http://schemas.microsoft.com/identity/claims/displayname',
  attrGroups: '',
  attrPhone: '',
  autoCreate: false,
  matchExisting: true,
  defaultRole: '',
  groupRoleMappings: [] as GroupRoleMapping[],
  requireMappedGroup: false,
  allowIdpInitiated: false,
  idp: { ...IDP, certNotAfter: inDays(12), certExpiringSoon: true },
  sp: sp(ID_AZURE),
  createdAt: ago(60 * 24 * 6),
  updatedAt: ago(60 * 24),
}

let sources: SamlSource[] = [okta, azure]

const find = (id: string) => sources.find((s) => s.id === id) ?? mockError(404, 'Not found')

function build(form: SourceForm, id: string, old?: SamlSource): SamlSource {
  if (!form.name?.trim()) mockError(400, 'name is required')
  if (!form.idpMetadata?.trim()) mockError(400, 'IdP metadata is required')
  const now = new Date().toISOString()
  return {
    ...form,
    name: form.name.trim(),
    id,
    idp: IDP,
    sp: sp(id),
    createdAt: old?.createdAt ?? now,
    updatedAt: now,
  }
}

const CHECKS = (s: SamlSource): TestResult => {
  const checks: TestResult['checks'] = [
    { key: 'metadata', ok: true, detail: `Entity ID: ${s.idp.entityId}` },
    { key: 'ssoUrl', ok: !!s.idp.ssoUrl, detail: s.idp.ssoUrl },
    {
      key: 'idpCert',
      ok: s.idp.certCount > 0 && !s.idp.certExpired,
      warning: s.idp.certExpiringSoon,
      detail: s.idp.certNotAfter,
    },
    { key: 'spKey', ok: true, detail: 'SP 私钥可解密并解析' },
    { key: 'spCert', ok: true, detail: `到期时间 ${s.sp.certNotAfter ?? ''}` },
  ]
  if (s.idpMetadataUrl) checks.push({ key: 'metadataUrl', ok: true, detail: '远程元数据与已保存内容一致' })
  if (!s.enabled) checks.push({ key: 'enabled', ok: true, warning: true, detail: '该身份源当前未启用，测试与登录入口不会出现在登录页' })
  return { ok: checks.every((c) => c.ok), checks }
}

const CERT = `-----BEGIN CERTIFICATE-----
MIIBszCCAVmgAwIBAgIUeE4Q1xwZ3J2d0u0UxW1w7qKkKnMwCgYIKoZIzj0EAwIw
gY8xCzAJBgNVBAYTAlVTMRIwEAYDVQQIDAlXaXNjb25zaW4xEDAOBgNVBAcMB01h
ZGlzb24xFTATBgNVBAoMDE5leEFkbWluIFNQMRcwFQYDVQQDDA5uZXhhZG1pbi1z
YW1sLXNwMB4XDTI2MDEwMTAwMDAwMFoXDTM2MDEwMTAwMDAwMFowgY8xCzAJBgNV
-----END CERTIFICATE-----
`

defineMock([
  { method: 'GET', url: '/plugins/saml/sources', handler: () => sources },
  {
    method: 'POST',
    url: '/plugins/saml/sources',
    handler: ({ body }) => {
      const row = build(body as SourceForm, genId())
      sources = [...sources, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/saml/sources/:id',
    handler: ({ params, body }) => {
      const old = find(params.id)
      const row = build(body as SourceForm, old.id, old)
      sources = sources.map((s) => (s.id === row.id ? row : s))
      return row
    },
  },
  {
    method: 'PUT',
    url: '/plugins/saml/sources/:id/enabled',
    handler: ({ params, body }) => {
      const row = { ...find(params.id), enabled: !!body?.enabled }
      sources = sources.map((s) => (s.id === row.id ? row : s))
      return row
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/saml/sources/:id',
    handler: ({ params }) => {
      find(params.id)
      sources = sources.filter((s) => s.id !== params.id)
      return null
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/test',
    handler: ({ params }) => CHECKS(find(params.id)),
  },
  {
    method: 'GET',
    url: '/plugins/saml/sources/:id/sp-cert',
    handler: ({ params }) => {
      find(params.id)
      return { cert: CERT, notAfter: inDays(3600) }
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/metadata/inspect',
    handler: ({ body }) => {
      const xml = String(body?.xml ?? '').trim()
      const url = String(body?.url ?? '').trim()
      if (!xml && !url) mockError(400, '请提供元数据 XML 或 URL')
      if (xml && !xml.includes('IDPSSODescriptor')) mockError(400, '元数据缺少 IDPSSODescriptor')
      return { metadata: xml || IDP_METADATA, idp: { ...IDP, metadataUrl: url } }
    },
  },
])
