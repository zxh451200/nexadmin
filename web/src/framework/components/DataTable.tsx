import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { ArrowDown, ArrowUp, Info, Loader2 } from 'lucide-react'
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
import { ListErrorBanner } from './ListErrorBanner'
import { BatchBar } from './table/BatchBar'
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
  /** 不分页列表的命中总数；不传则不凭空推断 data.length */
  total?: number
  /** 当前是否应用了筛选条件，用于区分真空态与筛选零命中 */
  hasFilters?: boolean
  filteredEmptyText?: ReactNode
  isError?: boolean
  onRetry?: () => void
  retrying?: boolean
  selection?: { selected: string[]; onChange: (selected: string[]) => void }
  /** 选中行后显示的批量操作；函数形式可拿到已选 id */
  batchActions?: ReactNode | ((selected: string[]) => ReactNode)
  rowActions?: (row: T) => ReactNode
  /** 拖动排序；disabled 时保留调用方意图并展示原因，与 pagination 同时开启时忽略拖动 */
  sortable?: {
    onReorder: (orderedIds: string[]) => void | Promise<unknown>
    disabled?: boolean
    disabledReason?: ReactNode
  }
  /**
   * 服务端排序（受控）：给了它，声明了 sortKey 的列会把表头变成可点按钮，点了就在升/降之间切。
   * 排序值由调用方持有并回填（一般同时也喂给工具条的下拉），这里只负责交互与状态指示，
   * 不排序 data ——数据顺序是后端给的。
   */
  sort?: { key: string; desc: boolean; onChange: (key: string, desc: boolean) => void }
  onRowClick?: (row: T) => void
  toolbar?: ReactNode
  /** 提供后启用列显示/隐藏设置，并按此 key 存 localStorage */
  columnSettingsKey?: string
  emptyText?: ReactNode
  className?: string
  /** 嵌在已有卡片/分区里时用：不再自带边框与投影，避免"卡片里再画一圈" */
  bare?: boolean
  /**
   * 满高形态（模板 products 页的整页表格）：配合 h-full 的页面容器，桌面端表格区
   * flex-1 + 内部滚动、表头吸顶，页面本身不滚；分页/批量条钉在视口底部。
   * 移动端忽略（窄屏卡片本来就顺文档流滚）。
   */
  fill?: boolean
  /**
   * 批量条放哪：viewport（默认）浮在视口底边；root 锚定在表格容器底边。
   * 主从双栏页（字典管理）必须用 root——两个表格都往视口 portal 会叠在一起。
   */
  batchBarPlacement?: 'viewport' | 'root'
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
  total,
  hasFilters,
  filteredEmptyText,
  isError,
  onRetry,
  retrying,
  selection,
  batchActions,
  rowActions,
  sortable,
  sort,
  onRowClick,
  toolbar,
  columnSettingsKey,
  emptyText,
  className,
  bare,
  fill,
  batchBarPlacement,
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

  const sortEnabled = !!sortable?.onReorder && !sortable.disabled && !pagination
  useEffect(() => {
    if (sortable?.onReorder && pagination) {
      console.warn('[DataTable] `sortable` and `pagination` cannot be used together; drag sorting is disabled.')
    }
  }, [sortable, pagination])

  // 拖动后的乐观顺序；数据刷新后以服务端为准
  const [override, setOverride] = useState<T[] | null>(null)
  useEffect(() => setOverride(null), [data])
  const rows = override ?? data
  const ids = useMemo(() => rows.map(getKey), [rows, getKey])

  // 分页表格的自愈：批量/单条删除很容易把人留在「total 还有、这一页却空了」的空白页上。
  // 只在确实空了且不在首页时退一页；首页或真·空表（total=0）不触发，不会和空状态抢显示。
  const pag = pagination === false ? undefined : pagination
  const { page: pagPage, pageSize: pagSize, total: pagTotal } = pag ?? { page: 1, pageSize: 0, total: 0 }
  const onPagChange = pag?.onChange
  useEffect(() => {
    if (loading || isError || rows.length > 0 || pagTotal === 0 || pagPage <= 1) return
    onPagChange?.(pagPage - 1, pagSize)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, isError, rows.length, pagPage, pagSize, pagTotal])

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
    if (!sortEnabled || !sortable || !e.over || e.active.id === e.over.id) return
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

  const colSpan = visibleColumns.length + (selection ? 1 : 0) + (rowActions ? 1 : 0)
  // 只有表格真的横向溢出时才吸附操作列，避免窄表里出现一块游离的白色块
  const { ref: scrollRef, overflowing } = useOverflowX<HTMLDivElement>()
  const pinActions = overflowing && !!rowActions
  const empty = !loading && rows.length === 0
  const failedEmpty = !!isError && empty
  const staleError = !!isError && !loading && rows.length > 0
  const sortNotice = !!sortable?.disabled && !!sortable.disabledReason && !loading && rows.length > 0
  const emptyNode = failedEmpty ? (
    <EmptyState
      title={t('common.loadFailed')}
      action={
        onRetry ? (
          <Button type="button" variant="outline" disabled={retrying} onClick={onRetry}>
            {retrying ? <Loader2 className="me-1 size-4 animate-spin" aria-hidden="true" /> : null}
            {t('common.retry')}
          </Button>
        ) : null
      }
    />
  ) : (
    <EmptyState
      title={hasFilters ? filteredEmptyText ?? t('common.emptyFiltered') : emptyText}
      description={hasFilters ? t('common.emptyFilteredHint') : undefined}
    />
  )

  // 列显示设置：统一放在表格上方工具栏右端，和 TreeTable 一致
  const settingsNode = columnSettingsKey && !isMobile ? <ColumnSettings columns={columns} hidden={hidden} onToggle={toggle} onReset={reset} /> : null

  // 满高形态：表头吸顶（th 各自 sticky，Table 元素不参与 overflow 裁剪）
  const thSticky = fill ? 'sticky top-0 z-10 bg-card' : undefined

  const desktop = (
    <div className={cn('overflow-hidden', fill && 'flex min-h-0 flex-1 flex-col', bare ? 'bg-transparent' : 'card-edge rounded-xl border border-border bg-card')}>
      {/* 列多时横向滚动，不许把最后一列（操作）裁掉；满高时纵向滚动也收在这里 */}
      <div ref={scrollRef} className={cn('overflow-x-auto', fill && 'min-h-0 flex-1 overflow-y-auto')}>
        <Table>
        <TableHeader>
          <TableRow>
            {selection ? (
              <TableHead className={cn('w-12', thSticky)}>
                <SelectBox checked={headState} onChange={toggleAll} label={t('dataTable.selectAllRows')} />
              </TableHead>
            ) : null}
            {visibleColumns.map((c) => {
              const sortableCol = !!sort && !!c.sortKey
              const active = sortableCol && sort!.key === c.sortKey
              return (
                <TableHead
                  key={c.key}
                  style={c.width !== undefined ? { width: c.width } : undefined}
                  className={cn('group/th whitespace-nowrap', thSticky, alignClass(c.align))}
                  aria-sort={active ? (sort!.desc ? 'descending' : 'ascending') : sortableCol ? 'none' : undefined}
                >
                  {sortableCol ? (
                    // 表头即排序入口：点一下升序、再点降序。只有「当前排序列」显示箭头（↑/↓ 两种状态，
                    // 不再是第三种双箭头）；未排序的列悬停才浮出淡箭头，充当「这里能点」的提示。
                    <button
                      type="button"
                      onClick={() => sort!.onChange(c.sortKey!, active ? !sort!.desc : false)}
                      title={t('dataTable.sortByColumn', { column: typeof c.title === 'string' ? c.title : c.key })}
                      className={cn(
                        'relative -mx-1 inline-flex max-w-full items-center gap-1 rounded-sm px-1 py-0.5 text-start transition-colors hover:text-foreground',
                        active ? 'text-foreground' : 'text-muted-foreground',
                      )}
                    >
                      <span className="truncate">{c.title}</span>
                      {active ? (
                        sort!.desc ? (
                          <ArrowDown className="size-3.5 shrink-0" aria-hidden="true" />
                        ) : (
                          <ArrowUp className="size-3.5 shrink-0" aria-hidden="true" />
                        )
                      ) : (
                        // 未激活的提示箭头改为绝对定位、不占布局宽度：否则窄列（如「状态」）
                        // 会被这枚 shrink-0 图标挤出空间，标题被 truncate 成「状」，像掉了字。
                        // 悬停亮度 0.7：0.4 的灰在深色卡面上（163 灰 × 0.4 ≈ 65 vs 背景 10）
                        // 对比度只有 ~1.6:1，用户看着像「箭头被挡住」，其实是根本没看见。
                        <ArrowUp
                          className="pointer-events-none absolute end-0 top-1/2 size-3.5 -translate-y-1/2 opacity-0 transition-opacity group-hover/th:opacity-70"
                          aria-hidden="true"
                        />
                      )}
                    </button>
                  ) : (
                    c.title
                  )}
                </TableHead>
              )
            })}
            {rowActions ? (
              // 操作列吸在右侧：宽表格横向滚动时也始终可见。w-px 让自动列宽按内容收缩，
              // 剩余宽度回到正文列——否则这块空白会被摊给操作列，图标右对齐后左侧空一大块。
              <TableHead
                className={cn(
                  'w-px whitespace-nowrap text-end',
                  thSticky,
                  pinActions && 'sticky inset-e-0 z-20 bg-card shadow-[inset_1px_0_0_hsl(var(--border))]',
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
                const cls = cn('group', onRowClick && 'cursor-pointer', selected.includes(id) && 'row-selected')
                const cells = (
                  <>
                    {selection ? (
                      <TableCell className="w-12 py-0">
                        <SelectBox
                          checked={selected.includes(id)}
                          onChange={() => toggleOne(id)}
                          label={t('dataTable.selectRow')}
                        />
                      </TableCell>
                    ) : null}
                    {visibleColumns.map((c, ci) => (
                      // max-w + truncate 全局兜底：单元格内容最长占 28rem，超限省略——
                      // 没有它，任何漏网的長串（URL/JSON/备注）都会把列宽撑爆整张表；
                      // 自带限宽的 render（max-w-* truncate / line-clamp-N）在此之内不受影响
                      <TableCell key={c.key} className={cn('max-w-[28rem] truncate', alignClass(c.align))}>
                        {ci === 0 && sortEnabled ? (
                          // 把手并进首列，不设独立空白列：静默时只折进首列左内边距的观感，hover 浮现
                          <div className="flex items-center gap-1">
                            <span className="inline-flex">
                              <SortableHandle className="drag-handle-quiet h-8 w-6 md:h-8 md:w-6" />
                            </span>
                            <span className="min-w-0">{renderCell(c, row, index)}</span>
                          </div>
                        ) : (
                          renderCell(c, row, index)
                        )}
                      </TableCell>
                    ))}
                    {rowActions ? (
                      <TableCell
                        className={cn(
                          'w-px whitespace-nowrap text-end',
                          pinActions &&
                            (selected.includes(id)
                              ? // 选中行不挂 hover 色：row-selected 不透明，行体与钉住的行尾同色
                                'sticky inset-e-0 z-10 row-selected shadow-[inset_1px_0_0_hsl(var(--border))]'
                              : 'sticky inset-e-0 z-10 bg-card shadow-[inset_1px_0_0_hsl(var(--border))] group-hover:row-hover'),
                        )}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <div className="row-actions flex items-center justify-end gap-1">{rowActions(row)}</div>
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
        // ps-2：与卡片首列复选框对齐——手机卡 px-3 减掉 leading 的 -ms-1、紧凑行本身就是 px-2，两种形态合起来都是 8px
        <label className="flex min-h-11 items-center gap-1 ps-2 text-sm text-muted-foreground">
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
                ? 'rounded-lg border border-border bg-card px-2 py-1 transition-colors hover:row-hover'
                : bare
                  ? 'border-b border-border/60 px-3 py-2.5 transition-colors last:border-0 hover:row-hover'
                  : 'card-edge rounded-xl border border-border bg-card px-3 py-2.5',
              selected.includes(id) && 'row-selected',
            )
            const body = (
              <MobileCardContent
                dense={dense}
                columns={visibleColumns}
                row={row}
                index={index}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                leading={
                  selection ? (
                    <SelectBox
                      checked={selected.includes(id)}
                      onChange={() => toggleOne(id)}
                      label={t('dataTable.selectRow')}
                    />
                  ) : null
                }
                actions={
                  sortEnabled ? (
                    <>
                      {/* 排序把手跟行操作同排（编辑左边），常显：卡片里静默把手只会在左侧留下一段空槽，
                          上下移按钮又要占两格、还多点一次。把手常显后触屏也能直接按住拖。 */}
                      <SortableHandle className="size-8" />
                      {rowActions ? rowActions(row) : null}
                    </>
                  ) : (
                    rowActions?.(row)
                  )
                }
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
    <div ref={rootRef} className={cn('relative flex flex-col gap-3', className)}>
      {/* 工具条在卡片外面、与卡片同宽（全站口径）。自定义工具条里的控件按右边缘对齐时，
          注意别把「列设置」这类只在某些视图出现的按钮算进来——它由这里统一占右侧槽位，
          页面自己再排一个视图专属按钮（如网格/列表切换）时要预留同宽空位，否则切视图整排控件会跳。 */}
      {showToolbar ? (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{toolbar}</div>
          {settingsNode}
        </div>
      ) : null}

      {sortNotice ? (
        <div role="status" className="flex items-start gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm text-muted-foreground">
          <Info className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>{sortable?.disabledReason}</span>
        </div>
      ) : null}

      {staleError && onRetry ? <ListErrorBanner onRetry={onRetry} retrying={retrying} /> : null}

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
          {/* 浮层挂到 body：页面里任何带 transform 的祖先都会让 position:fixed 改按它计算，
              表现就是拖拽时浮层离指针很远（偏一个侧栏/顶栏的距离）。 */}
          {createPortal(
            <DragOverlay dropAnimation={null}>
              {dragChip?.label ? <DragChip width={dragChip.width}>{dragChip.label}</DragChip> : null}
            </DragOverlay>,
            document.body,
          )}
        </DndContext>
      ) : (
        list
      )}

      {!failedEmpty ? (
        pagination ? (
          pagination.total > 0 ? (
            <TablePagination {...pagination} />
          ) : (
            <div className="num text-end text-sm text-muted-foreground">{t('dataTable.totalCount', { count: 0 })}</div>
          )
        ) : total !== undefined ? (
          <div className="num text-end text-sm text-muted-foreground">{t('dataTable.totalCount', { count: total })}</div>
        ) : null
      ) : null}

      {selection && selected.length > 0 ? (
        <BatchBar count={selected.length} onClear={() => selection.onChange([])} mode={batchBarPlacement === 'root' ? 'sticky' : 'viewport'}>
          {typeof batchActions === 'function' ? batchActions(selected) : batchActions}
        </BatchBar>
      ) : null}
    </div>
  )
}
