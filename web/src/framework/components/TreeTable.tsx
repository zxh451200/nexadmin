import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { closestCenter, DndContext, useDroppable, type DragEndEvent, type DragStartEvent } from '@dnd-kit/core'
import { arrayMove, SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useCompactLayout } from './table/use-compact-layout'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/framework/ui/table'
import { cn } from '@/lib/utils'
import { EmptyState } from './EmptyState'
import { ColumnSettings } from './table/ColumnSettings'
import { MobileCardContent } from './table/MobileCard'
import { SortableHandle, SortableItem } from './table/SortableItem'
import { alignClass, renderCell, type Column } from './table/types'
import { useColumnSettings } from './table/use-column-settings'
import { restrictToVertical, useSortableSensors } from './table/use-sortable-sensors'

export interface TreeTableProps<T extends { id: string; children?: T[] }> {
  columns: Column<T>[]
  data: T[]
  rowActions?: (row: T) => ReactNode
  /** 同一父级内拖动排序；根级 parentId 为 null */
  onReorder?: (parentId: string | null, orderedIds: string[]) => void | Promise<unknown>
  /** 跨父级移动：拖到目标节点上会成为其最后一个子节点。 */
  onMove?: (id: string, parentId: string | null) => void | Promise<unknown>
  defaultExpandAll?: boolean
  /** 筛选时用：不管记忆的展开状态，全部展开以便看到命中项 */
  forceExpandAll?: boolean
  loading?: boolean
  toolbar?: ReactNode
  columnSettingsKey?: string
  onRowClick?: (row: T) => void
  emptyText?: ReactNode
  className?: string
}

/** 按深度缩进（逻辑类 ps-，静态列出以便 Tailwind 扫描） */
const INDENT = ['ps-0', 'ps-6', 'ps-12', 'ps-[4.5rem]', 'ps-24', 'ps-[7.5rem]', 'ps-36', 'ps-[10.5rem]', 'ps-48']
const ROOT_DROP_ID = '__nexadmin_tree_root__'
const groupId = (parentId: string | null) => `g:${parentId ?? ''}`
const parseGroup = (cid: unknown): string | null | undefined =>
  typeof cid === 'string' && cid.startsWith('g:') ? cid.slice(2) || null : undefined

function RootDropZone() {
  const { setNodeRef, isOver } = useDroppable({ id: ROOT_DROP_ID })
  return (
    <div
      ref={setNodeRef}
      className={cn(
        'mb-2 rounded-md border border-dashed px-3 py-2 text-xs text-muted-foreground',
        isOver && 'border-primary bg-primary/5 text-primary',
      )}
    >
      放到这里移动到根菜单
    </div>
  )
}

function collectParents<T extends { id: string; children?: T[] }>(nodes: T[], out: string[] = []): string[] {
  for (const n of nodes) {
    if (n.children?.length) {
      out.push(n.id)
      collectParents(n.children, out)
    }
  }
  return out
}

function siblingsOf<T extends { id: string; children?: T[] }>(nodes: T[], parentId: string | null): T[] | undefined {
  if (parentId === null) return nodes
  for (const n of nodes) {
    if (n.id === parentId) return n.children
    const found = n.children ? siblingsOf(n.children, parentId) : undefined
    if (found) return found
  }
  return undefined
}

function reorderTree<T extends { id: string; children?: T[] }>(nodes: T[], parentId: string | null, ids: string[]): T[] {
  const sortBy = (list: T[]) => ids.map((id) => list.find((n) => n.id === id)).filter((n): n is T => !!n)
  if (parentId === null) return sortBy(nodes)
  return nodes.map((n) => {
    if (n.id === parentId && n.children) return { ...n, children: sortBy(n.children) }
    return n.children ? { ...n, children: reorderTree(n.children, parentId, ids) } : n
  })
}

