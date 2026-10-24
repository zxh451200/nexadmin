import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { LoginContinuation } from '@/framework/auth/useAuth'
import { useAuth } from '@/framework/auth/useAuth'

export function resumeCASLogin(): Promise<LoginContinuation> | null {
  const code = sessionStorage.getItem('nex-identity-cas-mfa')
  if (!code) return null
  // 失败也不重放 code；用户重新发起 CAS 登录即可。
  sessionStorage.removeItem('nex-identity-cas-mfa')
  return api.post<LoginContinuation>('/plugins/identity/cas/mfa/exchange', { code }, { silent: true })
}

export type SourceType = 'ldap' | 'cas'

export interface GroupRoleMapping {
  [key: string]: unknown
  externalGroup: string
  roleCodes: string[]
}

interface GroupMappingConfig {
  [key: string]: unknown
  attrGroups?: string
  groupRoleMappings?: GroupRoleMapping[]
  requireMappedGroup?: boolean
}

/** LDAP 配置。bindPassword 只在提交时使用：留空表示沿用旧值；服务端永远不会回显它。 */
export interface LdapConfig extends GroupMappingConfig {
  directorySync?: {
    [key: string]: unknown
    enabled: boolean
    filter: string
    subjectAttr: string
    /** 停用“已从目录消失”的账号；服务端默认关，且只在完整同步成功后生效。 */
    deactivateMissing?: boolean
  }
  url: string
  startTls: boolean
  /** 管理员显式确认：ldap:// 且未启用 StartTLS 时口令明文传输。服务端会把 ldaps / StartTLS 下的该标记清掉。 */
  allowPlaintext?: boolean
  caCert?: string
  bindDn?: string
  bindPassword?: string
  hasBindPassword?: boolean
  baseDn: string
  userFilter: string
  attrUsername: string
  attrNickname?: string
  attrEmail?: string
  attrPhone?: string
  timeoutSec: number
  matchExisting: boolean
  autoCreate: boolean
  defaultRole?: string
}

export interface CasConfig extends GroupMappingConfig {
  serverUrl: string
  version: '2' | '3'
  serviceBaseUrl: string
  caCert?: string
  attrUsername?: string
  attrNickname?: string
  attrEmail?: string
  attrPhone?: string
  timeoutSec: number
  matchExisting: boolean
  autoCreate: boolean
  defaultRole?: string
}

export interface IdentitySource {
  id: string
  type: SourceType
  name: string
  enabled: boolean
  config: LdapConfig | CasConfig | null
  /** CAS：可复制的登录入口 / 登出入口 */
  entryUrl?: string
  logoutUrl?: string
  createdAt: string
  updatedAt: string
}

export interface SourceForm {
  type: SourceType
  name: string
  enabled: boolean
  config: LdapConfig | CasConfig
}

export interface TestResult {
  ok: boolean
  stage?: 'connect' | 'bind' | 'search' | 'login' | 'validate'
  message?: string
  dn?: string
  username?: string
  nickname?: string
  email?: string
  phone?: string
}

const BASE = '/plugins/identity'
const KEY = ['plugin_identity'] as const

export function useSources() {
  const user = useAuth((s) => s.user)
  return useQuery({
    queryKey: [...KEY, 'sources', user?.tenantId, user?.id],
    enabled: !!user,
    queryFn: ({ signal }) => api.get<IdentitySource[]>(`${BASE}/sources`, undefined, { signal }),
  })
}

export interface DirectorySyncSummary {
  jobId?: string
  status?: string
  scanned?: number
  created?: number
  updated?: number
  /** 本次因“已不在目录里”而停用的账号数（仅 deactivateMissing 开启时非零）。 */
  deactivated?: number
  /** 是否完整跑完；中途失败时为 false。 */
  complete?: boolean
  error?: string
  startedAt?: string
  finishedAt?: string
}

export const getDirectorySync = (id: string, signal: AbortSignal) =>
  api.get<DirectorySyncSummary | null>(`${BASE}/sources/${encodeURIComponent(id)}/sync`, undefined, { signal, silent: true })

export const startDirectorySync = (id: string, signal: AbortSignal) =>
  api.post<{ jobId: string }>(`${BASE}/sources/${encodeURIComponent(id)}/sync`, {}, { signal, silent: true })

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveSource() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: SourceForm }) =>
      id ? api.put<IdentitySource>(`${BASE}/sources/${id}`, data) : api.post<IdentitySource>(`${BASE}/sources`, data),
    onSuccess: invalidate,
  })
}

export function useSetEnabled() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.put<IdentitySource>(`${BASE}/sources/${id}/enabled`, { enabled }),
    onSuccess: invalidate,
  })
}

export function useDeleteSource() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del(`${BASE}/sources/${id}`), onSuccess: invalidate })
}

export function useTestConnection() {
  return useMutation({ mutationFn: (id: string) => api.post<TestResult>(`${BASE}/sources/${id}/test-connection`) })
}

export function useTestLogin() {
  return useMutation({
    mutationFn: ({ id, username, password }: { id: string; username: string; password: string }) =>
      api.post<TestResult>(`${BASE}/sources/${id}/test-login`, { username, password }),
  })
}
