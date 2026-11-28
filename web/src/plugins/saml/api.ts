import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import { useAuth } from '@/framework/auth/useAuth'

export interface GroupRoleMapping {
  externalGroup: string
  roleCodes: string[]
}

export interface IdPCandidate {
  entityId: string
  ssoUrl: string
  ssoBinding: string
  sloUrl?: string
  sloBinding?: string
  certNotAfter?: string
  certCount: number
  certExpired: boolean
  certExpiringSoon: boolean
}

export interface IdPInfo {
  entityId: string
  ssoUrl: string
  ssoBinding: string
  sloUrl: string
  sloBinding?: string
  certNotAfter?: string
  certCount: number
  certExpired: boolean
  certExpiringSoon: boolean
  metadataUrl?: string
  metadataValidUntil?: string
  metadataCacheDuration?: string
  metadataCacheSeconds?: number
  metadataExpired: boolean
  metadataStale: boolean
  candidates?: IdPCandidate[]
  error?: string
}

export interface SPInfo {
  entityId: string
  acsUrl: string
  sloUrl: string
  metadataUrl: string
  loginUrl: string
  certNotAfter?: string
  certExpired: boolean
  certExpiringSoon: boolean
}

export interface SamlSource {
  id: string
  name: string
  enabled: boolean
  idpMetadata: string
  idpMetadataUrl: string
  idpEntityId: string
  spEntityIdOverride: string
  externalBaseUrl: string
  metadataCaCert: string
  metadataSignCert: string
  attrUsername: string
  attrEmail: string
  attrNickname: string
  attrGroups: string
  attrPhone: string
  attrExternalId: string
  autoCreate: boolean
  matchExisting: boolean
  matchProvisionedExternalId: boolean
  defaultRole: string
  groupRoleMappings: GroupRoleMapping[]
  requireMappedGroup: boolean
  syncRolesOnLogin: boolean
  revokeRolesOnEmptyGroups: boolean
  allowIdpInitiated: boolean
  signAuthnRequest: boolean
  nameIdFormat: string
  requireEncryptedAssertion: boolean
  signMetadata: boolean
  allowSha1Signatures: boolean
  requireSignedResponseForCBC: boolean
  rejectUnknownConditions: boolean
  forceAuthn: boolean
  authnContextClassRef: string
  requireAuthnContext: boolean
  metadataRefreshedAt?: string
  idp: IdPInfo
  sp: SPInfo
  createdAt: string
  updatedAt: string
}

export interface SourceForm {
  name: string
  enabled: boolean
  idpMetadata: string
  idpMetadataUrl: string
  idpEntityId: string
  spEntityIdOverride: string
  externalBaseUrl: string
  metadataCaCert: string
  metadataSignCert: string
  attrUsername: string
  attrEmail: string
  attrNickname: string
  attrGroups: string
  attrPhone: string
  attrExternalId: string
  autoCreate: boolean
  matchExisting: boolean
  matchProvisionedExternalId: boolean
  defaultRole: string
  groupRoleMappings: GroupRoleMapping[]
  requireMappedGroup: boolean
  syncRolesOnLogin: boolean
  revokeRolesOnEmptyGroups: boolean
  allowIdpInitiated: boolean
  signAuthnRequest: boolean
  nameIdFormat: string
  requireEncryptedAssertion: boolean
  signMetadata: boolean
  allowSha1Signatures: boolean
  requireSignedResponseForCBC: boolean
  rejectUnknownConditions: boolean
  forceAuthn: boolean
  authnContextClassRef: string
  requireAuthnContext: boolean
}

export interface Check {
  key: string
  ok: boolean
  warning?: boolean
  detail?: string
}

export interface TestResult {
  ok: boolean
  checks: Check[]
}

export interface RolePreview {
  allowed: boolean
  userScoped: boolean
  syncRolesOnLogin: boolean
  revokeRolesOnEmptyGroups: boolean
  matchedGroups: string[]
  desiredRoles: string[]
  previousRoles: string[]
  wouldAdd: string[]
  wouldRemove: string[]
  unavailableRoles: string[]
  reason?: string
}

export interface MetadataPreview {
  metadata: string
  idp: IdPInfo
}

export interface CertDetails {
  subject: string
  issuer: string
  serialNumber: string
  notBefore: string
  notAfter: string
  publicKeyAlgorithm: string
  signatureAlgorithm: string
  sha256Fingerprint: string
  sha1Thumbprint: string
}

