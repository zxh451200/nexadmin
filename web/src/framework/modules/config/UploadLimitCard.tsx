import { useEffect } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { TriangleAlert } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/framework/ui/card'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { useSaveUploadSettings, useUploadSettings } from './api'

const size = z.string().trim().regex(/^\d*(\.\d+)?$/)
const schema = z.object({
  maxImageMb: size,
  maxVideoMb: size,
  maxFileMb: size,
  allowedTypes: z.array(z.string()),
})
type FormValues = z.infer<typeof schema>

const show = (n: number) => (n > 0 ? String(n) : '')

export function UploadLimitCard() {
  const { t } = useTranslation()
  const { data } = useUploadSettings()
  const save = useSaveUploadSettings()
  const form = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: { maxImageMb: '', maxVideoMb: '', maxFileMb: '', allowedTypes: [] },
  })

  useEffect(() => {
    if (data) {
      form.reset({
        maxImageMb: show(data.maxImageMb),
        maxVideoMb: show(data.maxVideoMb),
        maxFileMb: show(data.maxFileMb),
        allowedTypes: data.allowedTypes,
      })
    }
  }, [data, form])

  const [img, vid, file] = form.watch(['maxImageMb', 'maxVideoMb', 'maxFileMb'])
  const unlimited = [img, vid, file].every((v) => !Number(v))

  const submit = form.handleSubmit(async (v) => {
    try {
      await save.mutateAsync({
        maxImageMb: Number(v.maxImageMb) || 0,
        maxVideoMb: Number(v.maxVideoMb) || 0,
        maxFileMb: Number(v.maxFileMb) || 0,
        allowedTypes: v.allowedTypes,
      })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  const sizeField = (name: 'maxImageMb' | 'maxVideoMb' | 'maxFileMb', label: string) => (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input {...field} type="text" inputMode="decimal" placeholder={t('sysConfig.upload.unlimited')} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t('sysConfig.upload.title')}</CardTitle>
        <CardDescription>{t('sysConfig.upload.description')}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {unlimited ? (
          <Alert>
            <TriangleAlert className="h-4 w-4 text-warning" aria-hidden="true" />
            <AlertDescription>{t('sysConfig.upload.unlimitedWarning')}</AlertDescription>
          </Alert>
        ) : null}
        <Form {...form}>
          <FormLayout id="upload-limit-form" onSubmit={submit}>
            <FormGrid cols={3}>
              {sizeField('maxImageMb', t('sysConfig.upload.maxImage'))}
              {sizeField('maxVideoMb', t('sysConfig.upload.maxVideo'))}
              {sizeField('maxFileMb', t('sysConfig.upload.maxFile'))}
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="allowedTypes"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('sysConfig.upload.allowedTypes')}</FormLabel>
                      <FormControl>
                        <RemoteSelect
                          url="/configs/upload-types"
                          multiple
                          value={field.value}
                          onChange={field.onChange}
                          placeholder={t('common.pleaseSelect')}
                        />
                      </FormControl>
                      <FormDescription>{t('sysConfig.upload.allowedTypesHint')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGridFull>
            </FormGrid>
            <Can perm="config:update">
              <div>
                <Button type="submit" disabled={save.isPending}>
                  {t('common.save')}
                </Button>
              </div>
            </Can>
          </FormLayout>
        </Form>
      </CardContent>
    </Card>
  )
}
