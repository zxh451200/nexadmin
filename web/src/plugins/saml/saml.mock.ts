import { defineMock, genId, mockError } from '@/framework/request/mock'
import { mockIssueSession } from '@/framework/auth/auth.mock'
import type {
  ConnectionTestStart,
  GroupRoleMapping,
  IdPCandidate,
  IdPInfo,
  RolePreview,
  SamlEvent,
  SamlSession,
  SamlSource,
  SourceForm,
  SourceVersion,
  SPInfo,
  TestResult,
} from './api'

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

const entityXML = (entityID: string, sso: string, cert: string) => `  <EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="${entityID}">
    <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
      <KeyDescriptor use="signing">
        <ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#">
          <ds:X509Data><ds:X509Certificate>${cert}</ds:X509Certificate></ds:X509Data>
        </ds:KeyInfo>
      </KeyDescriptor>
      <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="${sso}"/>
      <SingleLogoutService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="${sso}/slo"/>
    </IDPSSODescriptor>
  </EntityDescriptor>
`

const CERT_B64 = 'MIIBszCCAVmgAwIBAgIUeE4Q1xwZ3J2d0u0UxW1w7qKkKnMwCgYIKoZIzj0EAwIw'

const IDP_METADATA = `<?xml version="1.0"?>
<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata">
${entityXML('https://idp.example.com/metadata', 'https://idp.example.com/sso', CERT_B64).trim()}
</EntityDescriptor>`

// 打包元数据（模拟 ADFS / 联邦导出）：一个 EntitiesDescriptor 里放两个 IdP。
const BUNDLE_METADATA = `<?xml version="1.0"?>
<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata">
${entityXML('https://idp.example.com/metadata', 'https://idp.example.com/sso', CERT_B64)}${entityXML('https://idp2.example.com/metadata', 'https://idp2.example.com/sso', CERT_B64)}</EntitiesDescriptor>`

const C1: IdPCandidate = {
  entityId: 'https://idp.example.com/metadata',
  ssoUrl: 'https://idp.example.com/sso',
  ssoBinding: 'redirect',
  certNotAfter: inDays(240),
  certCount: 1,
  certExpired: false,
  certExpiringSoon: false,
}

const C2: IdPCandidate = {
  entityId: 'https://idp2.example.com/metadata',
  ssoUrl: 'https://idp2.example.com/sso',
  ssoBinding: 'redirect',
  certNotAfter: inDays(12),
  certCount: 1,
  certExpired: false,
  certExpiringSoon: true,
}

const infoFor = (candidate: IdPCandidate, candidates: IdPCandidate[]): IdPInfo => ({
  ...candidate,
  sloUrl: `${candidate.ssoUrl}/slo`,
  sloBinding: 'redirect',
  metadataValidUntil: inDays(30),
  metadataCacheDuration: 'P1D',
  metadataCacheSeconds: 86_400,
  metadataExpired: false,
  metadataStale: false,
  candidates,
})

