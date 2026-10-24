import type { ReactNode } from 'react'

export type ColumnAlign = 'start' | 'center' | 'end' | 'left' | 'right'

export interface Column<T> {
  key: string
  title: ReactNode
  render?: (row: T, index: number) => ReactNode
  width?: number | string
  align?: ColumnAlign
  /** 手机卡片中的角色：title 主标题 / sub 副标题 / normal 键值行(默认) / hidden 不显示 */
  mobile?: 'title' | 'sub' | 'hidden' | 'normal'
  /** 是否可在列设置中隐藏，默认 true */
  hideable?: boolean
}

export interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onChange: (page: number, pageSize: number) => void
  pageSizeOptions?: number[]
}

export function alignClass(align?: ColumnAlign): string {
  switch (align) {
    case 'center':
      return 'text-center'
    case 'end':
    case 'right':
      return 'text-end'
    default:
      return 'text-start'
  }
}

export function renderCell<T>(col: Column<T>, row: T, index: number): ReactNode {
  if (col.render) return col.render(row, index)
  const v = (row as Record<string, unknown>)[col.key]
  if (v === null || v === undefined || v === '') return <span className="text-muted-foreground">-</span>
  return v as ReactNode
}
