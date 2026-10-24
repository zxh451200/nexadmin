import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type PolicyMode = 'optional' | 'all' | 'roles'
export type RuleAction = 'allow' | 'deny'
export type RuleScope = 'all' | 'super'
export type UserStatus = 'enabled' | 'pending' | 'grace' | 'expired'

export interface TwoFactorStatus {
  enabled: boolean
  pending: boolean
  recoveryLeft: number
  required: boolean
  graceEndsAt: string | null
  canDisable: boolean
}

export interface SetupResult {
  secret: string
  uri: string
}

export interface Policy {
  mode: PolicyMode
  roles: string[]
  graceDays: number
}

export interface TwoFactorUser {
  userId: string
  username: string
  status: UserStatus
  enabledAt: string | null
  graceUntil: string | null
}

export interface IpRule {
  id: string
  action: RuleAction
  cidr: string
  scope: RuleScope
  remark: string
  enabled: boolean
  createdAt: string
}

export interface IpRuleForm {
  action: RuleAction
  cidr: string
  scope: RuleScope
  remark: string
  enabled: boolean
}

export interface IpRuleList {
  list: IpRule[]
  clientIp: string
}

export interface SecurityLog {
  id: string
  operatorName: string
  action: string
  target: string
  detail: string
  ip: string
  createdAt: string
}

const BASE = '/plugins/security'
const KEY = ['plugin_security'] as const

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

// ---- 个人 2FA ----

export function useStatus() {
  return useQuery({ queryKey: [...KEY, 'status'], queryFn: () => api.get<TwoFactorStatus>(`${BASE}/2fa/status`) })
}

export function useSetup() {
  return useMutation({ mutationFn: () => api.post<SetupResult>(`${BASE}/2fa/setup`) })
}

export function useEnable() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (code: string) => api.post<{ recoveryCodes: string[] }>(`${BASE}/2fa/enable`, { code }),
    onSuccess: invalidate,
  })
}

export function useDisable() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (p: { code: string; kind?: string }) => api.post<null>(`${BASE}/2fa/disable`, p),
    onSuccess: invalidate,
  })
}

export function useRegenerate() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (code: string) => api.post<{ recoveryCodes: string[] }>(`${BASE}/2fa/recovery-codes`, { code }),
    onSuccess: invalidate,
  })
}

// ---- 策略 / 用户 ----

export function usePolicy() {
  return useQuery({ queryKey: [...KEY, 'policy'], queryFn: () => api.get<Policy>(`${BASE}/policy`) })
}

export function useSavePolicy() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (p: Policy) => api.put<Policy>(`${BASE}/policy`, p), onSuccess: invalidate })
}

export function useUsers(query: PageQuery) {
  return useQuery({
    queryKey: [...KEY, 'users', query],
    queryFn: () => api.get<Page<TwoFactorUser>>(`${BASE}/2fa/users`, query),
    placeholderData: keepPreviousData,
  })
}

export function useResetUser() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (userId: string) => api.post<null>(`${BASE}/2fa/users/${userId}/reset`), onSuccess: invalidate })
}

// ---- IP 规则 ----

export function useRules() {
  return useQuery({ queryKey: [...KEY, 'rules'], queryFn: () => api.get<IpRuleList>(`${BASE}/ip-rules`) })
}

export function useSaveRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, data }: { id?: string; data: IpRuleForm }) =>
      id ? api.put<IpRule>(`${BASE}/ip-rules/${id}`, data) : api.post<IpRule>(`${BASE}/ip-rules`, data),
    onSuccess: invalidate,
  })
}

export function useSetRuleEnabled() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.put<IpRule>(`${BASE}/ip-rules/${id}/enabled`, { enabled }),
    onSuccess: invalidate,
  })
}

export function useDeleteRule() {
  const invalidate = useInvalidate()
  return useMutation({ mutationFn: (id: string) => api.del<null>(`${BASE}/ip-rules/${id}`), onSuccess: invalidate })
}

// ---- 日志 ----

export function useLogs(query: PageQuery) {
  return useQuery({
    queryKey: [...KEY, 'logs', query],
    queryFn: () => api.get<Page<SecurityLog>>(`${BASE}/logs`, query),
    placeholderData: keepPreviousData,
  })
}
