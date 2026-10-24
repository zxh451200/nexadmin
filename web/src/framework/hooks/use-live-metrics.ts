import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import { usePageLive, useRefreshOnResume } from './use-page-active'

/**
 * 实时指标：按间隔轮询接口，并把最近 max 次的结果留作曲线数据。
 * intervalMs 为 0 表示暂停自动刷新（仍可手动 refetch）。
 */
export function useLiveMetrics<T>(url: string, intervalMs: number, max = 30) {
  const live = usePageLive()
  const q = useQuery({
    queryKey: ['monitor', url],
    queryFn: () => api.get<T>(url, undefined, { silent: true }),
    refetchInterval: intervalMs > 0 && live ? intervalMs : false,
    staleTime: 0,
  })
  useRefreshOnResume(live, q.refetch, intervalMs > 0)
  const [history, setHistory] = useState<T[]>([])
  useEffect(() => {
    if (q.data) setHistory((h) => [...h, q.data as T].slice(-max))
  }, [q.dataUpdatedAt]) // eslint-disable-line react-hooks/exhaustive-deps
  return { latest: q.data, history, isLoading: q.isLoading, isFetching: q.isFetching, refetch: q.refetch }
}
