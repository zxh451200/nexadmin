import { useEffect, type ReactNode } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2, PlugZap } from 'lucide-react'
import { useForm, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { ZodTypeAny } from 'zod'
import { Can } from '@/framework/auth/Can'
import { FormLayout } from '@/framework/components/FormLayout'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/framework/ui/card'
import { Form } from '@/framework/ui/form'
import { useSaveSection, useTestSection, type AllSettings, type SectionKey } from './api'

interface Props<K extends SectionKey> {
  section: K
  /** 服务端当前值 */
  value: AllSettings[K]
  schema: ZodTypeAny
  /** 是否带「测试连接」按钮 */
  testable?: boolean
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  children: (form: UseFormReturn<any>) => ReactNode
}

/** 单个配置块：Card + 表单 + 保存/测试连接 */
export function SectionCard<K extends SectionKey>({ section, value, schema, testable, children }: Props<K>) {
  const { t } = useTranslation()
  const save = useSaveSection()
  const test = useTestSection()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const form = useForm<any>({
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    resolver: zodResolver(schema) as any,
    defaultValues: value,
  })
  useEffect(() => {
    form.reset(value)
  }, [value, form])

  const fail = (e: unknown) => toast.error(e instanceof Error ? e.message : t('common.operateFailed'))

  const submit = form.handleSubmit(async (body) => {
    try {
      await save.mutateAsync({ section, body })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      fail(e)
    }
  })

  const runTest = async () => {
    try {
      const r = await test.mutateAsync({ section, body: form.getValues() })
      if (r.ok) toast.success(t('settings.test.ok'))
      else toast.error(t('settings.test.failed', { reason: t(`settings.test.reason.${r.reason ?? 'unknown'}`) }))
    } catch (e) {
      fail(e)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t(`settings.${section}.title`)}</CardTitle>
        <CardDescription>{t(`settings.${section}.description`)}</CardDescription>
      </CardHeader>
      <Form {...form}>
        <FormLayout id={`settings-${section}`} onSubmit={submit}>
          <CardContent className="flex flex-col gap-4">{children(form)}</CardContent>
          <CardFooter className="flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:justify-end">
            <Can perm="settings:update">
              {testable ? (
                <Button type="button" variant="outline" className="w-full sm:w-auto" disabled={test.isPending} onClick={runTest}>
                  {test.isPending ? (
                    <Loader2 className="me-2 h-4 w-4 animate-spin" aria-hidden="true" />
                  ) : (
                    <PlugZap className="me-2 h-4 w-4" aria-hidden="true" />
                  )}
                  {t('settings.testConnection')}
                </Button>
              ) : null}
              <Button type="submit" className="w-full sm:w-auto" disabled={save.isPending}>
                {t('common.save')}
              </Button>
            </Can>
          </CardFooter>
        </FormLayout>
      </Form>
    </Card>
  )
}
