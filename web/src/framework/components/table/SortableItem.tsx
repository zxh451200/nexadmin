import { createContext, useContext, type CSSProperties, type ReactNode } from 'react'
import { GripVertical } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { TableRow } from '@/framework/ui/table'
import { cn } from '@/lib/utils'

type SortableCtx = Pick<ReturnType<typeof useSortable>, 'attributes' | 'listeners' | 'setActivatorNodeRef'>
const Ctx = createContext<SortableCtx | null>(null)

interface ItemProps {
  id: string
  as?: 'tr' | 'div'
  className?: string
  children: ReactNode
  onClick?: () => void
  onKeyDown?: React.KeyboardEventHandler<HTMLElement>
  tabIndex?: number
  'aria-selected'?: boolean
}

/** 可排序行/卡片；children 内用 <SortableHandle /> 作为拖动手柄 */
export function SortableItem({ id, as = 'div', className, children, ...rest }: ItemProps) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id })
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    // 正在拖的那一行不做补间动画，否则它自己也在缓动，跟手会慢半拍
    transition: isDragging ? undefined : transition,
    position: isDragging ? 'relative' : undefined,
    zIndex: isDragging ? 10 : undefined,
  }
  // 浮层（DragOverlay）才是跟着手走的那个；原地的那行压暗，表示「这是被拖走的原件」
  const cls = cn(isDragging && 'opacity-45', className)
  return (
    <Ctx.Provider value={{ attributes, listeners, setActivatorNodeRef }}>
      {as === 'tr' ? (
        <TableRow ref={setNodeRef} style={style} className={cls} data-sortable-id={id} {...rest}>
          {children}
        </TableRow>
      ) : (
        <div ref={setNodeRef} style={style} className={cls} data-sortable-id={id} {...rest}>
          {children}
        </div>
      )}
    </Ctx.Provider>
  )
}

/**
 * 拖动时跟随光标的浮层。表格行必须用浮层：行在 `overflow-x-auto` 的滚动容器里，
 * 浏览器会把纵向溢出也算作裁剪（overflow-x:auto 会让 overflow-y 变成 auto），
 * 直接 transform 整行会被容器裁掉、还会撑出滚动条——这就是「拖起来发涩」的主因。
 *
 * hint 是落点层级徽标（「顶级」「第 2 级」/ 超层级时的红字原因）：层级信息挂在浮层上，
 * 就不必在落位线旁边外挂一段文字——线尾外面通常紧接着下一列的内容，文字放过去会压住链接/状态列。
 */
export function DragChip({
  width,
  hint,
  children,
}: {
  width: number
  hint?: { text: string; danger?: boolean } | null
  children: ReactNode
}) {
  return (
    <div
      className="flex h-9 items-center gap-2 rounded-md border border-border bg-background px-3 text-sm shadow-lg"
      style={{ width: width > 0 ? width : '16rem' }}
    >
      <GripVertical className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {hint ? (
        <span
          className={cn(
            'shrink-0 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-medium',
            hint.danger ? 'bg-destructive/10 text-destructive' : 'bg-primary/10 text-primary',
          )}
        >
          {hint.text}
        </span>
      ) : null}
    </div>
  )
}

/** 触控区 44x44 的拖动手柄；触屏长按 200ms 后拖动（见 useSortableSensors） */
export function SortableHandle({ className }: { className?: string }) {
  const { t } = useTranslation()
  const ctx = useContext(Ctx)
  if (!ctx) return null
  return (
    <button
      type="button"
      ref={ctx.setActivatorNodeRef}
      {...ctx.attributes}
      {...ctx.listeners}
      data-drag-handle="1"
      aria-label={t('dataTable.dragHandle')}
      onClick={(e) => e.stopPropagation()}
      className={cn(
        'flex h-10 w-8 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing md:h-9 md:w-9',
        className,
      )}
    >
      <GripVertical className="h-5 w-5" aria-hidden="true" />
    </button>
  )
}
