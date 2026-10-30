import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DndContext, DragOverlay, type DragEndEvent, type DragStartEvent, type Modifier } from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { useCompactLayout } from './table/use-compact-layout'
import { useOverflowX } from './table/use-overflow-x'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/framework/ui/table'
import { cn } from '@/lib/utils'
import { EmptyState } from './EmptyState'
import { ExpandAllToggle } from './ExpandAllToggle'
import { ColumnSettings } from './table/ColumnSettings'
import { MobileCardContent } from './table/MobileCard'
import { DragChip, SortableHandle, SortableItem } from './table/SortableItem'
import { alignClass, renderCell, type Column } from './table/types'
import { useColumnSettings } from './table/use-column-settings'
import { pointerFirstCollision, useDragCursor, useEdgeAutoScroll, useSortableSensors } from './table/use-sortable-sensors'

export interface TreeTableProps<T extends { id: string; children?: T[] }> {
  columns: Column<T>[]
  data: T[]
  rowActions?: (row: T) => ReactNode
  /** 同一父级内拖动排序；根级 parentId 为 null */
  onReorder?: (parentId: string | null, orderedIds: string[]) => void | Promise<unknown>
  /** 跨父级移动：拖到目标节点上会成为其最后一个子节点。 */
  /**
   * 跨父级移动；afterId 表示插到该同级之后（空串=插到最前，undefined=追加到末尾）。
   * 落点是插入线，能力上必须能表达「插在第 N 行之后」。
   */
  onMove?: (id: string, parentId: string | null, afterId?: string) => void | Promise<unknown>
  /**
   * 拖到目标行右侧时先问一句「能不能挂到它下面」，返回 i18n key 就拦截并提示。
   * 用于「按钮不能有子菜单」这类只有页面知道的规则，省掉一次必然失败的服务端往返。
   */
  blockNest?: (active: T, target: T) => string | null
  /** 最大层级（根 = 1）；给了就在拖拽阶段拦截，不让用户拖出必然失败的层级 */
  maxDepth?: number
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
/** 横向位移超过多少像素视为「改变层级」而不是排序 */
const NEST_OFFSET = 24
/** 落点线从手柄列右侧起画：手柄 36px 是抓取区，线端圆点压在它下面会贴住六点手柄 */
const DROP_LINE_INSET = 48
/** 横向跟手打五折：位移看得见（这就是「往右拖=降级」的手势反馈），又不至于把行甩出滚动容器 */
const dampX: Modifier = ({ transform }) => ({ ...transform, x: transform.x * 0.5 })

const groupId = (parentId: string | null) => `g:${parentId ?? ''}`

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
  blockNest,
  maxDepth,
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
  /** 是否展开（forceExpandAll 时一律展开） */
  const isOpen = (n: T) => !!n.children?.length && (forceExpandAll || expanded.has(n.id))
  const commitExpanded = (next: Set<string>) => {
    // 刚展开的子树做一次入场动画（单行展开与「全部展开」都走这里）
    const opened = [...next].filter((id) => !expanded.has(id))
    if (opened.length) {
      const ids = new Set<string>()
      const walk = (n: T) => {
        for (const child of n.children ?? []) {
          ids.add(child.id)
          walk(child)
        }
      }
      for (const id of opened) {
        const node = findNode(tree, id)
        if (node) walk(node)
      }
      // 不去清这个标记：CSS 动画在行首次带上该类时播一次，
      // 若中途移除类，动画会被截断成「闪一下」（实测只显示 2 帧）。
      if (ids.size) setRevealed((prev) => new Set([...prev, ...ids]))
    }
    setExpanded(next)
    try {
      localStorage.setItem(storageKey, JSON.stringify([...next]))
    } catch {
      // 存储不可用时仅本次生效
    }
  }
  const [dragId, setDragId] = useState<string | null>(null)
  // 浮层内容：起拖时按 id 从数据里取第一列的单元格，宽度取被拖行的实际宽度
  const [dragChip, setDragChip] = useState<{ label: ReactNode; width: number } | null>(null)
  // 拖动中的「松手会发生什么」预览：右移=降为子级、左移=升一级、虚线框=回顶级；被 blockNest 拦下时给原因
  useDragCursor(!!dragId)
  // 拖到视口边缘时自动滚动（长表必需）
  useEdgeAutoScroll(!!dragId, () => scrollRef.current)
  const initialized = useRef(restored.current) // 已有记忆就不再套用「默认全部展开」
  useEffect(() => {
    if (!defaultExpandAll || initialized.current || data.length === 0) return
    initialized.current = true
    setExpanded(new Set(collectParents(data)))
  }, [data, defaultExpandAll])

