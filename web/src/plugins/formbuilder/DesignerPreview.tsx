import { useState } from 'react'
import { RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import type { FormDefinition } from './api'
import { FormRenderer } from './FormRenderer'
import { initialValues, validateValues } from './schema'

/** 实时预览：直接用当前（未保存的）定义渲染，与填写页是同一个 FormRenderer；可以填写并做客户端校验，但不会提交。 */
export function DesignerPreview({ def }: { def: FormDefinition }) {
  const { t } = useTranslation()
  const [values, setValues] = useState<Record<string, unknown>>(() => initialValues(def))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [checked, setChecked] = useState(false)

  const set = (key: string, v: unknown) => {
    const next = { ...values, [key]: v }
    setValues(next)
    // 已经点过"校验"后，边填边刷新错误
    if (checked) setErrors(validateValues(def, next, t))
  }
  const check = () => {
    const errs = validateValues(def, values, t)
    setErrors(errs)
    setChecked(true)
  }

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-5 rounded-lg border border-border bg-card p-4 md:p-6">
      <header className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold">{def.name || t('plugin_formbuilder.designer.untitled')}</h2>
        {def.description ? <p className="text-sm text-muted-foreground">{def.description}</p> : null}
      </header>
      {def.fields.length === 0 ? (
        <p role="status" className="py-6 text-center text-sm text-muted-foreground">
          {t('plugin_formbuilder.designer.emptyCanvas')}
        </p>
      ) : (
        <FormRenderer def={def} values={values} onChange={set} errors={errors} preview idPrefix="fb-pv" />
      )}
      {checked && Object.keys(errors).length === 0 ? (
        <p role="status" className="text-sm text-success">
          {t('plugin_formbuilder.designer.previewOk')}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2">
        <Button type="button" onClick={check}>
          {t('plugin_formbuilder.designer.previewCheck')}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setValues(initialValues(def))
            setErrors({})
            setChecked(false)
          }}
        >
          <RotateCcw aria-hidden="true" />
          {t('plugin_formbuilder.designer.previewReset')}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">{t('plugin_formbuilder.designer.previewHint')}</p>
    </div>
  )
}
