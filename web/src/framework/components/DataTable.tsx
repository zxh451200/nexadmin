import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DndContext, DragOverlay, type DragEndEvent, type DragStartEvent } from '@dnd-kit/core'
import { arrayMove, SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useCompactLayout } from './table/use-compact-layout'
import { useOverflowX } from './table/use-overflow-x'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Skeleton } from '@/framework/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/framework/ui/table'
import { cn } from '@/lib/utils'
import { EmptyState } from './EmptyState'
import { ColumnSettings } from './table/ColumnSettings'
import { MobileCardContent } from './table/MobileCard'
import { DragChip, SortableHandle, SortableItem } from './table/SortableItem'
import { TablePagination } from './table/TablePagination'
import { alignClass, renderCell, type Column, type PaginationProps } from './table/types'
import { useColumnSettings } from './table/use-column-settings'
import { pointerFirstCollision, restrictToVertical, useDragCursor, useEdgeAutoScroll, useSortableSensors } from './table/use-sortable-sensors'

export type { Column, PaginationProps } from './table/types'

export interface DataTableProps<T> {
  columns: Column<T>[]
  data: T[]
  rowKey: keyof T | ((row: T) => string)
  loading?: boolean
  pagination?: PaginationProps | false
  selection?: { selected: string[]; onChange: (selected: string[]) => void }
  /** 选中行后显示的批量操作；函数形式可拿到已选 id */
  batchActions?: ReactNode | ((selected: string[]) => ReactNode)
  rowActions?: (row: T) => ReactNode
  /** 拖动排序；与 pagination 同时开启时忽略拖动并 console.warn */
  sortable?: { onReorder: (orderedIds: string[]) => void | Promise<unknown> }
  onRowClick?: (row: T) => void
  toolbar?: ReactNode
  /** 提供后启用列显示/隐藏设置，并按此 key 存 localStorage */
  columnSettingsKey?: string
  emptyText?: ReactNode
  className?: string
}

function SelectBox({
  checked,
  onChange,
  label,
}: {
  checked: boolean | 'indeterminate'
  onChange: () => void
  label: string
}) {
  return (
    <div
      className="flex h-11 w-11 shrink-0 items-center justify-center md:h-9 md:w-9"
      onClick={(e) => e.stopPropagation()}
    >
      <Checkbox checked={checked} onCheckedChange={onChange} aria-label={label} />
    </div>
  )
}

