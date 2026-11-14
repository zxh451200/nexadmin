import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { RegionNode } from '@/framework/request/types'

export type RegionValue = { province?: string; city?: string; district?: string; street?: string }

/** 省市区（及可选的街道/乡镇第 4 级）树，内置数据基本不变，永久缓存。
    街道层只在 level=4 时下发；业务存储仍只存前三级 6 位代码，街道名由调用方折进详细地址。 */
export function useRegionTree(level: 2 | 3 | 4 = 3) {
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
  if (!d) return out
  out.push(d)
  const s = d.children?.find((n) => n.code === value.street)
  if (s) out.push(s)
  return out
}