export function TreeTable<T extends { id: string; children?: T[] }>({
  columns,
  data,
  rowActions,
  onReorder,
  onMove,
  defaultExpandAll,
  forceExpandAll,
  loading,
  toolbar,
  columnSettingsKey,
  onRowClick,
  emptyText,
  className,
}: TreeTableProps<T>) {
  const { t } = useTranslation()
  const { ref: rootRef, compact: isMobile, phone } = useCompactLayout()
  const dense = isMobile && !phone
  const sensors = useSortableSensors()
  const { hidden, toggle, reset, visibleColumns } = useColumnSettings(columns, columnSettingsKey)

  const [override, setOverride] = useState<T[] | null>(null)
  useEffect(() => setOverride(null), [data])
  const tree = override ?? data

  // 展开状态记在 localStorage（按 columnSettingsKey，没有就按页面路径），刷新后保持
  const storageKey = `nex-tree-expanded:${columnSettingsKey ?? window.location.pathname}`
  const restored = useRef(false)
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    try {
      const raw = localStorage.getItem(storageKey)
      if (raw) {
        restored.current = true
        return new Set<string>(JSON.parse(raw) as string[])
      }
    } catch {
      // 存储不可用或数据损坏：按默认处理
    }
    return new Set()
  })
  const commitExpanded = (next: Set<string>) => {
    setExpanded(next)
    try {
      localStorage.setItem(storageKey, JSON.stringify([...next]))
    } catch {
      // 存储不可用时仅本次生效
    }
  }
  const [dragId, setDragId] = useState<string | null>(null)
  const initialized = useRef(restored.current) // 已有记忆就不再套用「默认全部展开」
  useEffect(() => {
    if (!defaultExpandAll || initialized.current || data.length === 0) return
    initialized.current = true
    setExpanded(new Set(collectParents(data)))
  }, [data, defaultExpandAll])

  const allParents = useMemo(() => collectParents(tree), [tree])
  const toggleNode = (id: string) => {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    commitExpanded(next)
  }

  const containsNode = (nodes: T[], ancestor: string, target: string): boolean => {
    for (const node of nodes) {
      if (node.id === ancestor) return !!node.children?.some((child) => child.id === target || containsNode([child], child.id, target))
      if (node.children && containsNode(node.children, ancestor, target)) return true
    }
    return false
  }
  const moveTree = (nodes: T[], id: string, parentId: string | null): T[] => {
    let moved: T | undefined
    const remove = (list: T[]): T[] => list.flatMap((node) => {
      if (node.id === id) { moved = node; return [] }
      return [node.children ? { ...node, children: remove(node.children) } : node]
    })
    const without = remove(nodes)
    if (!moved) return nodes
    if (parentId === null) return [...without, moved]
    const append = (list: T[]): T[] => list.map((node) => {
      if (node.id === parentId) return { ...node, children: [...(node.children ?? []), moved!] }
      return node.children ? { ...node, children: append(node.children) } : node
    })
    return append(without)
  }

  const onDragStart = (e: DragStartEvent) => setDragId(String(e.active.id))
  const onDragEnd = async (e: DragEndEvent) => {
    setDragId(null)
    if ((!onReorder && !onMove) || !e.over || e.active.id === e.over.id) return
    const parentId = parseGroup(e.active.data.current?.sortable?.containerId)
    const overParent = parseGroup(e.over.data.current?.sortable?.containerId)
    if (parentId === undefined) return
    const active = String(e.active.id)
    const rootDrop = e.over.id === ROOT_DROP_ID
    const target = rootDrop ? null : String(e.over.id)
    const targetParent = rootDrop ? null : target
    if (rootDrop || parentId !== overParent) {
      if (!onMove || (target && containsNode(tree, active, target))) return
      setOverride(moveTree(tree, active, targetParent))
      try {
        await onMove(active, targetParent)
      } catch {
        setOverride(null)
        toast.error(t('dataTable.reorderFailed'))
      }
      return
    }
    if (!onReorder) return
    const list = siblingsOf(tree, parentId)
    if (!list) return
    const ids = list.map((n) => n.id)
    const from = ids.indexOf(String(e.active.id))
    const to = ids.indexOf(String(e.over.id))
    if (from < 0 || to < 0) return
    const ordered = arrayMove(ids, from, to)
    setOverride(reorderTree(tree, parentId, ordered))
    try {
      await onReorder(parentId, ordered)
    } catch {
      setOverride(null)
      toast.error(t('dataTable.reorderFailed'))
    }
  }

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

  const isOpen = (n: T) => !!n.children?.length && (forceExpandAll || expanded.has(n.id)) && dragId !== n.id

  const renderToggle = (node: T, big?: boolean) => {
    const size = big ? 'h-9 w-9' : 'h-8 w-8'
    if (!node.children?.length) return <span className={cn('inline-block shrink-0', size)} aria-hidden="true" />
    const open = forceExpandAll || expanded.has(node.id)
    return (
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className={cn('shrink-0', size)}
        aria-expanded={open}
        aria-label={open ? t('dataTable.collapse') : t('dataTable.expand')}
        onClick={(e) => {
          e.stopPropagation()
          toggleNode(node.id)
        }}
      >
        {open ? (
          <ChevronDown className="h-4 w-4" aria-hidden="true" />
        ) : (
          <ChevronRight className="h-4 w-4 rtl:rotate-180" aria-hidden="true" />
        )}
      </Button>
    )
  }

  const colSpan = visibleColumns.length + (onReorder ? 1 : 0) + (rowActions ? 1 : 0)
  const empty = !loading && tree.length === 0

  const wrapGroup = (parentId: string | null, nodes: T[], content: ReactNode) =>
    onReorder ? (
      <SortableContext
        id={groupId(parentId)}
        items={nodes.map((n) => n.id)}
        strategy={verticalListSortingStrategy}
      >
        {content}
      </SortableContext>
    ) : (
      content
    )

  const renderRows = (nodes: T[], parentId: string | null, depth: number): ReactNode =>
    wrapGroup(
      parentId,
      nodes,
      nodes.map((node, index) => {
        const cells = (
          <>
            {onReorder ? (
              <TableCell className="w-14 py-0">
                <SortableHandle />
              </TableCell>
            ) : null}
            {visibleColumns.map((c, ci) => (
              <TableCell key={c.key} className={cn('whitespace-nowrap', alignClass(c.align))}>
                {ci === 0 ? (
                  <div className={cn('flex items-center gap-1', INDENT[Math.min(depth, INDENT.length - 1)])}>
                    {renderToggle(node)}
                    <div className="min-w-0">{renderCell(c, node, index)}</div>
                  </div>
                ) : (
                  renderCell(c, node, index)
                )}
              </TableCell>
            ))}
            {rowActions ? (
              <TableCell className="whitespace-nowrap text-end" onClick={(e) => e.stopPropagation()}>
                <div className="flex items-center justify-end gap-1">{rowActions(node)}</div>
              </TableCell>
            ) : null}
          </>
        )
        return (
          <Fragment key={node.id}>
            {onReorder ? (
              <SortableItem id={node.id} as="tr" className={cn(onRowClick && 'cursor-pointer')} {...clickProps(node)}>
                {cells}
              </SortableItem>
            ) : (
              <TableRow className={cn(onRowClick && 'cursor-pointer')} {...clickProps(node)}>
                {cells}
              </TableRow>
            )}
            {isOpen(node) ? renderRows(node.children ?? [], node.id, depth + 1) : null}
          </Fragment>
        )
      }),
    )

  const desktop = (
    <div className="card-edge overflow-hidden rounded-xl border border-border bg-card">
      <Table>
        <TableHeader>
          <TableRow>
            {onReorder ? (
              <TableHead className="w-14">
                <span className="sr-only">{t('dataTable.dragHandle')}</span>
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
            {rowActions ? <TableHead className="text-end">{t('common.actions')}</TableHead> : null}
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
              <TableCell colSpan={colSpan}>
                <EmptyState title={emptyText} />
              </TableCell>
            </TableRow>
          ) : null}
          {!loading ? renderRows(tree, null, 0) : null}
        </TableBody>
      </Table>
    </div>
  )

  const renderCards = (nodes: T[], parentId: string | null, depth: number): ReactNode =>
    wrapGroup(
      parentId,
      nodes,
      nodes.map((node, index) => {
        const card = (
          <div
            className={
              dense
                ? 'rounded-lg border border-border bg-card px-2 py-1 transition-colors hover:bg-muted/50'
                : 'card-edge rounded-xl border border-border bg-card px-3 py-2.5'
            }
          >
            <MobileCardContent
              dense={dense}
              columns={visibleColumns}
              row={node}
              index={index}
              onClick={onRowClick ? () => onRowClick(node) : undefined}
              leading={
                <>
                  {onReorder ? <SortableHandle /> : null}
                  {renderToggle(node, true)}
                </>
              }
              actions={rowActions ? rowActions(node) : undefined}
            />
          </div>
        )
        const children = isOpen(node) ? (
          <div className="ms-4 mt-3 flex flex-col gap-3 border-s border-border ps-3">
            {renderCards(node.children ?? [], node.id, depth + 1)}
          </div>
        ) : null
        return onReorder ? (
          <SortableItem key={node.id} id={node.id}>
            {card}
            {children}
          </SortableItem>
        ) : (
          <div key={node.id}>
            {card}
            {children}
          </div>
        )
      }),
    )

  const mobile = (
    <div className={dense ? 'flex flex-col gap-1.5' : 'flex flex-col gap-3'}>
      {loading
        ? Array.from({ length: 4 }, (_, i) => (
            <div key={i} className="space-y-2 rounded-lg border border-border bg-card p-3">
              <Skeleton className="h-5 w-2/3" />
              <Skeleton className="h-4 w-full" />
            </div>
          ))
        : null}
      {empty ? (
        <div className="rounded-lg border border-border bg-card">
          <EmptyState title={emptyText} />
        </div>
      ) : null}
      {!loading ? renderCards(tree, null, 0) : null}
    </div>
  )

  const list = isMobile ? mobile : desktop

  return (
    <div ref={rootRef} className={cn('flex flex-col gap-3', className)}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{toolbar}</div>
        <div className="flex items-center gap-1.5 md:gap-2">
          {allParents.length > 0 ? (
            <>
              <Button type="button" variant="outline" size="sm" onClick={() => commitExpanded(new Set(allParents))}>
                {t('dataTable.expandAll')}
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={() => commitExpanded(new Set())}>
                {t('dataTable.collapseAll')}
              </Button>
            </>
          ) : null}
          {columnSettingsKey && !isMobile ? (
            <ColumnSettings columns={columns} hidden={hidden} onToggle={toggle} onReset={reset} />
          ) : null}
        </div>
      </div>

      {(onReorder || onMove) ? (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          modifiers={[restrictToVertical]}
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
          onDragCancel={() => setDragId(null)}
        >
          {onMove ? <RootDropZone /> : null}
          {list}
        </DndContext>
      ) : (
        list
      )}
    </div>
  )
}
