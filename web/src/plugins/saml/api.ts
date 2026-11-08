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
  certNotAfter?: string
  certCount: number
  certExpired: boolean
  certExpiringSoon: boolean
  metadataUrl?: string
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
  attrUsername: string
  attrEmail: string
  attrNickname: string
  attrGroups: string
  attrPhone: string
  autoCreate: boolean
  matchExisting: boolean
  defaultRole: string
  groupRoleMappings: GroupRoleMapping[]
  requireMappedGroup: boolean
  allowIdpInitiated: boolean
  signAuthnRequest: boolean
  nameIdFormat: string
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
  attrUsername: string
  attrEmail: string
  attrNickname: string
  attrGroups: string
  attrPhone: string
  autoCreate: boolean
  matchExisting: boolean
  defaultRole: string
  groupRoleMappings: GroupRoleMapping[]
  requireMappedGroup: boolean
  allowIdpInitiated: boolean
  signAuthnRequest: boolean
  nameIdFormat: string
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

export interface MetadataPreview {
  metadata: string
  idp: IdPInfo
}

export interface SPCert {
  cert: string
  notAfter: string
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

export function getSPCert(id: string) {
  return api.get<SPCert>(`${BASE}/sources/${id}/sp-cert`, undefined, { silent: true })
}

export function rotateSPCert(id: string) {
  return api.post<SPCert>(`${BASE}/sources/${id}/rotate-sp-cert`, {}, { silent: true })
}

export function refreshMetadata(id: string) {
  return api.post<{ changed: boolean; source: SamlSource }>(`${BASE}/sources/${id}/refresh`, {}, { silent: true })
}

export function inspectMetadata(input: { url?: string; xml?: string }) {
  return api.post<MetadataPreview>(`${BASE}/metadata/inspect`, input, { silent: true })
}
