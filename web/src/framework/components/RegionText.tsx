import { findRegionPath, useRegionTree, type RegionValue } from './pickers/useRegionTree'

export interface RegionTextProps {
  value?: RegionValue | null
  separator?: string
  className?: string
}

/** 把行政区划代码显示成「广东省 / 深圳市 / 南山区」 */
export function RegionText({ value, separator = ' / ', className }: RegionTextProps) {
  const { data } = useRegionTree(3)
  const path = findRegionPath(data, value ?? undefined)
  if (!path.length) {
    const raw = [value?.province, value?.city, value?.district].filter(Boolean).join(separator)
    return <span className={className}>{raw || '-'}</span>
  }
  return <span className={className}>{path.map((n) => n.name).join(separator)}</span>
}