  const allParents = useMemo(() => collectParents(tree), [tree])
  // 展开动画：由 commitExpanded 标记「刚露出来的子树」，CSS 负责那一小段淡入+位移。
  // 收起是瞬间的——表格行没有可过渡的高度容器，硬做会出现行高跳变。
  const [revealed, setRevealed] = useState<Set<string>>(() => new Set())

  const toggleNode = (id: string) => {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    commitExpanded(next)
  }

  const depthOf = (nodes: T[], id: string | null, level = 0): number | undefined => {
    if (id === null) return level - 1
    for (const node of nodes) {
      if (node.id === id) return level
      const hit = node.children?.length ? depthOf(node.children, id, level + 1) : undefined
      if (hit !== undefined) return hit
    }
    return undefined
  }
  const heightOf = (node: T): number => 1 + Math.max(0, ...(node.children ?? []).map(heightOf))
  const findNode = (nodes: T[], id: string): T | undefined => {
    for (const node of nodes) {
      if (node.id === id) return node
      const hit = node.children?.length ? findNode(node.children, id) : undefined
      if (hit) return hit
    }
    return undefined
  }

  /** id 的父级：根级节点返回 null，找不到返回 undefined */
  const parentOf = (nodes: T[], id: string | null): string | null | undefined => {
    if (id === null) return null
    const walk = (list: T[], parent: string | null): string | null | undefined => {
      for (const node of list) {
        if (node.id === id) return parent
        if (node.children?.length) {
          const hit = walk(node.children, node.id)
          if (hit !== undefined) return hit
        }
      }
      return undefined
    }
    return walk(nodes, null)
  }
  const containsNode = (nodes: T[], ancestor: string, target: string): boolean => {
    for (const node of nodes) {
      if (node.id === ancestor) return !!node.children?.some((child) => child.id === target || containsNode([child], child.id, target))
      if (node.children && containsNode(node.children, ancestor, target)) return true
    }
    return false
  }
  const moveTree = (nodes: T[], id: string, parentId: string | null, afterId?: string): T[] => {
    let moved: T | undefined
    const remove = (list: T[]): T[] => list.flatMap((node) => {
      if (node.id === id) { moved = node; return [] }
      return [node.children ? { ...node, children: remove(node.children) } : node]
    })
    const without = remove(nodes)
    if (!moved) return nodes
    // afterId 未给=追加到末尾；空串=插到最前；否则插到该同级之后
    const insertAt = (list: T[]): T[] => {
      const at = afterId === undefined ? list.length : afterId === '' ? 0 : list.findIndex((n) => n.id === afterId) + 1
      const copy = [...list]
      copy.splice(at < 0 ? list.length : at, 0, moved!)
      return copy
    }
    if (parentId === null) return insertAt(without)
    const descend = (list: T[]): T[] => list.map((node) => {
      if (node.id === parentId) return { ...node, children: insertAt(node.children ?? []) }
      return node.children ? { ...node, children: descend(node.children) } : node
    })
    return descend(without)
  }

  // ---- 拖动落点：插入线 + 缩进（Notion / Airtable / antd Tree 同一套）----
  // 线的纵向位置 = 插到哪一行前后，左缩进 = 落成第几级。不再有投放框，也不整行高亮。
  const [pointer, setPointer] = useState<{ x: number; y: number; startX: number } | null>(null)

  useEffect(() => {
    if (!dragId) return
    const track = (ev: PointerEvent) => setPointer((p) => (p ? { ...p, x: ev.clientX, y: ev.clientY } : p))
    window.addEventListener('pointermove', track, { passive: true })
    return () => window.removeEventListener('pointermove', track)
  }, [dragId])

