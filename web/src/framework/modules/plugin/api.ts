import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export type PluginEdition = 'free' | 'pro'
export type PluginStatus = 'active' | 'unlicensed' | 'expired' | 'invalid' | 'revoked'

export const PLUGIN_EDITIONS: PluginEdition[] = ['free', 'pro']
export const PLUGIN_STATUSES: PluginStatus[] = ['active', 'unlicensed', 'expired', 'invalid', 'revoked']

/** 到期前多少天提示 */
export const EXPIRING_DAYS = 30

export interface PluginInfo {
  id: string
  name: string
  version: string
  edition: PluginEdition
  description?: string
  status: PluginStatus
  customer?: string
  /** 授权序列号；旧授权没有，无序列号的授权不能被吊销清单单独吊销 */
  serial?: string
  /** ISO 时间；免费插件或未授权时为空 */
  expiresAt?: string | null
  reason?: string
}

const KEY = ['plugins', 'list'] as const

export function usePlugins() {
  return useQuery({ queryKey: KEY, queryFn: () => api.get<PluginInfo[]>('/plugins') })
}

export function useImportLicense() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, content }: { id: string; content: string }) =>
      api.post<{ needRestart: boolean }>(`/plugins/${encodeURIComponent(id)}/license`, { content }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}

/** 距离到期的天数（向上取整）；无到期时间返回 null */
export function daysUntil(expiresAt?: string | null, now = Date.now()): number | null {
  if (!expiresAt) return null
  const ts = new Date(expiresAt).getTime()
  if (Number.isNaN(ts)) return null
  return Math.ceil((ts - now) / 86_400_000)
}

/** 授权有效且 30 天内到期 */
export function isExpiringSoon(p: PluginInfo, now = Date.now()): boolean {
  if (p.status !== 'active') return false
  const d = daysUntil(p.expiresAt, now)
  return d !== null && d >= 0 && d <= EXPIRING_DAYS
}
