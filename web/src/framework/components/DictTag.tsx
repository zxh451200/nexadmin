import { Badge, type BadgeProps } from '@/framework/ui/badge'
import type { DictItem } from '@/framework/request/types'
import { useDictItems } from './pickers/useDictItems'

export function tagVariant(tag: DictItem['tag']): BadgeProps['variant'] {
  switch (tag) {
    case 'success':
    case 'warning':
    case 'info':
    case 'destructive':
      return tag
    default:
      return 'secondary'
  }
}

export interface DictTagProps {
  dict: string
  value?: string | number | null
  className?: string
}

/** 按字典项 tag 变体显示 Badge；找不到字典项时原样显示值 */
export function DictTag({ dict, value, className }: DictTagProps) {
  const { data } = useDictItems(dict)
  if (value === undefined || value === null || value === '') return <span className="text-muted-foreground">-</span>
  const item = data?.find((d) => String(d.value) === String(value))
  return (
    <Badge variant={tagVariant(item?.tag)} className={className}>
      {item?.label ?? String(value)}
    </Badge>
  )
}
