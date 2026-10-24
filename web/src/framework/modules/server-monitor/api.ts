import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'

export interface DiskInfo {
  mount: string
  totalGb: number
  usedGb: number
}

export interface ServerSnapshot {
  ts: number
  cpu: { usage: number; cores: number; load1: number; load5: number; load15: number }
  memory: { totalGb: number; usedGb: number }
  disks: DiskInfo[]
  network: { inKbps: number; outKbps: number }
  runtime: { goroutines: number; heapMb: number; gcCount: number; gcPauseMs: number }
  info: { os: string; arch: string; goVersion: string; hostname: string; ip: string; startedAt: string; uptimeSec: number }
}

export type HealthStatus = 'ok' | 'warn' | 'fail'

/** 与后端 servermonitor.CheckNames 一致，也是页面展示顺序 */
export const HEALTH_ITEMS = ['db', 'cache', 'storage', 'migrations', 'plugins', 'disk'] as const
export type HealthItem = (typeof HEALTH_ITEMS)[number]

export interface HealthCheck {
  status: HealthStatus
  latencyMs: number
  message: string
}

/** GET /monitor/health：status 是各项里最差的 */
export interface HealthSnapshot {
  status: HealthStatus
  ts: number
  checks: Record<HealthItem, HealthCheck>
}

/** GET /monitor/client-errors：同指纹聚合为一行，hitCount 为累计次数 */
export interface ClientErrorRow {
  id: string
  userId: string
  username: string
  route: string
  message: string
  stack: string
  ua: string
  version: string
  fingerprint: string
  hitCount: number
  lastSeenAt: string
  createdAt: string
}

export interface ClientErrorQuery {
  page: number
  pageSize: number
  keyword?: string
}

/** 健康检查：refetchMs 为 0/false 时不轮询（页面不可见时由调用方传 false） */
export function useHealth(refetchMs: number | false) {
  return useQuery({
    queryKey: ['monitor', 'health'],
    queryFn: () => api.get<HealthSnapshot>('/monitor/health', undefined, { silent: true }),
    refetchInterval: refetchMs,
    staleTime: 0,
  })
}

export function useClientErrors(query: ClientErrorQuery) {
  return useQuery({
    queryKey: ['monitor', 'clientErrors', query],
    queryFn: () => api.get<Page<ClientErrorRow>>('/monitor/client-errors', { ...query }, { silent: true }),
    placeholderData: keepPreviousData,
  })
}
