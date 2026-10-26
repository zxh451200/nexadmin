import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export type PluginEdition = 'free' | 'pro'
export type PluginStatus = 'active' | 'unlicensed' | 'expired' | 'invalid' | 'revoked' | 'disabled'

export const PLUGIN_EDITIONS: PluginEdition[] = ['free', 'pro']
export const PLUGIN_STATUSES: PluginStatus[] = ['active', 'unlicensed', 'expired', 'invalid', 'revoked', 'disabled']

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

/* ---- 插件市场 ---- */

export interface MarketEntry {
  id: string
  name: string
  description?: string
  category?: string
  edition: PluginEdition
  version?: string
  /** GitHub owner/name（monorepo 也填它） */
  repo: string
  ref?: string
  /** monorepo 里本插件所在子目录；单插件仓库留空 */
  subdir?: string
  /** zip 直链（OSS/自建镜像）；设置后安装器优先于 GitHub */
  url?: string
  homepage?: string
  /** 展示用价格："待定"、"¥299"、"免费" */
  price?: string
  author?: string
  installed: boolean
  installedVersion?: string
  /** 运行期授权状态；未装为空 */
  status?: string | null
}

export type MarketJobState = 'running' | 'restarting' | 'done' | 'failed'

export interface MarketJob {
  id: string
  kind: 'install' | 'remove' | 'rebuild'
  state: MarketJobState
  log: string[]
}

export const MARKET_KEY = ['plugins', 'market'] as const

export function useMarket() {
  return useQuery({ queryKey: MARKET_KEY, queryFn: () => api.get<MarketEntry[]>('/plugins/market') })
}

/** 轮询任务进度；running/restarting 时每 1.5s 拉一次。404（自重启后任务消失）不重试，由调用方按重启分支处理。 */
export function useMarketJob(id: string | null) {
  return useQuery({
    queryKey: ['plugins', 'market', 'job', id],
    enabled: !!id,
    retry: false,
    refetchInterval: (q) => {
      const s = q.state.data?.state
      return s === 'running' || s === 'restarting' ? 1500 : false
    },
    queryFn: () => api.get<MarketJob>(`/plugins/market/jobs/${encodeURIComponent(id ?? '')}`),
  })
}

export function useInstallPlugin() {
  return useMutation({
    mutationFn: ({ id, force }: { id: string; force?: boolean }) =>
      api.post<{ jobId: string }>('/plugins/market/install', { id, force }),
  })
}

export function useRemovePlugin() {
  return useMutation({
    mutationFn: ({ id, purge }: { id: string; purge?: boolean }) =>
      api.post<{ jobId: string }>('/plugins/market/remove', { id, purge }),
  })
}

export function useRebuildServer() {
  return useMutation({
    mutationFn: () => api.post<{ jobId: string }>('/plugins/market/rebuild', {}),
  })
}

/** 服务是否已恢复（自重启等待用）；任何错误都视为未恢复。 */
export async function serverAlive(): Promise<boolean> {
  try {
    await api.get('/system/status')
    return true
  } catch {
    return false
  }
}
