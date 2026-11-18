import { ChevronsDownUp, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { allCollapsed, nextExpandedAfterBulkToggle } from './tree-expand'

/**
 * 树/树形表格的「全部展开 / 全部收起」合并按钮：
 * 一个都没展开时给「全部展开」，其余情况给「全部收起」（与侧栏底部按钮同一联动逻辑）。
 * 没有可展开节点时不渲染。
 *
 * className 用于拉齐所在工具条的高度口径：TreeTable 工具条右端的「列设置」是 icon 按钮
 * （桌面 36 / 移动 40），默认 size=sm（桌面 32 / 移动 36）会矮一截；那边传 `h-10 md:h-9`
 * 对齐。PermissionTree 里它和「全选」同为 sm，不传，保持不变。
 */
export function ExpandAllToggle({ ids, expanded, onChange, className }: { ids: readonly string[]; expanded: ReadonlySet<string>; onChange: (next: Set<string>) => void; className?: string }) {
  const { t } = useTranslation()
  if (ids.length === 0) return null
  const collapsed = allCollapsed(ids, expanded)
  return (
    <Button type="button" variant="outline" size="sm" className={className} onClick={() => onChange(nextExpandedAfterBulkToggle(ids, expanded))}>
      {collapsed ? <ChevronsUpDown aria-hidden="true" /> : <ChevronsDownUp aria-hidden="true" />}
      {collapsed ? t('dataTable.expandAll') : t('dataTable.collapseAll')}
    </Button>
  )
}
