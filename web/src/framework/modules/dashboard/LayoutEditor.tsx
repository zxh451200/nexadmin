import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import { closestCenter, DndContext, type Announcements, type DragEndEvent, type UniqueIdentifier } from '@dnd-kit/core'
import { arrayMove, rectSortingStrategy, SortableContext, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { ArrowDown, ArrowUp, Eye, GripVertical, Loader2, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { useSortableSensors } from '@/framework/components/table/use-sortable-sensors'
import { Button } from '@/framework/ui/button'
import { Switch } from '@/framework/ui/switch'
import { cn } from '@/lib/utils'
import { CARD_GRID, CARD_SPAN } from './card-size'
import { moveVisible, sameLayout } from './layout'
import type { DashboardCard, SavedLayout } from './types'

interface Props {
  cards: DashboardCard[]
  /** 进入编辑时的布局（已经过 mergeLayout，order 含全部卡片） */
  base: SavedLayout
  saving: boolean
  /** 保存；返回 true 表示成功（由调用方退出编辑），失败保留编辑状态 */
  onSave: (layout: SavedLayout) => Promise<boolean>
  onExit: () => void
  /** 恢复默认（删除已保存布局）；成功后由调用方退出编辑 */
  onReset: () => Promise<void>
}

interface TileProps {
  card: DashboardCard
  title: string
  index: number
  total: number
  onMove: (id: string, delta: -1 | 1) => void
  onHide: (id: string) => void
}

function EditTile({ card, title, index, total, onMove, onHide }: TileProps) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: card.id })
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    position: isDragging ? 'relative' : undefined,
    zIndex: isDragging ? 10 : undefined,
  }
  return (
    <div ref={setNodeRef} style={style} className={cn('min-w-0', CARD_SPAN[card.size])}>
      <div
        className={cn(
          'flex min-h-20 flex-wrap items-center gap-x-2 gap-y-1 rounded-lg border border-dashed border-border bg-card p-2',
          isDragging && 'border-primary bg-muted shadow-md',
        )}
      >
        <button
          type="button"
          ref={setActivatorNodeRef}
          {...attributes}
          {...listeners}
          aria-label={t('dashboard.layout.dragHandle', { title })}
          title={t('dashboard.layout.dragHandle', { title })}
          className="flex size-10 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
        >
          <GripVertical className="size-5" aria-hidden="true" />
        </button>
        <div className="min-w-0 flex-1 basis-24">
          <p className="truncate text-sm font-medium">{title}</p>
          <p className="text-xs text-muted-foreground">{t(`dashboard.layout.size.${card.size}`)}</p>
        </div>
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-10"
            disabled={index === 0}
            aria-label={t('dashboard.layout.moveUp', { title })}
            title={t('dashboard.layout.moveUp', { title })}
            onClick={() => onMove(card.id, -1)}
          >
            <ArrowUp className="size-4" aria-hidden="true" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-10"
            disabled={index === total - 1}
            aria-label={t('dashboard.layout.moveDown', { title })}
            title={t('dashboard.layout.moveDown', { title })}
            onClick={() => onMove(card.id, 1)}
          >
            <ArrowDown className="size-4" aria-hidden="true" />
          </Button>
          <label className="flex min-h-10 min-w-10 cursor-pointer items-center justify-center gap-2 px-1">
            <Switch checked onCheckedChange={() => onHide(card.id)} aria-label={t('dashboard.layout.visibleSwitch', { title })} />
          </label>
        </div>
      </div>
    </div>
  )
}

