import { useCallback, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { DndContext, PointerSensor, useDraggable, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { CSS } from '@dnd-kit/utilities'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { DragCursorHost } from '@/framework/components/table/use-sortable-sensors'
import type { ScreenItem } from './api'
import { canPlace, clampItem, GRID_COLS, GRID_ROWS } from './chartData'
import { builtinText } from './builtinText'

const ROW = 24

interface ItemProps {
  item: ScreenItem
  name: string
  selected: boolean
  cellW: number
  onSelect: () => void
  onChange: (next: ScreenItem) => boolean
  onRemove: () => void
}

function CanvasItem({ item, name, selected, cellW, onSelect, onChange, onRemove }: ItemProps) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({ id: item.chartId })

  const onKey = (e: ReactKeyboardEvent) => {
    const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[e.key]
    if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault()
      onRemove()
    } else if (d) {
      e.preventDefault()
      onChange(e.shiftKey ? { ...item, w: item.w + d[0], h: item.h + d[1] } : { ...item, x: item.x + d[0], y: item.y + d[1] })
    }
  }
  const startResize = (e: ReactPointerEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const sx = e.clientX
    const sy = e.clientY
    const base = item
    const move = (ev: PointerEvent) => {
      const w = base.w + Math.round((ev.clientX - sx) / cellW)
      const h = base.h + Math.round((ev.clientY - sy) / ROW)
      onChange({ ...base, w, h })
    }
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
  }

  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      role="button"
      tabIndex={0}
      aria-label={t('plugin_report.screen.itemAria', { name, x: item.x, y: item.y, w: item.w, h: item.h })}
      aria-pressed={selected}
      onClick={onSelect}
      onFocus={onSelect}
      onKeyDown={onKey}
      className={cn(
        'absolute flex touch-none select-none flex-col overflow-hidden rounded-lg border bg-card text-xs text-card-foreground shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        selected ? 'border-primary' : 'border-border',
        isDragging && 'z-10 opacity-80',
      )}
      style={{ insetInlineStart: item.x * cellW + 2, top: item.y * ROW + 2, width: item.w * cellW - 4, height: item.h * ROW - 4, transform: CSS.Translate.toString(transform) }}
    >
      <span className="truncate bg-muted px-2 py-1 font-medium">{builtinText(t, item.title || name)}</span>
      <span className="num m-auto text-muted-foreground">
        {item.w} × {item.h}
      </span>
      <span
        aria-hidden="true"
        onPointerDown={startResize}
        className="absolute bottom-0 end-0 size-4 cursor-se-resize rounded-ss-md bg-primary"
      />
    </div>
  )
}

interface Props {
  items: ScreenItem[]
  names: Record<string, string>
  selected: number
  onSelect: (i: number) => void
  onChange: (items: ScreenItem[]) => void
  onReject: () => void
}

/** 画布式布局编辑器：24 列栅格，拖拽移动、右下角手柄缩放、键盘方向键移动（Shift+方向键缩放，Delete 删除）。 */
export function EditorCanvas({ items, names, selected, onSelect, onChange, onReject }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }))

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const update = () => setWidth(el.clientWidth)
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const cellW = width / GRID_COLS
  const rows = Math.min(GRID_ROWS, Math.max(20, ...items.map((i) => i.y + i.h + 6)))
  const apply = useCallback(
    (idx: number, next: ScreenItem): boolean => {
      const cand = clampItem(next)
      if (!canPlace(items, idx, cand)) {
        onReject()
        return false
      }
      onChange(items.map((o, i) => (i === idx ? cand : o)))
      return true
    },
    [items, onChange, onReject],
  )
  const onDragEnd = (e: DragEndEvent) => {
    const idx = items.findIndex((i) => i.chartId === e.active.id)
    if (idx < 0 || cellW <= 0) return
    const it = items[idx]
    apply(idx, { ...it, x: it.x + Math.round(e.delta.x / cellW), y: it.y + Math.round(e.delta.y / ROW) })
  }

  return (
    <DndContext sensors={sensors} onDragEnd={onDragEnd}>
      <DragCursorHost />
      <div
        ref={ref}
        dir="ltr"
        className="relative w-full overflow-hidden rounded-lg border border-dashed border-border bg-muted/30"
        style={{
          height: rows * ROW,
          backgroundImage: 'linear-gradient(to right, hsl(var(--border) / 0.5) 1px, transparent 1px), linear-gradient(to bottom, hsl(var(--border) / 0.5) 1px, transparent 1px)',
          backgroundSize: `${cellW || 1}px ${ROW}px`,
        }}
      >
        {width > 0
          ? items.map((it, i) => (
              <CanvasItem
                key={it.chartId}
                item={it}
                name={names[it.chartId] ?? it.chartId}
                selected={i === selected}
                cellW={cellW}
                onSelect={() => onSelect(i)}
                onChange={(n) => apply(i, n)}
                onRemove={() => {
                  onChange(items.filter((_, j) => j !== i))
                  onSelect(-1)
                }}
              />
            ))
          : null}
      </div>
    </DndContext>
  )
}
