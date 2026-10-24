import type { LucideIcon } from 'lucide-react'
import {
  AlignLeft,
  AlertTriangle,
  CalendarDays,
  CheckSquare,
  ChevronsUpDown,
  CircleDot,
  Eye,
  GripVertical,
  Hash,
  Heading,
  Paperclip,
  Star,
  ToggleLeft,
  Type,
} from 'lucide-react'
import { KeyboardSensor, MouseSensor, TouchSensor, closestCenter, DndContext, useSensor, useSensors, type DragEndEvent } from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { FieldType, FormField } from './api'
import { fieldProblems } from './schema'

export const FIELD_ICONS: Record<FieldType, LucideIcon> = {
  text: Type,
  textarea: AlignLeft,
  number: Hash,
  radio: CircleDot,
  checkbox: CheckSquare,
  select: ChevronsUpDown,
  date: CalendarDays,
  switch: ToggleLeft,
  rating: Star,
  file: Paperclip,
  section: Heading,
}

interface Props {
  fields: FormField[]
  selected: number
  onSelect: (index: number) => void
  onMove: (from: number, to: number) => void
}

/** 画布：字段按顺序排列，拖动手柄排序（鼠标 / 触屏长按 / 键盘空格），点击选中后在右侧编辑属性。 */
export function FieldCanvas({ fields, selected, onSelect, onMove }: Props) {
  const { t } = useTranslation()
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 4 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 200, tolerance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const onDragEnd = (e: DragEndEvent) => {
    if (!e.over || e.active.id === e.over.id) return
    const from = fields.findIndex((f) => f.key === e.active.id)
    const to = fields.findIndex((f) => f.key === e.over?.id)
    if (from >= 0 && to >= 0) onMove(from, to)
  }

  if (fields.length === 0) {
    return (
      <p role="status" className="rounded-lg border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
        {t('plugin_formbuilder.designer.emptyCanvas')}
      </p>
    )
  }
  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={fields.map((f) => f.key)} strategy={verticalListSortingStrategy}>
        <ul className="flex flex-col gap-2">
          {fields.map((f, i) => (
            <CanvasRow key={f.key} field={f} selected={i === selected} onSelect={() => onSelect(i)} />
          ))}
        </ul>
      </SortableContext>
    </DndContext>
  )
}

function CanvasRow({ field, selected, onSelect }: { field: FormField; selected: boolean; onSelect: () => void }) {
  const { t } = useTranslation()
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: field.key })
  const Icon = FIELD_ICONS[field.type]
  const problems = fieldProblems(field)
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={cn(
        'flex items-center gap-1 rounded-lg border bg-card text-card-foreground',
        selected ? 'border-primary ring-1 ring-primary' : 'border-border',
        isDragging && 'relative z-10 shadow-md',
      )}
    >
      <button
        type="button"
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        aria-label={t('plugin_formbuilder.designer.drag', { name: field.title || field.key })}
        title={t('plugin_formbuilder.designer.drag', { name: field.title || field.key })}
        className="flex size-10 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
      >
        <GripVertical className="size-4" aria-hidden="true" />
      </button>
      <button
        type="button"
        onClick={onSelect}
        aria-pressed={selected}
        className="flex min-h-10 min-w-0 flex-1 items-center gap-2 rounded-md py-2 pe-3 text-start focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Icon className="size-4 shrink-0 text-primary" aria-hidden="true" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">
            {field.title || <span className="text-muted-foreground">{t('plugin_formbuilder.designer.untitled')}</span>}
            {field.required ? <span className="ms-1 text-destructive" aria-hidden="true">*</span> : null}
          </span>
          <span className="block truncate text-xs text-muted-foreground">
            {t(`plugin_formbuilder.types.${field.type}`)} · <code className="font-mono">{field.key}</code>
          </span>
        </span>
        {field.showIf ? <Eye className="size-4 shrink-0 text-muted-foreground" aria-label={t('plugin_formbuilder.designer.hasCond')} /> : null}
        {problems.length > 0 ? (
          <AlertTriangle className="size-4 shrink-0 text-warning" aria-label={problems.map((p) => t(`plugin_formbuilder.problem.${p}`)).join('; ')} />
        ) : null}
      </button>
    </li>
  )
}