export function DataTable<T>({
  columns,
  data,
  rowKey,
  loading,
  pagination,
  selection,
  batchActions,
  rowActions,
  sortable,
  onRowClick,
  toolbar,
  columnSettingsKey,
  emptyText,
  className,
}: DataTableProps<T>) {
  const { t } = useTranslation()
  const { ref: rootRef, compact: isMobile, phone } = useCompactLayout()
  const dense = isMobile && !phone
  const sensors = useSortableSensors()
  const { hidden, toggle, reset, visibleColumns } = useColumnSettings(columns, columnSettingsKey)

  const getKey = useCallback(
    (row: T) => (typeof rowKey === 'function' ? rowKey(row) : String(row[rowKey])),
    [rowKey],
  )

  const sortEnabled = !!sortable && !pagination
  useEffect(() => {
    if (sortable && pagination) {
      console.warn('[DataTable] `sortable` and `pagination` cannot be used together; drag sorting is disabled.')
    }
  }, [sortable, pagination])

  // 拖动后的乐观顺序；数据刷新后以服务端为准
  const [override, setOverride] = useState<T[] | null>(null)
  useEffect(() => setOverride(null), [data])
  const rows = override ?? data
  const ids = useMemo(() => rows.map(getKey), [rows, getKey])

  // 浮层内容：起拖时取该行第一列的单元格，宽度取被拖行的实际宽度
  const [dragChip, setDragChip] = useState<{ label: ReactNode; width: number } | null>(null)
  useDragCursor(!!dragChip)
  useEdgeAutoScroll(!!dragChip, () => scrollRef.current)
  // 拖动排序用行号播报（dnd-kit 默认是英文，且会念出内部 id）
  const a11y = useMemo(() => {
    const rowNo = (id: unknown) => Math.max(1, ids.indexOf(String(id)) + 1)
    return {
      announcements: {
        onDragStart: ({ active }: { active: { id: string | number } }) => t('dataTable.a11y.picked', { row: rowNo(active.id) }),
        onDragOver: ({ active, over }: { active: { id: string | number }; over: { id: string | number } | null }) =>
          over ? t('dataTable.a11y.over', { row: rowNo(active.id), target: rowNo(over.id) }) : t('dataTable.a11y.overEmpty', { row: rowNo(active.id) }),
        onDragEnd: ({ active, over }: { active: { id: string | number }; over: { id: string | number } | null }) =>
          over ? t('dataTable.a11y.dropped', { row: rowNo(active.id), target: rowNo(over.id) }) : t('dataTable.a11y.droppedEmpty', { row: rowNo(active.id) }),
        onDragCancel: ({ active }: { active: { id: string | number } }) => t('dataTable.a11y.cancelled', { row: rowNo(active.id) }),
      },
    }
  }, [t, ids])

  const onDragStart = (e: DragStartEvent) => {
    const id = String(e.active.id)
    const row = rows.find((r) => getKey(r) === id)
    const first = visibleColumns[0] ?? columns[0]
    setDragChip({
      label: row && first ? renderCell(first, row, 0) : null,
      width: Math.round(e.active.rect.current.initial?.width ?? 0),
    })
  }
  const onDragCancel = () => setDragChip(null)

  const onDragEnd = async (e: DragEndEvent) => {
    setDragChip(null)
    if (!sortable || !e.over || e.active.id === e.over.id) return
    const from = ids.indexOf(String(e.active.id))
    const to = ids.indexOf(String(e.over.id))
    if (from < 0 || to < 0) return
    const next = arrayMove(rows, from, to)
    setOverride(next)
    try {
      await sortable.onReorder(next.map(getKey))
    } catch {
      setOverride(null)
      toast.error(t('dataTable.reorderFailed'))
    }
  }

  const selected = selection?.selected ?? []
  const allSelected = rows.length > 0 && ids.every((id) => selected.includes(id))
  const someSelected = ids.some((id) => selected.includes(id))
  const toggleAll = () => {
    if (!selection) return
    selection.onChange(allSelected ? selected.filter((id) => !ids.includes(id)) : [...new Set([...selected, ...ids])])
  }
  const toggleOne = (id: string) => {
    if (!selection) return
    selection.onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id])
  }
  const headState: boolean | 'indeterminate' = allSelected ? true : someSelected ? 'indeterminate' : false

  const clickProps = (row: T) =>
    onRowClick
      ? {
          onClick: () => onRowClick(row),
          tabIndex: 0,
          onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => {
            if (e.key === 'Enter' && e.target === e.currentTarget) onRowClick(row)
          },
        }
      : {}

  const colSpan = visibleColumns.length + (sortEnabled ? 1 : 0) + (selection ? 1 : 0) + (rowActions ? 1 : 0)
  // 只有表格真的横向溢出时才吸附操作列，避免窄表里出现一块游离的白色块
  const { ref: scrollRef, overflowing } = useOverflowX<HTMLDivElement>()
  const pinActions = overflowing && !!rowActions
  const empty = !loading && rows.length === 0
  const emptyNode = <EmptyState title={emptyText} />

  // 列显示设置：统一放在表格上方工具栏右端，和 TreeTable 一致
  const settingsNode = columnSettingsKey && !isMobile ? <ColumnSettings columns={columns} hidden={hidden} onToggle={toggle} onReset={reset} /> : null

  const desktop = (
    <div className="card-edge overflow-hidden rounded-xl border border-border bg-card">
      {/* 列多时横向滚动，不许把最后一列（操作）裁掉 */}
      <div ref={scrollRef} className="overflow-x-auto">
        <Table>
        <TableHeader>
          <TableRow>
            {sortEnabled ? (
              <TableHead className="w-14">
                <span className="sr-only">{t('dataTable.dragHandle')}</span>
              </TableHead>
            ) : null}
            {selection ? (
              <TableHead className="w-12">
                <SelectBox checked={headState} onChange={toggleAll} label={t('dataTable.selectAllRows')} />
              </TableHead>
            ) : null}
            {visibleColumns.map((c) => (
              <TableHead
                key={c.key}
                style={c.width !== undefined ? { width: c.width } : undefined}
                className={cn('whitespace-nowrap', alignClass(c.align))}
              >
                {c.title}
              </TableHead>
            ))}
            {rowActions ? (
              // 操作列吸在右侧：宽表格横向滚动时也始终可见
              <TableHead
                className={cn(
                  'text-end',
                  pinActions && 'sticky end-0 z-20 bg-muted shadow-[inset_1px_0_0_hsl(var(--border))]',
                )}
              >
                {t('common.actions')}
              </TableHead>
            ) : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading
            ? Array.from({ length: 5 }, (_, i) => (
                <TableRow key={`sk${i}`}>
                  {Array.from({ length: colSpan }, (_, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-5 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            : null}
          {empty ? (
            <TableRow>
              <TableCell colSpan={colSpan}>{emptyNode}</TableCell>
            </TableRow>
          ) : null}
          {!loading
            ? rows.map((row, index) => {
                const id = getKey(row)
                const cls = cn('group', onRowClick && 'cursor-pointer', selected.includes(id) && 'bg-muted/50')
                const cells = (
                  <>
                    {sortEnabled ? (
                      <TableCell className="w-14 py-0">
                        <SortableHandle />
                      </TableCell>
                    ) : null}
                    {selection ? (
                      <TableCell className="w-12 py-0">
                        <SelectBox
                          checked={selected.includes(id)}
                          onChange={() => toggleOne(id)}
                          label={t('dataTable.selectRow')}
                        />
                      </TableCell>
                    ) : null}
                    {visibleColumns.map((c) => (
                      <TableCell key={c.key} className={cn('whitespace-nowrap', alignClass(c.align))}>
                        {renderCell(c, row, index)}
                      </TableCell>
                    ))}
                    {rowActions ? (
                      <TableCell
                        className={cn(
                          'whitespace-nowrap text-end',
                          pinActions &&
                            'sticky end-0 z-10 bg-card shadow-[inset_1px_0_0_hsl(var(--border))] group-hover:bg-muted',
                        )}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <div className="flex items-center justify-end gap-1">{rowActions(row)}</div>
                      </TableCell>
                    ) : null}
                  </>
                )
                return sortEnabled ? (
                  <SortableItem key={id} id={id} as="tr" className={cls} {...clickProps(row)}>
                    {cells}
                  </SortableItem>
                ) : (
                  <TableRow key={id} className={cls} {...clickProps(row)}>
                    {cells}
                  </TableRow>
                )
              })
            : null}
        </TableBody>
      </Table>
      </div>
    </div>
  )

  const mobile = (
    <div className={dense ? 'flex flex-col gap-1.5' : 'flex flex-col gap-3'}>
      {selection && rows.length > 0 && !loading ? (
        <label className="flex min-h-11 items-center gap-1 text-sm text-muted-foreground">
          <SelectBox checked={headState} onChange={toggleAll} label={t('dataTable.selectAllRows')} />
          {t('common.selectAll')}
        </label>
      ) : null}
      {loading
        ? Array.from({ length: 4 }, (_, i) => (
            <div key={i} className="space-y-2 rounded-lg border border-border bg-card p-3">
              <Skeleton className="h-5 w-2/3" />
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-4 w-full" />
            </div>
          ))
        : null}
      {empty ? <div className="rounded-lg border border-border bg-card">{emptyNode}</div> : null}
      {!loading
        ? rows.map((row, index) => {
            const id = getKey(row)
            const cls = cn(
              dense
                ? 'rounded-lg border border-border bg-card px-2 py-1 transition-colors hover:bg-muted/50'
                : 'card-edge rounded-xl border border-border bg-card px-3 py-2.5',
              selected.includes(id) && 'bg-muted/50',
            )
            const body = (
              <MobileCardContent
                dense={dense}
                columns={visibleColumns}
                row={row}
                index={index}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                leading={
                  <>
                    {sortEnabled ? <SortableHandle /> : null}
                    {selection ? (
                      <SelectBox
                        checked={selected.includes(id)}
                        onChange={() => toggleOne(id)}
                        label={t('dataTable.selectRow')}
                      />
                    ) : null}
                  </>
                }
                actions={rowActions ? rowActions(row) : undefined}
              />
            )
            return sortEnabled ? (
              <SortableItem key={id} id={id} className={cls}>
                {body}
              </SortableItem>
            ) : (
              <div key={id} className={cls}>
                {body}
              </div>
            )
          })
        : null}
    </div>
  )

  const list = isMobile ? mobile : desktop
  const showToolbar = !!toolbar || !!settingsNode

  return (
    <div ref={rootRef} className={cn('flex flex-col gap-3', className)}>
      {showToolbar ? (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{toolbar}</div>
          {settingsNode}
        </div>
      ) : null}

      {selection && selected.length > 0 ? (
        <div
          className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-muted px-3 py-2"
          role="region"
          aria-label={t('dataTable.batchActions')}
        >
          <span className="num text-sm text-foreground">{t('dataTable.selectedCount', { count: selected.length })}</span>
          <Button type="button" variant="ghost" size="sm" onClick={() => selection.onChange([])}>
            {t('common.clear')}
          </Button>
          <div className="ms-auto flex flex-wrap items-center gap-2">
            {typeof batchActions === 'function' ? batchActions(selected) : batchActions}
          </div>
        </div>
      ) : null}

      {sortEnabled ? (
        <DndContext
          sensors={sensors}
          collisionDetection={pointerFirstCollision}
          modifiers={[restrictToVertical]}
          accessibility={a11y}
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
          onDragCancel={onDragCancel}
        >
          <SortableContext items={ids} strategy={verticalListSortingStrategy}>
            {list}
          </SortableContext>
          <DragOverlay dropAnimation={null}>
            {dragChip?.label ? <DragChip width={dragChip.width}>{dragChip.label}</DragChip> : null}
          </DragOverlay>
        </DndContext>
      ) : (
        list
      )}

      {pagination && pagination.total > 0 ? <TablePagination {...pagination} /> : null}
    </div>
  )
}
