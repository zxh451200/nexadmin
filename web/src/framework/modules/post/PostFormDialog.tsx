import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { useCreatePost, useUpdatePost, type SysPost } from './api'

export interface PostFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: SysPost | null
  /** 新增时用于计算默认排序 */
  count: number
}

interface FormValues {
  name: string
  code: string
  deptId: string
  status: '1' | '0'
  remark: string
}

export function PostFormDialog({ open, onOpenChange, editing, count }: PostFormDialogProps) {
  const { t } = useTranslation()
  const create = useCreatePost()
  const update = useUpdatePost()

  const schema = useMemo(
    () =>
      z.object({
        name: z.string().trim().min(1, t('post.validation.nameRequired')).max(50),
        code: z
          .string()
          .trim()
          .min(1, t('post.validation.codeRequired'))
          .regex(/^[A-Za-z0-9_-]+$/, t('post.validation.codeInvalid')),
        deptId: z.string(),
        status: z.enum(['1', '0']),
        remark: z.string().max(200),
      }),
    [t],
  )

  const form = useForm<FormValues>({
    resolver: zodResolver(schema) as never,
    defaultValues: { name: '', code: '', deptId: '', status: '1', remark: '' },
  })

  useEffect(() => {
    if (!open) return
    form.reset(
      editing
        ? {
            name: editing.name,
            code: editing.code,
            deptId: editing.deptId ?? '',
            status: editing.status,
            remark: editing.remark ?? '',
          }
        : { name: '', code: '', deptId: '', status: '1', remark: '' },
    )
  }, [open, editing, form])

  const pending = create.isPending || update.isPending

  const onSubmit = form.handleSubmit(async (v) => {
    const body = {
      name: v.name.trim(),
      code: v.code.trim(),
      deptId: v.deptId || undefined,
      sort: editing ? editing.sort : (count + 1) * 10,
      status: v.status,
      remark: v.remark.trim() || undefined,
    }
    try {
      if (editing) await update.mutateAsync({ id: editing.id, ...body })
      else await create.mutateAsync(body)
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  const formId = 'sys-post-form'

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={editing ? t('post.action.edit') : t('post.action.add')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={pending}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id={formId} onSubmit={onSubmit} showRequiredHint>
          <FormGrid cols={1}>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('post.field.name')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="code"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('post.field.code')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="deptId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('post.field.dept')}</FormLabel>
                  <FormControl>
                    <TreeSelect url="/depts/tree" value={field.value} onChange={(v) => field.onChange(v ?? '')} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormGridFull>
              <SwitchField control={form.control} name="status" label={t('post.field.status')} valueOn="1" valueOff="0" />
            </FormGridFull>
            <FormGridFull>
              <FormField
                control={form.control}
                name="remark"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('post.field.remark')}</FormLabel>
                    <FormControl>
                      <Textarea rows={3} {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
