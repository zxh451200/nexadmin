import { useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { DictItem } from '@/framework/request/types'
import type { FormField } from './api'
import { usesOptions } from './schema'

/**
 * 设计器里字典绑定的字段还没有发布快照（options 为空）。这里按字典当前的启用项把 options 展开，
 * 供预览与显隐条件的取值下拉使用；只用于展示，不会写回草稿。queryKey 与框架的字典缓存一致。
 */
export function useResolvedFields(fields: FormField[]): FormField[] {
  const codes = useMemo(() => [...new Set(fields.filter((f) => usesOptions(f) && f.dictType).map((f) => f.dictType as string))], [fields])
  const items = useQueries({
    queries: codes.map((code) => ({
      queryKey: ['dict-items', code],
      queryFn: () => api.get<DictItem[]>(`/dicts/${code}/items`),
      staleTime: 5 * 60 * 1000,
    })),
    combine: (results) => results.map((r) => r.data),
  })
  return useMemo(() => {
    const byCode = new Map(codes.map((c, i) => [c, items[i]]))
    return fields.map((f) => {
      const list = f.dictType && usesOptions(f) ? byCode.get(f.dictType) : undefined
      return list && list.length > 0 ? { ...f, options: list.map((d) => ({ label: d.label, value: String(d.value) })) } : f
    })
  }, [fields, codes, items])
}