/** 编辑布局：拖动（dnd-kit）/ 上移下移 / 显示隐藏，底部操作条 保存·取消·恢复默认 */
export function LayoutEditor({ cards, base, saving, onSave, onExit, onReset }: Props) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const sensors = useSortableSensors()
  const [draft, setDraft] = useState<SavedLayout>(base)
  const [live, setLive] = useState('')

  const byId = useMemo(() => new Map(cards.map((c) => [c.id, c])), [cards])
  const titleOf = useCallback((id: UniqueIdentifier) => t(byId.get(String(id))?.title ?? String(id)), [byId, t])
  const hiddenSet = useMemo(() => new Set(draft.hidden), [draft.hidden])
  const visibleIds = useMemo(() => draft.order.filter((id) => !hiddenSet.has(id) && byId.has(id)), [draft.order, hiddenSet, byId])
  const hiddenIds = useMemo(() => draft.order.filter((id) => hiddenSet.has(id) && byId.has(id)), [draft.order, hiddenSet, byId])
  const dirty = !sameLayout(draft, base)

  // 刷新/关闭浏览器标签页时提示未保存（BrowserRouter 没有路由拦截；切换工作台标签会保留草稿）
  useEffect(() => {
    if (!dirty) return
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [dirty])

  const move = (id: string, delta: -1 | 1) => {
    const next = moveVisible(draft.order, draft.hidden, id, delta)
    if (next === draft.order) return
    setDraft({ ...draft, order: next })
    const pos = next.filter((x) => !hiddenSet.has(x)).indexOf(id) + 1
    setLive(t('dashboard.layout.announce.moved', { title: titleOf(id), pos, total: visibleIds.length }))
  }

  const hide = (id: string) => {
    setDraft({ ...draft, hidden: [...draft.hidden, id] })
    setLive(t('dashboard.layout.announce.hidden', { title: titleOf(id) }))
  }

  const show = (id: string) => {
    // 恢复的卡片放到可见卡片末尾
    setDraft({ order: [...draft.order.filter((x) => x !== id), id], hidden: draft.hidden.filter((x) => x !== id) })
    setLive(t('dashboard.layout.announce.shown', { title: titleOf(id) }))
  }

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    if (!over || active.id === over.id) return
    const from = visibleIds.indexOf(String(active.id))
    const to = visibleIds.indexOf(String(over.id))
    if (from < 0 || to < 0) return
    // 在可见序列里换位，再回填到含隐藏项的完整顺序（隐藏项位置不变）
    const moved = arrayMove(visibleIds, from, to)
    const slots = draft.order.map((x, i) => (hiddenSet.has(x) || !byId.has(x) ? -1 : i)).filter((i) => i >= 0)
    const order = [...draft.order]
    slots.forEach((slot, k) => {
      order[slot] = moved[k]
    })
    setDraft({ ...draft, order })
  }

  const announcements: Announcements = {
    onDragStart: ({ active }) => t('dashboard.layout.announce.dragStart', { title: titleOf(active.id) }),
    onDragOver: ({ active, over }) =>
      over ? t('dashboard.layout.announce.dragOver', { title: titleOf(active.id), pos: visibleIds.indexOf(String(over.id)) + 1, total: visibleIds.length }) : undefined,
    onDragEnd: ({ active, over }) =>
      over ? t('dashboard.layout.announce.dragEnd', { title: titleOf(active.id), pos: visibleIds.indexOf(String(over.id)) + 1, total: visibleIds.length }) : undefined,
    onDragCancel: ({ active }) => t('dashboard.layout.announce.dragCancel', { title: titleOf(active.id) }),
  }

  const cancel = async () => {
    if (dirty && !(await confirm({ title: t('dashboard.layout.discardTitle'), description: t('dashboard.layout.discardDesc'), destructive: true, confirmText: t('dashboard.layout.discard') }))) return
    onExit()
  }

  const reset = async () => {
    if (!(await confirm({ title: t('dashboard.layout.resetTitle'), description: t('dashboard.layout.resetDesc'), destructive: true }))) return
    await onReset()
  }

  return (
    <div className="flex flex-col gap-3 md:gap-4">
      <p className="text-sm text-muted-foreground">{t('dashboard.layout.hint')}</p>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={onDragEnd}
        accessibility={{ announcements, screenReaderInstructions: { draggable: t('dashboard.layout.dragInstructions') } }}
      >
        <SortableContext items={visibleIds} strategy={rectSortingStrategy}>
          <div className={CARD_GRID}>
            {visibleIds.map((id, i) => (
              <EditTile key={id} card={byId.get(id)!} title={titleOf(id)} index={i} total={visibleIds.length} onMove={move} onHide={hide} />
            ))}
          </div>
        </SortableContext>
      </DndContext>

      <section aria-labelledby="dashboard-hidden-title" className="flex flex-col gap-2 rounded-lg border border-border p-3">
        <h2 id="dashboard-hidden-title" className="text-sm font-medium">
          {t('dashboard.layout.hiddenTitle', { count: hiddenIds.length })}
        </h2>
        {hiddenIds.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('dashboard.layout.hiddenEmpty')}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {hiddenIds.map((id) => (
              <li key={id} className="flex min-h-12 items-center gap-2 rounded-md bg-muted/50 px-3 py-1">
                <span className="min-w-0 flex-1 truncate text-sm">{titleOf(id)}</span>
                <Button type="button" variant="outline" className="min-h-10" onClick={() => show(id)} aria-label={t('dashboard.layout.restore', { title: titleOf(id) })}>
                  <Eye className="me-1.5 size-4" aria-hidden="true" />
                  {t('dashboard.layout.show')}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <p role="status" aria-live="polite" className="sr-only">
        {live}
      </p>

      <div className="sticky bottom-0 z-20 -mx-3 flex flex-wrap items-center justify-between gap-2 border-t border-border bg-background/95 px-3 py-3 backdrop-blur md:-mx-6 md:px-6">
        <Button type="button" variant="ghost" className="min-h-10" disabled={saving} onClick={() => void reset()}>
          <RotateCcw className="me-1.5 size-4" aria-hidden="true" />
          {t('dashboard.layout.reset')}
        </Button>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" className="min-h-10" disabled={saving} onClick={() => void cancel()}>
            {t('dashboard.layout.cancel')}
          </Button>
          <Button type="button" className="min-h-10" disabled={saving || !dirty} onClick={() => void onSave(draft)}>
            {saving ? <Loader2 className="me-1.5 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" /> : null}
            {t('dashboard.layout.save')}
          </Button>
        </div>
      </div>
    </div>
  )
}
