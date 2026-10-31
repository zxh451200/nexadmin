import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { useCreateDept, useUpdateDept, type SysDept } from './api'
import { resolveUserLabel } from '../user/api'

export interface DeptFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing?: SysDept | null
  defaultParentId?: string | null
  tree: SysDept[]
}

interface FormValues {
  parentId: string
  name: string
  leaderId: string
  phone: string
  email: string
  sort: number
  status: '1' | '0'
}

function findNode(nodes: SysDept[], id: string): SysDept | undefined {
  for (const n of nodes) {
    if (n.id === id) return n
    const hit = n.children ? findNode(n.children, id) : undefined
    if (hit) return hit
  }
  return undefined
}

function collectIds(node: SysDept, out: Set<string> = new Set()) {
  out.add(node.id)
  node.children?.forEach((c) => collectIds(c, out))
  return out
}

export function DeptFormDialog({ open, onOpenChange, editing, defaultParentId, tree }: DeptFormDialogProps) {
  const { t } = useTranslation()
  const create = useCreateDept()
  const update = useUpdateDept()

  const schema = useMemo(
    () =>
      z.object({
        parentId: z.string(),
        name: z.string().trim().min(1, t('dept.validation.nameRequired')).max(50),
        leaderId: z.string(),
        phone: z.string().regex(/^[0-9+\-\s()]*$/, t('dept.validation.phoneInvalid')),
        email: z.string().refine((v) => !v || z.string().email().safeParse(v).success, t('dept.validation.emailInvalid')),
        sort: z.coerce.number({ invalid_type_error: t('dept.validation.sortInvalid') }).int(t('dept.validation.sortInvalid')),
        status: z.enum(['1', '0']),
      }),
    [t],
  )

  const form = useForm<FormValues>({
    resolver: zodResolver(schema) as never,
    defaultValues: { parentId: '', name: '', leaderId: '', phone: '', email: '', sort: 10, status: '1' },
  })

  useEffect(() => {
    if (!open) return
    if (editing) {
      form.reset({
        parentId: editing.parentId ?? '',
        name: editing.name,
        leaderId: editing.leaderId ?? '',
        phone: editing.phone ?? '',
        email: editing.email ?? '',
        sort: editing.sort,
        status: editing.status,
      })
    } else {
      const parent = defaultParentId ? findNode(tree, defaultParentId) : undefined
      form.reset({
        parentId: defaultParentId ?? '',
        name: '',
        leaderId: '',
        phone: '',
        email: '',
        sort: ((parent ? parent.children?.length ?? 0 : tree.length) + 1) * 10,
        status: '1',
      })
    }
  }, [open, editing, defaultParentId, tree, form])

  const blocked = useMemo(() => (editing ? collectIds(editing) : new Set<string>()), [editing])
  // 编辑态负责人回显：dept.leaderName 是写入时冗余的（负责人变更后会被清空），有值时直接传入
  // 避免等首页结果；缺名时由 resolveLabel 走 GET /users/:id 兜底，不再显示原始 id
  const leaderInitial = useMemo(
    () => (editing?.leaderId && editing.leaderName ? [{ value: editing.leaderId, label: editing.leaderName }] : undefined),
    [editing],
  )
  const pending = create.isPending || update.isPending

  const onSubmit = form.handleSubmit(async (v) => {
    const body = {
      parentId: v.parentId || null,
      name: v.name.trim(),
      leaderId: v.leaderId || undefined,
      phone: v.phone.trim() || undefined,
      email: v.email.trim() || undefined,
      sort: v.sort,
      status: v.status,
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

  const formId = 'sys-dept-form'

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('dept.action.edit') : t('dept.action.add')}
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
          <FormGrid>
            <FormField
              control={form.control}
              name="parentId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('dept.field.parent')}</FormLabel>
                  <FormControl>
                    <TreeSelect
                      data={tree as unknown as Record<string, unknown>[]}
                      value={field.value}
                      onChange={(v) => field.onChange(v ?? '')}
                      placeholder={t('dept.field.parentPlaceholder')}
                      isDisabled={(n) => blocked.has(n.id as string) || n.readonly === true}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('dept.field.name')}</FormLabel>
                  <FormControl>
                    <Input {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="leaderId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('dept.field.leader')}</FormLabel>
                  <FormControl>
                    <RemoteSelect
                      url="/users"
                      labelKey="nickname"
                      value={field.value}
                      onChange={(v) => field.onChange(v ?? '')}
                      initialOptions={leaderInitial}
                      resolveLabel={resolveUserLabel}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="phone"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('dept.field.phone')}</FormLabel>
                  <FormControl>
                    <Input type="tel" inputMode="tel" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="email"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('dept.field.email')}</FormLabel>
                  <FormControl>
                    <Input type="email" inputMode="email" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="sort"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('dept.field.sort')}</FormLabel>
                  <FormControl>
                    <Input type="number" inputMode="numeric" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormGridFull>
              <SwitchField control={form.control} name="status" label={t('dept.field.status')} valueOn="1" valueOff="0" />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
