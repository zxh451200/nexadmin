import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { usePageLive, useRefreshOnResume } from '@/framework/hooks/use-page-active'
import type { QueryResult } from './api'

/**
 * 取一张图表的数据。refetchMs > 0 时按间隔自动刷新，页面不在前台（切走标签/浏览器标签页隐藏）时暂停，
 * 回到前台立即补刷一次。
 */
export function useChartResult(keyParts: readonly unknown[], fn: () => Promise<QueryResult>, opts: { enabled?: boolean; refetchMs?: number } = {}) {
  const live = usePageLive()
  const auto = !!opts.refetchMs && opts.refetchMs > 0
  const q = useQuery({
    queryKey: ['plugin_report', 'result', ...keyParts],
    queryFn: fn,
    enabled: opts.enabled ?? true,
    placeholderData: keepPreviousData,
    retry: false,
    refetchInterval: auto && live ? opts.refetchMs : false,
  })
  useRefreshOnResume(live, q.refetch, auto)
  return q
}
