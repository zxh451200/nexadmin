import { useEffect, useRef, useState } from 'react'
import { AlertCircle, ArrowLeft, CheckCircle2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ApiError } from '@/framework/request/http'
import { Alert, AlertDescription, AlertTitle } from '@/framework/ui/alert'
import { cardSurface } from '@/framework/ui/card'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useFillDetail, useSubmit, type SubmitResult } from './api'
import { FormRenderer } from './FormRenderer'
import { fieldErrorsFromMessage, initialValues, newIdemKey, validateValues, visibleValues } from './schema'

/** 填写页：按后端返回的「已发布版本」渲染；提交前做客户端校验，服务端的校验错误按字段回显。 */
export function FillView({ id }: { id: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { data, isLoading, isError } = useFillDetail(id)
  const submit = useSubmit()
  const [values, setValues] = useState<Record<string, unknown>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState('')
  const [done, setDone] = useState<SubmitResult | null>(null)
  const idemKey = useRef(newIdemKey())
  const formRef = useRef<HTMLFormElement>(null)
  const loadedFor = useRef<string>()

  useEffect(() => {
    if (data && loadedFor.current !== `${data.id}:${data.publishedVersion}`) {
      loadedFor.current = `${data.id}:${data.publishedVersion}`
      setValues(initialValues(data.definition))
    }
  }, [data])

  const back = () => navigate('/system/formbuilder/fill')
  const reset = () => {
    if (!data) return
    setValues(initialValues(data.definition))
    setErrors({})
    setFormError('')
    setDone(null)
    idemKey.current = newIdemKey()
  }
  const focusFirst = (errs: Record<string, string>) => {
    const first = data?.definition.fields.find((f) => errs[f.key])
    if (!first) return
    requestAnimationFrame(() => {
      const el = formRef.current?.querySelector<HTMLElement>(`[data-field="${first.key}"]`)
      el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el?.querySelector<HTMLElement>('input,textarea,button,[role="combobox"]')?.focus({ preventScroll: true })
    })
  }

  if (isLoading) {
    return (
      <PageContainer>
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-80 w-full" />
      </PageContainer>
    )
  }
  if (isError || !data) {
    return (
      <PageContainer>
        <EmptyState
          title={t('plugin_formbuilder.fillNotFound')}
          action={
            <Button variant="outline" onClick={back}>
              <ArrowLeft aria-hidden="true" />
              {t('common.back')}
            </Button>
          }
        />
      </PageContainer>
    )
  }

  const def = data.definition
  const closed = data.closed !== ''

  const onSubmit = async () => {
    setFormError('')
    const local = validateValues(def, values, t)
    setErrors(local)
    if (Object.keys(local).length > 0) return focusFirst(local)
    try {
      // 只提交可见字段的非空值；隐藏字段的值服务端也会丢弃
      const res = await submit.mutateAsync({ id, data: visibleValues(def, values).clean, idemKey: idemKey.current })
      setDone(res)
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : t('common.operateFailed')
      const parsed = fieldErrorsFromMessage(def, msg)
      setErrors(parsed.fields)
      setFormError(parsed.rest || (Object.keys(parsed.fields).length === 0 ? msg : ''))
      focusFirst(parsed.fields)
    }
  }

  if (done) {
    return (
      <PageContainer>
        <Card className="mx-auto flex w-full max-w-2xl flex-col items-center gap-4 px-4 py-12 text-center">
          <CheckCircle2 className="size-10 text-success" aria-hidden="true" />
          <h1 className="text-lg font-semibold" role="status">
            {t('plugin_formbuilder.fill.submitted')}
          </h1>
          <p className="max-w-md whitespace-pre-wrap break-words text-sm text-muted-foreground">{done.message || t('plugin_formbuilder.fill.successDefault')}</p>
          <div className="flex flex-wrap justify-center gap-2">
            <Button onClick={back}>{t('plugin_formbuilder.fill.backToList')}</Button>
            {data.allowRepeat && !closed ? (
              <Button variant="outline" onClick={reset}>
                {t('plugin_formbuilder.fill.again')}
              </Button>
            ) : null}
          </div>
        </Card>
      </PageContainer>
    )
  }

  return (
    <PageContainer>
      <PageHeader
        title={def.name}
        description={def.description}
        actions={
          <Button variant="outline" onClick={back}>
            <ArrowLeft aria-hidden="true" />
            {t('common.back')}
          </Button>
        }
      />
      <form
        ref={formRef}
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          void onSubmit()
        }}
        className={cn(cardSurface, 'mx-auto flex w-full max-w-2xl flex-col gap-5 p-4 md:p-6 [&_input]:text-base [&_textarea]:text-base md:[&_input]:text-sm md:[&_textarea]:text-sm')}
      >
        {closed ? (
          <Alert variant="warning">
            <AlertCircle aria-hidden="true" />
            <AlertTitle>{t(`plugin_formbuilder.closed.${data.closed}`)}</AlertTitle>
            <AlertDescription>{t(`plugin_formbuilder.closedHint.${data.closed}`)}</AlertDescription>
          </Alert>
        ) : null}
        {formError ? (
          <Alert variant="destructive">
            <AlertCircle aria-hidden="true" />
            <AlertTitle>{t('plugin_formbuilder.fill.failed')}</AlertTitle>
            <AlertDescription>{formError}</AlertDescription>
          </Alert>
        ) : null}
        <FormRenderer
          def={def}
          values={values}
          disabled={closed || submit.isPending}
          errors={errors}
          idPrefix="fb-fill"
          onChange={(key, v) => {
            const next = { ...values, [key]: v }
            setValues(next)
            // 改过的字段先清掉它自己的错误；其余错误在下次提交时重新计算
            if (errors[key]) setErrors((prev) => Object.fromEntries(Object.entries(prev).filter(([k]) => k !== key)))
          }}
        />
        {closed ? null : (
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={submit.isPending}>
              {submit.isPending ? t('common.loading') : t('common.submit')}
            </Button>
          </div>
        )}
      </form>
    </PageContainer>
  )
}