  /** 拖动开始时快照一次行坐标：排序动画会改行位置，实时读 DOM 会「套住自己」来回跳 */
  type RowBox = { top: number; bottom: number; left: number; right: number; handleLeft: number }
  const [rects, setRects] = useState<Map<string, RowBox>>(() => new Map())

  const snapshotRects = () => {
    const next = new Map<string, RowBox>()
    for (const el of document.querySelectorAll<HTMLElement>('[data-sortable-id]')) {
      const id = el.dataset.sortableId
      if (!id) continue
      const r = el.getBoundingClientRect()
      const handle = el.querySelector<HTMLElement>('button[aria-label]')?.getBoundingClientRect()
      next.set(id, { top: r.top, bottom: r.bottom, left: r.left, right: r.right, handleLeft: handle?.left ?? r.left + 8 })
    }
    setRects(next)
  }

  /** 某个父级子树最后一行的底边：append 进去时会落在它下面（用快照坐标） */
  const appendAnchor = (parentId: string | null) => {
    const list = parentId === null ? tree : (findNode(tree, parentId)?.children ?? [])
    if (!list.length) return null
    const deepest = (n: T): T => (isOpen(n) && n.children?.length ? deepest(n.children[n.children.length - 1]) : n)
    const last = deepest(list[list.length - 1])
    return rects.get(last.id)?.bottom ?? null
  }

  type DropPlan = {
    y: number
    left: number
    width: number
    mode: 'reorder' | 'move' | 'blocked'
    parentId: string | null
    afterId?: string
    edge?: 'before' | 'after'
    overId: string
    /** 落点所在的层级（0=顶级），用于在指示器上直接标出"会放到第几级" */
    depth: number
    reason?: string
  }

