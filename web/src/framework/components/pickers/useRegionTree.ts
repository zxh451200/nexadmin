import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { RegionNode } from '@/framework/request/types'

export type RegionValue = { province?: string; city?: string; district?: string }

/** 省市区树，内置数据基本不变，永久缓存 */
export function useRegionTree(level: 2 | 3 = 3) {
  return useQuery({
    queryKey: ['regions-tree', level],
    queryFn: () => api.get<RegionNode[]>('/regions/tree', { level }),
    staleTime: Infinity,
    gcTime: Infinity,
  })
}

export function findRegionPath(tree: RegionNode[] | undefined, value: RegionValue | undefined): RegionNode[] {
  if (!tree || !value) return []
  const out: RegionNode[] = []
  const p = tree.find((n) => n.code === value.province)
  if (!p) return out
  out.push(p)
  const c = p.children?.find((n) => n.code === value.city)
  if (!c) return out
  out.push(c)
  const d = c.children?.find((n) => n.code === value.district)
  if (d) out.push(d)
  return out
}
