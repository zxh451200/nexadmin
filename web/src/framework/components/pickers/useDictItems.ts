import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { DictItem } from '@/framework/request/types'

/** 拉取字典项（react-query 缓存 5 分钟） */
export function useDictItems(dict: string | undefined) {
  return useQuery({
    queryKey: ['dict-items', dict],
    queryFn: () => api.get<DictItem[]>(`/dicts/${dict}/items`),
    enabled: !!dict,
    staleTime: 5 * 60 * 1000,
  })
}