  const drop = useMemo<DropPlan | null>(() => {
    if (!dragId || !pointer || rects.size === 0 || (!onMove && !onReorder)) return null
    const active = dragId
    const activeNode = findNode(tree, active)
    if (!activeNode) return null

    // 指针落在谁的快照行里（跳过自己；越过首尾时贴到首行/末行）
    let overId: string | null = null
    let box: RowBox | null = null
    for (const [id, r] of rects) {
      if (id === active) continue
      if (pointer.y >= r.top && pointer.y <= r.bottom) {
        overId = id
        box = r
        break
      }
    }
    if (!overId) {
      // 落在行与行之间的空隙（表格行有 padding）：取纵向最近的一行，而不是"贴到末行"——
      // 后者会让指针一进空隙目标就跳到表格底部，手感就是"拖不动/乱跳"。
      const entries = [...rects.entries()].filter(([id]) => id !== active)
      if (!entries.length) return null
      const mid = (r: RowBox) => (r.top + r.bottom) / 2
      entries.sort((a, b) => Math.abs(pointer.y - mid(a[1])) - Math.abs(pointer.y - mid(b[1])))
      const pick = entries[0]
      overId = pick[0]
      box = pick[1]
    }
    const overNode = findNode(tree, overId)
    if (!overNode || !box || containsNode(tree, active, overId)) return null
    const dx = pointer.x - pointer.startX
    const activeParent = parentOf(tree, active) ?? null
    const overParent = parentOf(tree, overId) ?? null
    const firstHandle = rects.values().next().value?.handleLeft ?? box.left + 8

    const plan = (o: {
      y: number
      indentBase: number | null
      depth: number
      mode: DropPlan['mode']
      parentId: string | null
      afterId?: string
      edge?: 'before' | 'after'
      reason?: string
    }): DropPlan => {
      const left = (o.indentBase ?? firstHandle) + DROP_LINE_INSET + Math.max(0, o.depth) * 24
      return {
        y: o.y,
        left,
        width: Math.max(120, box!.right - left - 8),
        mode: o.mode,
        parentId: o.parentId,
        afterId: o.afterId,
        edge: o.edge,
        overId: overId!,
        depth: o.depth,
        reason: o.reason,
      }
    }
    const siblingsOf = (pid: string | null): T[] => (pid === null ? tree : (findNode(tree, pid)?.children ?? []))
    /** 插到 overId 前/后：before 要取它的前一个同级，空串表示插到最前 */
    const afterIdAt = (edge: 'before' | 'after', pid: string | null): string => {
      if (edge === 'after') return overId
      const list = siblingsOf(pid)
      const idx = list.findIndex((n) => n.id === overId)
      return idx > 0 ? list[idx - 1].id : ''
    }

    if (dx > NEST_OFFSET && onMove) {
      // 挂到悬停行下面：落在它最后一个子节点之后（没有子节点就插到最前）
      const depth = (depthOf(tree, overId) ?? 0) + 1
      const kids = overNode.children ?? []
      const afterId = kids.length ? kids[kids.length - 1].id : ''
      const y = appendAnchor(overId) ?? box.bottom
      if (maxDepth && depth + heightOf(activeNode) - 1 > maxDepth) {
        return plan({ y, indentBase: box.handleLeft, depth, mode: 'blocked', parentId: overId, afterId, reason: 'dataTable.maxDepthReached' })
      }
      const reason = blockNest?.(activeNode, overNode) ?? null
      return plan({ y, indentBase: box.handleLeft, depth, mode: reason ? 'blocked' : 'move', parentId: overId, afterId, reason: reason ?? undefined })
    }
    if (dx < -NEST_OFFSET && onMove && activeParent !== null) {
      // 升一级：插到原父级后面（位置精确，不是甩到祖父末尾）
      const grand = parentOf(tree, activeParent) ?? null
      const y = appendAnchor(grand)
      if (y === null) return null
      const depth = grand === null ? 0 : (depthOf(tree, grand) ?? 0) + 1
      return plan({
        y,
        indentBase: grand === null ? null : (rects.get(grand)?.handleLeft ?? null),
        depth,
        mode: 'move',
        parentId: grand,
        afterId: activeParent,
      })
    }
    const edge = pointer.y < box.top + (box.bottom - box.top) / 2 ? 'before' : 'after'
    if (overParent === activeParent) {
      return plan({
        y: edge === 'before' ? box.top : box.bottom,
        indentBase: box.handleLeft,
        depth: depthOf(tree, overId) ?? 0,
        mode: 'reorder',
        parentId: overParent,
        edge,
        afterId: afterIdAt(edge, overParent),
      })
    }
    // 跨组但没横向位移：插到悬停行的前/后（同样是位置精确，不是追加到组尾）
    if (!onMove) return null
    return plan({
      y: edge === 'before' ? box.top : box.bottom,
      indentBase: box.handleLeft,
      depth: depthOf(tree, overId) ?? 0,
      mode: 'move',
      parentId: overParent,
      afterId: afterIdAt(edge, overParent),
      edge,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dragId, pointer, rects, tree, expanded, onMove, onReorder, blockNest, maxDepth])

  const onDragStart = (e: DragStartEvent) => {
    const id = String(e.active.id)
    setDragId(id)
    const ae = e.activatorEvent as PointerEvent | undefined
    setPointer({ x: ae?.clientX ?? 0, y: ae?.clientY ?? 0, startX: ae?.clientX ?? 0 })
    snapshotRects()
    const node = findNode(tree, id)
    const first = columns[0]
    setDragChip({
      label: node && first ? renderCell(first, node, 0) : null,
      width: Math.round(e.active.rect.current.initial?.width ?? 0),
    })
  }
  const onDragCancel = () => {
    setDragId(null)
    setDragChip(null)
    setPointer(null)
  }

  const moveTo = async (id: string, parentId: string | null, afterId?: string) => {
    if (!onMove) return
    if (parentId !== null) commitExpanded(new Set(expanded).add(parentId))
    setOverride(moveTree(tree, id, parentId, afterId))
    try {
      await onMove(id, parentId, afterId)
    } catch {
      setOverride(null)
      toast.error(t('dataTable.reorderFailed'))
    }
  }

  // ---- 键盘重排：拖拽不稳时的确定路径（Notion / Linear 都提供）----
  // 只用 Alt 组合键：不劫持 Tab（那是焦点导航），也不和 dnd-kit 的「空格起拖 + 方向键移动」冲突。
  const nudgeRow = async (id: string, delta: -1 | 1) => {
    if (!onReorder) return
    const pid = parentOf(tree, id) ?? null
    const list = siblingsOf(tree, pid)
    if (!list) return
    const i = list.findIndex((n) => n.id === id)
    const j = i + delta
    if (i < 0 || j < 0 || j >= list.length) return
    const ids = list.map((n) => n.id)
    const [moved] = ids.splice(i, 1)
    ids.splice(j, 0, moved)
    setOverride(reorderTree(tree, pid, ids))
    try {
      await onReorder(pid, ids)
    } catch {
      setOverride(null)
      toast.error(t('dataTable.reorderFailed'))
    }
  }

  /** 降级：成为上一个同级节点的最后一个子节点（没有上一个同级就什么都不做） */
  const indentRow = async (id: string) => {
    if (!onMove) return
    const node = findNode(tree, id)
    const pid = parentOf(tree, id) ?? null
    const list = siblingsOf(tree, pid)
    if (!node || !list) return
    const i = list.findIndex((n) => n.id === id)
    if (i <= 0) return
    const prev = list[i - 1]
    if (!prev) return
    if (maxDepth && (depthOf(tree, prev.id) ?? 0) + 1 + heightOf(node) - 1 > maxDepth) {
      toast.error(t('dataTable.maxDepthReached', { count: maxDepth }))
      return
    }
    const reason = blockNest?.(node, prev)
    if (reason) {
      toast.error(t(reason, { count: maxDepth }))
      return
    }
    const kids = prev.children ?? []
    await moveTo(id, prev.id, kids.length ? kids[kids.length - 1].id : '')
  }

  /** 升级：插到原父级之后（位置精确，不是甩到祖父末尾） */
  const outdentRow = async (id: string) => {
    if (!onMove) return
    const pid = parentOf(tree, id) ?? null
    if (pid === null) return
    const grand = parentOf(tree, pid) ?? null
    await moveTo(id, grand, pid)
  }

  const onRowKeyDown = (e: React.KeyboardEvent, id: string) => {
    if (!e.altKey) return
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      void nudgeRow(id, -1)
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      void nudgeRow(id, 1)
    } else if (e.key === 'ArrowRight') {
      e.preventDefault()
      void indentRow(id)
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault()
      void outdentRow(id)
    }
  }

  const onDragEnd = async (e: DragEndEvent) => {
    const active = String(e.active.id)
    const activeNode = findNode(tree, active)
    const plan = drop
    setDragId(null)
    setDragChip(null)
    setPointer(null)
    if (!activeNode) return

    if (plan?.mode === 'blocked') {
      toast.error(t(plan.reason ?? 'dataTable.nestBlocked', { count: maxDepth }))
      return
    }
    if (plan?.mode === 'move') {
      await moveTo(active, plan.parentId, plan.afterId)
      return
    }
    if (!onReorder) return
    // 键盘拖动没有指针：退回按 over 做同组排序
    const overId = plan?.overId ?? (e.over ? String(e.over.id) : null)
    if (!overId) return
    const parentId = parentOf(tree, overId) ?? null
    const list = siblingsOf(tree, parentId)
    if (!list) return
    const ids = list.map((n) => n.id).filter((id) => id !== active)
    const idx = ids.indexOf(overId)
    if (idx < 0) return
    ids.splice(plan?.edge === 'before' ? idx : idx + 1, 0, active)
    setOverride(reorderTree(tree, parentId, ids))
    try {
      await onReorder(parentId, ids)
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

  // 起拖时不收起被拖节点的子树：整表会在抓起的瞬间向上重排，目标位置从指针底下跑掉——
  // 这是树形表拖动「发涩/乱跳」的主因。布局保持稳定，只有被拖行压暗。

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
        <ChevronRight
          className={cn(
            'h-4 w-4 transition-transform duration-150 motion-reduce:transition-none rtl:rotate-180',
            open ? 'rotate-90 rtl:rotate-90' : 'rotate-0 rtl:rotate-180',
          )}
          aria-hidden="true"
        />
      </Button>
    )
  }

  const colSpan = visibleColumns.length + (onReorder ? 1 : 0) + (rowActions ? 1 : 0)
  // 只有表格真的横向溢出时才吸附操作列
  const { ref: scrollRef, overflowing } = useOverflowX<HTMLDivElement>()
  const pinActions = overflowing && !!rowActions
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

  // dnd-kit 自带的播报是硬编码英文；用行号重新播报，避免中文界面里给读屏用户念英文 ID
  const flatIds = useMemo(() => {
    const out: string[] = []
    const walk = (nodes: T[]) => nodes.forEach((n) => { out.push(n.id); if (n.children?.length) walk(n.children) })
    walk(tree)
    return out
  }, [tree])
  const rowno = (id: unknown) => Math.max(1, flatIds.indexOf(String(id)) + 1)
  const a11y = useMemo(
    () => ({
      announcements: {
        onDragStart: ({ active }: { active: { id: string | number } }) => t('dataTable.a11y.picked', { row: rowno(active.id) }),
        onDragOver: ({ active, over }: { active: { id: string | number }; over: { id: string | number } | null }) =>
          over ? t('dataTable.a11y.over', { row: rowno(active.id), target: rowno(over.id) }) : t('dataTable.a11y.overEmpty', { row: rowno(active.id) }),
        onDragEnd: ({ active, over }: { active: { id: string | number }; over: { id: string | number } | null }) => {
          if (!over) return t('dataTable.a11y.droppedEmpty', { row: rowno(active.id) })
          // 被规则拦下的这次拖放并没有生效，别对着读屏用户说「已放到第 N 行」
          const a = findNode(tree, String(active.id))
          const tgt = findNode(tree, String(over.id))
          const reason = a && tgt ? blockNest?.(a, tgt) ?? null : null
          return reason ? t(reason) : t('dataTable.a11y.dropped', { row: rowno(active.id), target: rowno(over.id) })
        },
        onDragCancel: ({ active }: { active: { id: string | number } }) => t('dataTable.a11y.cancelled', { row: rowno(active.id) }),
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, flatIds, tree, blockNest],
  )

  const renderRows = (nodes: T[], parentId: string | null, depth: number): ReactNode =>
    wrapGroup(
      parentId,
      nodes,
      nodes.map((node, index) => {
        // 有拖动列时缩进加在拖动列上：箭头/名称自然跟随，层级一眼可见
        const indent = INDENT[Math.min(depth, INDENT.length - 1)]
        const cells = (
          <>
            {onReorder ? (
              // 把手也按深度缩进（用 padding 而非单元格宽度，避免把后面的列整体推走）
              <TableCell className="w-14 py-0">
                <div className={indent}>
                  <span className="inline-flex" onKeyDown={(e) => onRowKeyDown(e, node.id)}>
                    <SortableHandle />
                  </span>
                </div>
              </TableCell>
            ) : null}
            {visibleColumns.map((c, ci) => (
              <TableCell key={c.key} className={cn('whitespace-nowrap', alignClass(c.align))}>
                {ci === 0 ? (
                  <div className={cn('flex items-center gap-1', indent)}>
                    {renderToggle(node)}
                    <div className="min-w-0">{renderCell(c, node, index)}</div>
                  </div>
                ) : (
                  renderCell(c, node, index)
                )}
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
                <div className="flex items-center justify-end gap-1">{rowActions(node)}</div>
              </TableCell>
            ) : null}
          </>
        )
        return (
          <Fragment key={node.id}>
            {onReorder ? (
              <SortableItem id={node.id} as="tr" className={cn('group', onRowClick && 'cursor-pointer', revealed.has(node.id) && 'tree-row-in')} {...clickProps(node)}>
                {cells}
              </SortableItem>
            ) : (
              <TableRow className={cn('group', onRowClick && 'cursor-pointer', revealed.has(node.id) && 'tree-row-in')} {...clickProps(node)}>
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
      {/* 列多时横向滚动，不许把最后一列（操作）裁掉 */}
      <div ref={scrollRef} className="overflow-x-auto">
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
              <TableCell colSpan={colSpan}>
                <EmptyState title={emptyText} />
              </TableCell>
            </TableRow>
          ) : null}
          {!loading ? renderRows(tree, null, 0) : null}
        </TableBody>
      </Table>
      </div>
    </div>
  )

  const renderCards = (nodes: T[], parentId: string | null, depth: number): ReactNode =>
    wrapGroup(
      parentId,
      nodes,
      nodes.map((node, index) => {
        const card = (
          <div
            className={cn(
              dense
                ? 'rounded-lg border border-border bg-card px-2 py-1 transition-colors hover:bg-muted/50'
                : 'card-edge rounded-xl border border-border bg-card px-3 py-2.5',
              revealed.has(node.id) && 'tree-row-in',
            )}
          >
            <MobileCardContent
              dense={dense}
              columns={visibleColumns}
              row={node}
              index={index}
              onClick={onRowClick ? () => onRowClick(node) : undefined}
              leading={
                <>
                  {onReorder ? (
                    <span className="inline-flex" onKeyDown={(e) => onRowKeyDown(e, node.id)}>
                      <SortableHandle />
                    </span>
                  ) : null}
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
            <ExpandAllToggle
              ids={allParents}
              expanded={forceExpandAll ? new Set(allParents) : expanded}
              onChange={commitExpanded}
            />
          ) : null}
          {columnSettingsKey && !isMobile ? (
            <ColumnSettings columns={columns} hidden={hidden} onToggle={toggle} onReset={reset} />
          ) : null}
        </div>
      </div>

      {(onReorder || onMove) ? (
        <DndContext
          sensors={sensors}
          collisionDetection={pointerFirstCollision}
          modifiers={[dampX]}
          accessibility={a11y}
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
          onDragCancel={onDragCancel}
        >
          {list}
          {/* 浮层挂到 body：页面里任何带 transform 的祖先都会让 position:fixed 改按它计算，
              表现就是拖拽时浮层离指针很远（偏一个侧栏/顶栏的距离）。 */}
          {createPortal(
            <DragOverlay dropAnimation={null}>
              {dragChip?.label ? <DragChip width={dragChip.width}>{dragChip.label}</DragChip> : null}
            </DragOverlay>,
            document.body,
          )}
          {dragId && drop
            ? createPortal(
                <div
                  data-drop-line="1"
                  className="pointer-events-none fixed z-[60]"
                  style={{ left: drop.left, top: drop.y - 1, width: drop.width }}
                  role="status"
                  aria-live="polite"
                >
                  {/* 落位线：3px + 外发光；两端各一个大圆点，被拖的浮层盖住一端时另一端仍能看见。
                      右侧再标一句「会放到第几级」——树形拖拽最容易搞不清的就是落点的层级。 */}
                  <div
                    className={cn(
                      'relative h-[3px] rounded-full',
                      drop.mode === 'blocked'
                        ? 'bg-destructive shadow-[0_0_10px_rgba(220,38,38,0.45)]'
                        : 'bg-primary shadow-[0_0_10px_rgba(46,52,234,0.45)]',
                    )}
                  >
                    <span
                      className={cn(
                        'absolute -start-2 -top-[5px] size-[13px] rounded-full ring-2 ring-background',
                        drop.mode === 'blocked' ? 'bg-destructive' : 'bg-primary',
                      )}
                    />
                    <span
                      className={cn(
                        'absolute -end-2 -top-[5px] size-[13px] rounded-full ring-2 ring-background',
                        drop.mode === 'blocked' ? 'bg-destructive' : 'bg-primary',
                      )}
                    />
                    {/* 落点层级一律标出来（顶级也标「第 1 级」）：树形拖拽最容易搞不清的就是会落到第几级 */}
                    <span
                      className={cn(
                        'absolute start-4 -top-[11px] whitespace-nowrap rounded-full px-2 py-0.5 text-[10px] font-medium shadow-sm',
                        drop.mode === 'blocked'
                          ? 'bg-destructive text-white'
                          : 'bg-primary text-primary-foreground',
                      )}
                    >
                      {t('dataTable.dropLevel', { level: drop.depth + 1 })}
                    </span>
                  </div>
                  {drop.mode === 'blocked' && drop.reason ? (
                    <div className="mt-1 text-xs font-medium text-destructive">{t(drop.reason, { count: maxDepth })}</div>
                  ) : null}
                </div>,
                document.body,
              )
            : null}
        </DndContext>
      ) : (
        list
      )}
    </div>
  )
}
