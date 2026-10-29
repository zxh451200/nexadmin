import { ChevronsDownUp, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { allCollapsed, nextExpandedAfterBulkToggle } from './tree-expand'

/**
 * 树/树形表格的「全部展开 / 全部收起」合并按钮：
 * 一个都没展开时给「全部展开」，其余情况给「全部收起」（与侧栏底部按钮同一联动逻辑）。
 * 没有可展开节点时不渲染。
 */
export function ExpandAllToggle({ ids, expanded, onChange }: { ids: readonly string[]; expanded: ReadonlySet<string>; onChange: (next: Set<string>) => void }) {
  const { t } = useTranslation()
  if (ids.length === 0) return null
  const collapsed = allCollapsed(ids, expanded)
  return (
    <Button type="button" variant="outline" size="sm" onClick={() => onChange(nextExpandedAfterBulkToggle(ids, expanded))}>
      {collapsed ? <ChevronsUpDown aria-hidden="true" /> : <ChevronsDownUp aria-hidden="true" />}
      {collapsed ? t('dataTable.expandAll') : t('dataTable.collapseAll')}
    </Button>
  )
}
