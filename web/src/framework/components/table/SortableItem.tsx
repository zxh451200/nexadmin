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
}

/** 可排序行/卡片；children 内用 <SortableHandle /> 作为拖动手柄 */
export function SortableItem({ id, as = 'div', className, children, ...rest }: ItemProps) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } =
    useSortable({ id })
  const style: CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    position: isDragging ? 'relative' : undefined,
    zIndex: isDragging ? 10 : undefined,
  }
  const cls = cn(isDragging && 'bg-muted shadow-md', className)
  return (
    <Ctx.Provider value={{ attributes, listeners, setActivatorNodeRef }}>
      {as === 'tr' ? (
        <TableRow ref={setNodeRef} style={style} className={cls} {...rest}>
          {children}
        </TableRow>
      ) : (
        <div ref={setNodeRef} style={style} className={cls} {...rest}>
          {children}
        </div>
      )}
    </Ctx.Provider>
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
      aria-label={t('dataTable.dragHandle')}
      onClick={(e) => e.stopPropagation()}
      className={cn(
        'flex h-10 w-8 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing md:h-9 md:w-9',
        className,
      )}
    >
      <GripVertical className="h-5 w-5" aria-hidden="true" />
    </button>
  )
}
