import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { DictItem } from '@/framework/request/types'

/** 字典值 → 用户可见文本：导出、标题这类需要纯文本的地方用它，别把枚举码直接上屏。
 *  查不到就原样返回，宁可多一个能读懂的值，也不要在 CSV 里留空白格。 */
export function dictText(items: DictItem[] | undefined, value: string | number | null | undefined): string {
  if (value === null || value === undefined || value === '') return ''
  return items?.find((d) => String(d.value) === String(value))?.label ?? String(value)
}

/** 拉取字典项（react-query 缓存 5 分钟） */
export function useDictItems(dict: string | undefined) {
  return useQuery({
    queryKey: ['dict-items', dict],
    queryFn: () => api.get<DictItem[]>(`/dicts/${dict}/items`),
    enabled: !!dict,
    staleTime: 5 * 60 * 1000,
  })
}