const SP = (id: string): SPInfo => ({
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
const ID_FED = '7610000000000000003'

const okta: SamlSource = {
  id: ID_OKTA,
  name: 'Okta（示例）',
  enabled: true,
  idpMetadata: IDP_METADATA,
  idpMetadataUrl: 'https://idp.example.com/metadata',
  idpEntityId: C1.entityId,
  spEntityIdOverride: '',
  externalBaseUrl: ORIGIN,
  metadataCaCert: '',
  metadataSignCert: '',
  attrUsername: 'uid',
  attrEmail: 'mail',
  attrNickname: 'displayName',
  attrGroups: 'groups',
  attrPhone: '',
  attrExternalId: '',
  autoCreate: true,
  matchExisting: false,
  matchProvisionedExternalId: false,
  defaultRole: 'guest',
  groupRoleMappings: [{ externalGroup: 'cn=ops,ou=groups', roleCodes: ['operator'] }],
  requireMappedGroup: false,
  syncRolesOnLogin: true,
  revokeRolesOnEmptyGroups: false,
  allowIdpInitiated: true,
  signAuthnRequest: false,
  nameIdFormat: '',
  requireEncryptedAssertion: false,
  signMetadata: false,
  allowSha1Signatures: false,
  requireSignedResponseForCBC: false,
  rejectUnknownConditions: false,
  forceAuthn: false,
  authnContextClassRef: '',
  requireAuthnContext: false,
  metadataRefreshedAt: ago(60 * 24 * 2),
  idp: infoFor(C1, [C1]),
  sp: SP(ID_OKTA),
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
  autoCreate: false,
  matchExisting: true,
  defaultRole: '',
  groupRoleMappings: [] as GroupRoleMapping[],
  syncRolesOnLogin: false,
  allowIdpInitiated: false,
  forceAuthn: true,
  authnContextClassRef: 'mfa',
  requireAuthnContext: true,
  metadataRefreshedAt: undefined,
  idp: infoFor(C1, [C1]),
  sp: SP(ID_AZURE),
  createdAt: ago(60 * 24 * 6),
  updatedAt: ago(60 * 24),
}

const federation: SamlSource = {
  ...okta,
  id: ID_FED,
  name: '联邦打包元数据（示例）',
  enabled: false,
  idpMetadata: BUNDLE_METADATA,
  idpMetadataUrl: 'https://federation.example.com/metadata',
  idpEntityId: C2.entityId,
  requireSignedResponseForCBC: true,
  rejectUnknownConditions: true,
  idp: infoFor(C2, [C1, C2]),
  sp: SP(ID_FED),
  createdAt: ago(60 * 24 * 3),
  updatedAt: ago(60 * 24),
}

let sources: SamlSource[] = [okta, azure, federation]

// 版本快照只保留可编辑配置，不把 idp/sp 等派生字段混进 SourceForm。
const configFor = (source: SamlSource): SourceForm => {
  const config = { ...source }
  for (const key of ['id', 'idp', 'sp', 'metadataRefreshedAt', 'createdAt', 'updatedAt']) {
    Reflect.deleteProperty(config, key)
  }
  return config
}

const versionsFor = (source: SamlSource): SourceVersion[] => {
  const current = configFor(source)
  return [
    {
      version: 2,
      config: current,
      operatorId: 'demo',
      operatorName: '演示管理员',
      reason: '更新身份源配置',
      createdAt: source.updatedAt,
    },
    {
      version: 1,
      config: { ...current, enabled: !current.enabled },
      operatorId: 'demo',
      operatorName: '演示管理员',
      reason: '初始导入',
      createdAt: source.createdAt,
    },
  ]
}

const eventsFor = (source: SamlSource): SamlEvent[] => [
  {
    id: `${source.id}-event-login`,
    type: 'login',
    result: 'success',
    operatorId: '',
    operatorName: '',
    userId: 'demo',
    correlationId: `${source.id}-correlation`,
    ip: '127.0.0.1',
    detail: `用户通过 ${source.idp.entityId} 完成单点登录`,
    createdAt: ago(15),
  },
  {
    id: `${source.id}-event-refresh`,
    type: 'metadata.refresh',
    result: 'success',
    operatorId: 'demo',
    operatorName: '演示管理员',
    userId: '',
    correlationId: `${source.id}-metadata`,
    ip: '127.0.0.1',
    detail: '元数据已刷新，配置校验通过',
    createdAt: ago(60 * 26),
  },
]

const sessionsBySource: Record<string, SamlSession[]> = Object.fromEntries(sources.map((source) => [source.id, [
  {
    id: `${source.id}-session-active`,
    userId: 'demo',
    sessionIndex: `_${source.id}`,
    expiresAt: inDays(1),
    lastSeenAt: ago(15),
  },
  {
    id: `${source.id}-session-revoked`,
    userId: 'demo',
    sessionIndex: `_${source.id}-old`,
    expiresAt: inDays(1),
    lastSeenAt: ago(60 * 26),
    revokedAt: ago(60 * 25),
  },
]]))

const find = (id: string) => sources.find((s) => s.id === id) ?? mockError(404, 'Not found')

function build(form: SourceForm, id: string, old?: SamlSource): SamlSource {
  if (!form.name?.trim()) mockError(400, 'name is required')
  if (!form.idpMetadata?.trim()) mockError(400, 'IdP metadata is required')
  const candidates = form.idpMetadata.includes('EntitiesDescriptor') ? [C1, C2] : [C1]
  if (form.enabled && candidates.length > 1 && !form.idpEntityId) mockError(400, '请先选择 Entity ID')
  const selected = candidates.find((c) => c.entityId === form.idpEntityId) ?? candidates[0]
  const now = new Date().toISOString()
  return {
    ...form,
    name: form.name.trim(),
    id,
    idpEntityId: selected.entityId,
    idp: infoFor(selected, candidates),
    metadataRefreshedAt: old?.metadataRefreshedAt,
    sp: SP(id),
    createdAt: old?.createdAt ?? now,
    updatedAt: now,
  }
}

const CHECKS = (s: SamlSource): TestResult => {
  const checks: TestResult['checks'] = [
    { key: 'metadata', ok: true, detail: `Entity ID: ${s.idp.entityId}` },
    {
      key: 'entity',
      ok: true,
      detail: `已选择 ${s.idp.entityId}（元数据共 ${s.idp.candidates?.length ?? 1} 个 IdP）`,
    },
    { key: 'ssoUrl', ok: !!s.idp.ssoUrl, detail: s.idp.ssoUrl },
    {
      key: 'idpCert',
      ok: s.idp.certCount > 0 && !s.idp.certExpired,
      warning: s.idp.certExpiringSoon,
      detail: s.idp.certNotAfter,
    },
    { key: 'spKey', ok: true, detail: 'SP 私钥可解密并解析（RSA）' },
    ...(s.requireEncryptedAssertion
      ? [{ key: 'assertionEncryption', ok: true, detail: 'RSA 私钥可解密 XML 加密断言' }]
      : []),
    { key: 'spCert', ok: true, detail: `到期时间 ${s.sp.certNotAfter ?? ''}` },
    { key: 'sloUrl', ok: !!s.idp.sloUrl, detail: s.idp.sloUrl || 'IdP 未提供单点登出端点，只能本地登出' },
    {
      key: 'roleMappings',
      ok: true,
      warning: s.syncRolesOnLogin && s.revokeRolesOnEmptyGroups,
      detail:
        s.syncRolesOnLogin && s.revokeRolesOnEmptyGroups
          ? '空组断言会收回本来源授予的角色；IdP 漏发组属性时也会收权'
          : `已配置 ${s.groupRoleMappings.length} 条精确组映射`,
    },
    ...(s.matchProvisionedExternalId
      ? [{
          key: 'externalIdMatching',
          ok: !!s.attrExternalId,
          detail: s.attrExternalId
            ? `按稳定属性 ${s.attrExternalId} 精确匹配 SCIM 预配账号`
            : '已开启外部 ID 精确匹配，但未配置属性名',
        }]
      : []),
    ...(s.metadataSignCert
      ? [{ key: 'metadataSignature', ok: true, detail: '已保存的元数据由钉住的证书签名' }]
      : []),
    ...(s.metadataCaCert
      ? [{ key: 'metadataCa', ok: true, detail: '拉取元数据时只信任配置的内网 CA' }]
      : []),
  ]
  if (s.idpMetadataUrl) checks.push({ key: 'metadataUrl', ok: true, detail: '远程元数据与已保存内容一致' })
  if (s.authnContextClassRef)
    checks.push({
      key: 'authnContext',
      ok: true,
      warning: !s.requireAuthnContext,
      detail: s.requireAuthnContext
        ? `断言必须命中其中之一：${s.authnContextClassRef}`
        : `只在 AuthnRequest 里声明，未强制校验断言：${s.authnContextClassRef}`,
    })
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

const CERT_NEXT = `-----BEGIN CERTIFICATE-----
MIIBszCCAVmgAwIBAgIUeE4Q1xwZ3J2d0u0UxW1w7qKkKnMwCgYIKoZIzj0EAwIw
TkVYVC1DRVJUSUZJQ0FURS1NT0NL
-----END CERTIFICATE-----
`

const mockCertDetails = (notAfter: string) => ({
  subject: 'CN=nexadmin-saml-saml',
  issuer: 'CN=nexadmin-saml-saml',
  serialNumber: '7612345678901234567',
  notBefore: new Date(Date.now() - 365 * 24 * 60 * 60_000).toISOString(),
  notAfter,
  publicKeyAlgorithm: 'RSA',
  signatureAlgorithm: 'SHA256-RSA',
  sha256Fingerprint: 'AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99',
  sha1Thumbprint: 'AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD',
})

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
    method: 'POST',
    url: '/plugins/saml/sources/:id/import-sp-key',
    handler: ({ params, body }) => {
      const cert = String(body?.cert ?? '').trim()
      const key = String(body?.key ?? '').trim()
      if (!cert.includes('BEGIN CERTIFICATE') || !key.includes('PRIVATE KEY')) {
        mockError(400, 'saml: 证书或私钥不是 PEM 格式')
      }
      find(params.id)
      return { cert, notAfter: inDays(3650) }
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
    method: 'POST',
    url: '/plugins/saml/sources/:id/role-preview',
    handler: ({ params, body }) => {
      const source = find(params.id)
      const groups = Array.isArray(body?.groups) ? body.groups.map(String) : []
      if (groups.length > 1000) mockError(400, 'saml: 预览组数量不能超过 1000')
      const matchedGroups = source.groupRoleMappings
        .map((mapping) => mapping.externalGroup)
        .filter((group) => groups.includes(group))
      const desiredRoles = [...new Set(source.groupRoleMappings.flatMap((mapping) =>
        groups.includes(mapping.externalGroup) ? mapping.roleCodes : [],
      ))]
      const allowed = !source.requireMappedGroup || matchedGroups.length > 0
      const result: RolePreview = {
        allowed,
        userScoped: !!body?.userId,
        syncRolesOnLogin: source.syncRolesOnLogin,
        revokeRolesOnEmptyGroups: source.revokeRolesOnEmptyGroups,
        matchedGroups,
        desiredRoles,
        previousRoles: [],
        wouldAdd: source.syncRolesOnLogin && !!body?.userId ? desiredRoles : [],
        wouldRemove: [],
        unavailableRoles: [],
        reason: allowed ? undefined : '当前账号不在允许登录的组内',
      }
      return result
    },
  },
  {
    method: 'GET',
    url: '/plugins/saml/sources/:id/sp-cert',
    handler: ({ params }) => {
      find(params.id)
      const notAfter = inDays(3600)
      return { cert: CERT, notAfter, details: mockCertDetails(notAfter) }
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/rotate-sp-cert',
    handler: ({ params }) => {
      find(params.id)
      // 两段式轮换第一步：只发布，不改变当前签名证书。
      const notAfter = inDays(3600)
      const nextNotAfter = inDays(3650)
      return {
        cert: CERT, notAfter, details: mockCertDetails(notAfter),
        nextCert: CERT_NEXT, nextNotAfter, nextDetails: mockCertDetails(nextNotAfter),
      }
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/promote-sp-cert',
    handler: ({ params }) => {
      const notAfter = inDays(3650)
      const row = { ...find(params.id), sp: { ...find(params.id).sp, certNotAfter: notAfter } }
      sources = sources.map((s) => (s.id === row.id ? row : s))
      const prevNotAfter = inDays(3600)
      const prevUntil = inDays(7)
      return {
        cert: CERT_NEXT, notAfter, details: mockCertDetails(notAfter),
        prevCert: CERT, prevNotAfter, prevDetails: mockCertDetails(prevNotAfter), prevUntil,
      }
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/refresh',
    handler: ({ params }) => {
      const row = { ...find(params.id), metadataRefreshedAt: new Date().toISOString() }
      sources = sources.map((s) => (s.id === row.id ? row : s))
      return { changed: false, source: row }
    },
  },
  {
    method: 'GET',
    url: '/plugins/saml/sources/:id/versions',
    handler: ({ params }) => versionsFor(find(params.id)),
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/rollback',
    handler: ({ params, body }) => {
      const source = find(params.id)
      const version = Number(body?.version)
      const target = versionsFor(source).find((item) => item.version === version)
      if (!target) mockError(404, 'saml: 版本不存在')
      const row = build(target.config, source.id, source)
      sources = sources.map((s) => (s.id === row.id ? row : s))
      return row
    },
  },
  {
    method: 'GET',
    url: '/plugins/saml/sources/:id/events',
    handler: ({ params }) => eventsFor(find(params.id)),
  },
  {
    method: 'GET',
    url: '/plugins/saml/sources/:id/sessions',
    handler: ({ params }) => {
      find(params.id)
      return sessionsBySource[params.id] ?? []
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/sessions/:sessionId/revoke',
    handler: ({ params }) => {
      find(params.id)
      const rows = sessionsBySource[params.id] ?? []
      const session = rows.find((item) => item.id === params.sessionId)
      if (!session) mockError(404, 'saml: 会话不存在')
      session.revokedAt = new Date().toISOString()
      return null
    },
  },
  {
    method: 'POST',
    url: '/plugins/saml/sources/:id/test/start',
    handler: ({ params, body }) => {
      find(params.id)
      const mode = String(body?.mode ?? '')
      if (mode !== 'sso' && mode !== 'slo') mockError(400, 'saml: 测试模式无效')
      const nameId = String(body?.nameId ?? '').trim()
      if (mode === 'slo' && !nameId) mockError(400, 'saml: 单点登出测试需要 Name ID')
      const result: ConnectionTestStart = {
        testId: genId(),
        mode,
        binding: 'redirect',
        url: mode === 'sso' ? 'https://idp.example.com/sso' : 'https://idp.example.com/sso/slo',
        relayState: params.id,
      }
      return result
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
      const isBundle = (xml || url).includes('EntitiesDescriptor') || url.includes('federation')
      const candidates = isBundle ? [C1, C2] : [C1]
      const idp: IdPInfo = isBundle
        ? {
            entityId: '',
            ssoUrl: '',
            ssoBinding: '',
            sloUrl: '',
            certCount: 0,
            certExpired: false,
            certExpiringSoon: false,
            metadataValidUntil: inDays(30),
            metadataCacheDuration: 'P1D',
            metadataCacheSeconds: 86_400,
            metadataExpired: false,
            metadataStale: false,
            candidates,
          }
        : infoFor(C1, candidates)
      return { metadata: xml || (isBundle ? BUNDLE_METADATA : IDP_METADATA), idp }
    },
  },
])
