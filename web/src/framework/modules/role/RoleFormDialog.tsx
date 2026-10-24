import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { DictSelect } from '@/framework/components/DictSelect'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import { useCreateRole, useUpdateRole, type Role, type RoleInput } from './api'

const FORM_ID = 'role-form'

export interface RoleFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 为空表示新建 */
  role: Role | null
  /** 新建时默认排序 */
  nextSort: number
}

export function RoleFormDialog({ open, onOpenChange, role, nextSort }: RoleFormDialogProps) {
  const { t } = useTranslation()
  const create = useCreateRole()
  const update = useUpdateRole()
  const isEdit = !!role

  const schema = useMemo(
    () =>
      z
        .object({
          name: z.string().trim().min(1, t('role.v.nameRequired')).max(32),
          code: z
            .string()
            .trim()
            .min(1, t('role.v.codeRequired'))
            .max(32)
            .regex(/^[A-Za-z][A-Za-z0-9_]*$/, t('role.v.codeFormat')),
          sort: z.number({ invalid_type_error: t('role.v.sortNumber') }).int(t('role.v.sortNumber')),
          enabled: z.boolean(),
          assignable: z.boolean(),
          dataScope: z.string().min(1),
          deptIds: z.array(z.string()),
          remark: z.string().max(200),
        })
        .refine((v) => v.dataScope !== 'custom' || v.deptIds.length > 0, {
          path: ['deptIds'],
          message: t('role.v.deptRequired'),
        }),
    [t],
  )
  type FormValues = z.infer<typeof schema>

  const toValues = (r: Role | null): FormValues => ({
    name: r?.name ?? '',
    code: r?.code ?? '',
    sort: r?.sort ?? nextSort,
    enabled: (r?.status ?? '1') === '1',
    assignable: r?.builtin ? false : (r?.assignable ?? true),
    dataScope: r?.dataScope ?? 'self',
    deptIds: r?.deptIds ?? [],
    remark: r?.remark ?? '',
  })

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: toValues(role) })
  useEffect(() => {
    if (open) form.reset(toValues(role))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, role])

  const dataScope = form.watch('dataScope')
  const pending = create.isPending || update.isPending

  const onSubmit = form.handleSubmit(async (v) => {
    const input: RoleInput = {
      name: v.name,
      code: v.code,
      sort: v.sort,
      status: v.enabled ? '1' : '0',
      assignable: role?.builtin ? false : v.assignable,
      dataScope: v.dataScope,
      deptIds: v.dataScope === 'custom' ? v.deptIds : [],
      remark: v.remark,
    }
    try {
      if (role) await update.mutateAsync({ id: role.id, input })
      else await create.mutateAsync(input)
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  })

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={isEdit ? t('role.form.editTitle') : t('role.form.createTitle')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={pending}>
            {pending ? t('common.loading') : t('common.save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <FormLayout id={FORM_ID} onSubmit={onSubmit} showRequiredHint>
          <FormGrid>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('role.form.name')}</FormLabel>
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
                  <FormLabel>{t('role.form.code')}</FormLabel>
                  <FormControl>
                    <Input {...field} disabled={role?.builtin} autoComplete="off" />
                  </FormControl>
                  <FormDescription>{t('role.form.codeHint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="sort"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('role.form.sort')}</FormLabel>
                  <FormControl>
                    <Input
                      type="number"
                      inputMode="numeric"
                      value={Number.isNaN(field.value) ? '' : field.value}
                      onChange={(e) => field.onChange(e.target.value === '' ? NaN : Number(e.target.value))}
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="enabled"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('role.form.status')}</FormLabel>
                  <div className="flex min-h-11 items-center md:min-h-9">
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} disabled={role?.builtin} />
                    </FormControl>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="assignable"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('role.form.assignable')}</FormLabel>
                  <div className="flex min-h-11 items-center md:min-h-9">
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} disabled={role?.builtin} aria-describedby="role-assignable-hint" />
                    </FormControl>
                  </div>
                  <p id="role-assignable-hint" className="text-xs text-muted-foreground">
                    {role?.builtin ? t('role.form.assignableBuiltin') : t('role.form.assignableHint')}
                  </p>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormGridFull>
              <FormField
                control={form.control}
                name="dataScope"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('role.form.dataScope')}</FormLabel>
                    <FormControl>
                      <DictSelect dict="data_scope" value={field.value} onChange={field.onChange} allowClear={false} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            {dataScope === 'custom' ? (
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="deptIds"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('role.form.deptIds')}</FormLabel>
                      <FormControl>
                        <TreeSelect url="/depts/tree" multiple value={field.value} onChange={field.onChange} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGridFull>
            ) : null}
            <FormGridFull>
              <FormField
                control={form.control}
                name="remark"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('role.form.remark')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={3} />
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
