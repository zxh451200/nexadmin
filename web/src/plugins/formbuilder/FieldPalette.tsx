import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { FieldType } from './api'
import { FIELD_ICONS } from './FieldCanvas'
import { FIELD_TYPES } from './schema'

interface Props {
  onAdd: (type: FieldType) => void
  disabled?: boolean
}

/** 字段类型面板：点击把该类型的字段加到当前选中字段之后（没有选中则加到末尾） */
export function FieldPalette({ onAdd, disabled }: Props) {
  const { t } = useTranslation()
  return (
    <section aria-labelledby="fb-palette" className="flex flex-col gap-2">
      <h2 id="fb-palette" className="text-sm font-semibold">
        {t('plugin_formbuilder.designer.palette')}
      </h2>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-1">
        {FIELD_TYPES.map((type) => {
          const Icon = FIELD_ICONS[type]
          return (
            <Button key={type} type="button" variant="outline" className="justify-start gap-2" disabled={disabled} onClick={() => onAdd(type)}>
              <Icon className="size-4 shrink-0 text-primary" aria-hidden="true" />
              <span className="truncate">{t(`plugin_formbuilder.types.${type}`)}</span>
            </Button>
          )
        })}
      </div>
    </section>
  )
}
