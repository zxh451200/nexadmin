import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page, PageQuery } from '@/framework/request/types'

export type AuditAction = 'create' | 'update' | 'delete'

export interface AuditLog {
  id: string
  table: string
  rowPk: string
  action: AuditAction
  /** "0" 表示系统（取不到当前用户） */
  operatorId: string
  operatorName: string
  tenantId: string
  /** 变化字段名摘要（逗号分隔） */
  fields: string
  createdAt: string
}

export interface FieldChange {
  old: unknown
  new: unknown
}

export interface AuditLogDetail extends AuditLog {
  changes: Record<string, FieldChange>
}

export interface AuditConfig {
  tables: string[]
  available: string[]
}

export interface LogQuery extends PageQuery {
  table?: string
  operator?: string
  action?: string
  from?: string
  to?: string
}

const BASE = '/plugins/audit'
const KEY = ['plugin_audit'] as const

export function useLogs(query: LogQuery) {
  return useQuery({
    queryKey: [...KEY, 'logs', query],
    queryFn: () => api.get<Page<AuditLog>>(`${BASE}/logs`, query),
    placeholderData: keepPreviousData,
  })
}

export function useLogDetail(id?: string) {
  return useQuery({
    queryKey: [...KEY, 'log', id],
    queryFn: () => api.get<AuditLogDetail>(`${BASE}/logs/${id}`),
    enabled: !!id,
  })
}

export function useLogTables() {
  return useQuery({
    queryKey: [...KEY, 'log-tables'],
    queryFn: () => api.get<string[]>(`${BASE}/log-tables`),
  })
}

export function useConfig(enabled = true) {
  return useQuery({
    queryKey: [...KEY, 'config'],
    queryFn: () => api.get<AuditConfig>(`${BASE}/config`),
    enabled,
  })
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useSaveConfig() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (tables: string[]) => api.put<AuditConfig>(`${BASE}/config`, { tables }),
    onSuccess: invalidate,
  })
}

export function useCleanLogs() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (days: number) => api.del<{ deleted: number }>(`${BASE}/logs`, { days }),
    onSuccess: invalidate,
  })
}