export interface SPCert {
  cert: string
  notAfter: string
  details: CertDetails
  /** 已发布、尚未切换的新证书（两段式轮换第一步） */
  nextCert?: string
  nextNotAfter?: string
  nextDetails?: CertDetails
  /** 切换后保留的上一代证书（仅用于解密，到期自动不再公布） */
  prevCert?: string
  prevNotAfter?: string
  prevUntil?: string
  prevDetails?: CertDetails
}

export interface SourceVersion {
  version: number
  config: SourceForm
  operatorId: string
  operatorName: string
  reason: string
  createdAt: string
}

export interface SamlEvent {
  id: string
  type: string
  result: string
  operatorId: string
  operatorName: string
  userId: string
  correlationId: string
  ip: string
  detail: string
  createdAt: string
}

export interface SamlSession {
  id: string
  userId: string
  sessionIndex: string
  expiresAt: string
  lastSeenAt: string
  revokedAt?: string
}

export interface ConnectionTestStart {
  testId: string
  mode: string
  binding: 'redirect' | 'post'
  url: string
  samlRequest?: string
  relayState?: string
}

const BASE = '/plugins/saml'
const KEY = ['plugin_saml'] as const

export function useSources() {
  const user = useAuth((s) => s.user)
  return useQuery({
    queryKey: [...KEY, 'sources', user?.tenantId, user?.id],
    enabled: !!user,
    queryFn: ({ signal }) => api.get<SamlSource[]>(`${BASE}/sources`, undefined, { signal }),
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveSource() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: SourceForm }) =>
      id ? api.put<SamlSource>(`${BASE}/sources/${id}`, data) : api.post<SamlSource>(`${BASE}/sources`, data),
    onSuccess: invalidate,
  })
}

export function useDeleteSource() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`${BASE}/sources/${id}`), onSuccess: invalidate })
}

export function useSetEnabled() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.put(`${BASE}/sources/${id}/enabled`, { enabled }),
    onSuccess: invalidate,
  })
}

export function useTestSource() {
  return useMutation({
    mutationFn: (id: string) => api.post<TestResult>(`${BASE}/sources/${id}/test`, {}, { silent: true }),
  })
}

export function previewRoles(id: string, input: { groups: string[]; userId?: string }) {
  return api.post<RolePreview>(`${BASE}/sources/${id}/role-preview`, input, { silent: true })
}

export function getSPCert(id: string) {
  return api.get<SPCert>(`${BASE}/sources/${id}/sp-cert`, undefined, { silent: true })
}

export function rotateSPCert(id: string) {
  return api.post<SPCert>(`${BASE}/sources/${id}/rotate-sp-cert`, {}, { silent: true })
}

/** 切换到已发布的新证书（两段式轮换第二步），旧证书保留 7 天做解密兜底。 */
export function promoteSPCert(id: string) {
  return api.post<SPCert>(`${BASE}/sources/${id}/promote-sp-cert`, {}, { silent: true })
}

export function importSPKey(id: string, input: { cert: string; key: string }) {
  return api.post<SPCert>(`${BASE}/sources/${id}/import-sp-key`, input, { silent: true })
}

export function refreshMetadata(id: string) {
  return api.post<{ changed: boolean; source: SamlSource }>(`${BASE}/sources/${id}/refresh`, {}, { silent: true })
}

export function inspectMetadata(input: { url?: string; xml?: string; caCert?: string; signCert?: string }) {
  return api.post<MetadataPreview>(`${BASE}/metadata/inspect`, input, { silent: true })
}

export function useSourceVersions(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'versions', id],
    enabled: !!id,
    queryFn: ({ signal }) => api.get<SourceVersion[]>(`${BASE}/sources/${id}/versions`, undefined, { signal }),
  })
}

export function useSourceEvents(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'events', id],
    enabled: !!id,
    queryFn: ({ signal }) => api.get<SamlEvent[]>(`${BASE}/sources/${id}/events`, undefined, { signal }),
  })
}

export function useSourceSessions(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'sessions', id],
    enabled: !!id,
    queryFn: ({ signal }) => api.get<SamlSession[]>(`${BASE}/sources/${id}/sessions`, undefined, { signal }),
  })
}

export function useRollbackSource() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, version, reason }: { id: string; version: number; reason: string }) =>
      api.post<SamlSource>(`${BASE}/sources/${id}/rollback`, { version, reason }),
    onSuccess: invalidate,
  })
}

export function useRevokeSession() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, sessionId }: { id: string; sessionId: string }) =>
      api.post(`${BASE}/sources/${id}/sessions/${sessionId}/revoke`, {}),
    onSuccess: invalidate,
  })
}

export function startConnectionTest(id: string, input: { mode: 'sso' | 'slo'; nameId?: string }) {
  return api.post<ConnectionTestStart>(`${BASE}/sources/${id}/test/start`, input, { silent: true })
}
