import { useCallback, useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { usePermission } from '@/framework/auth/usePermission'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { DictSelect } from '@/framework/components/DictSelect'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { IconPicker } from '@/framework/components/IconPicker'
import { SwitchField } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { useCreateMenu, useUpdateMenu, type MenuType, type SysMenu } from './api'

export interface MenuFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 编辑时传入；新增时为空 */
  editing?: SysMenu | null
  /** 新增下级时的默认上级 */
  defaultParentId?: string | null
  tree: SysMenu[]
}

interface FormValues {
  type: MenuType
  parentId: string
  title: string
  icon: string
  path: string
  component: string
  perm: string
  sort: number
  keepAlive: boolean
  hidden: boolean
  external: boolean
  status: '1' | '0'
}

function findNode(nodes: SysMenu[], id: string): SysMenu | undefined {
  for (const n of nodes) {
    if (n.id === id) return n
    const hit = n.children ? findNode(n.children, id) : undefined
    if (hit) return hit
  }
  return undefined
}

function collectIds(node: SysMenu, out: Set<string> = new Set()) {
  out.add(node.id)
  node.children?.forEach((c) => collectIds(c, out))
  return out
}

export function MenuFormDialog({ open, onOpenChange, editing, defaultParentId, tree }: MenuFormDialogProps) {
  const { t } = useTranslation()
  // 这一页写的都是没有 tenant_id 的全局行：非平台调用方只读（后端 core.RequirePlatform() 挡）。
  const { isPlatform } = usePermission()
  const create = useCreateMenu()
  const update = useUpdateMenu()
  const tr = useCallback((s: string) => t(s, { defaultValue: s }), [t])

  const schema = useMemo(
    () =>
      z
        .object({
          type: z.enum(['dir', 'menu', 'button']),
          parentId: z.string(),
          title: z.string().trim().min(1, t('sysMenu.validation.titleRequired')),
          icon: z.string(),
          path: z.string(),
          component: z.string(),
          perm: z.string(),
          sort: z.coerce.number({ error: t('sysMenu.validation.sortInvalid') }).int(t('sysMenu.validation.sortInvalid')),
          keepAlive: z.boolean(),
          hidden: z.boolean(),
          external: z.boolean(),
          status: z.enum(['1', '0']),
        })
        .superRefine((v, ctx) => {
          if (v.type === 'menu' && !v.path.trim()) {
            ctx.addIssue({ code: 'custom', path: ['path'], message: t('sysMenu.validation.pathRequired') })
          }
          if (v.type === 'button' && !v.perm.trim()) {
            ctx.addIssue({ code: 'custom', path: ['perm'], message: t('sysMenu.validation.permRequired') })
          }
        }),
    [t],
  )

  const form = useForm<FormValues>({
    resolver: zodResolver(schema) as never,
    defaultValues: {
      type: 'dir',
      parentId: '',
      title: '',
      icon: '',
      path: '',
      component: '',
      perm: '',
      sort: 10,
      keepAlive: false,
      hidden: false,
      external: false,
      status: '1',
    },
  })

  useEffect(() => {
    if (!open) return
    if (editing) {
      form.reset({
        type: editing.type,
        parentId: editing.parentId ?? '',
        title: editing.title,
        icon: editing.icon ?? '',
        path: editing.path ?? '',
        component: editing.component ?? '',
        perm: editing.perm ?? '',
        sort: editing.sort,
        keepAlive: editing.keepAlive,
        hidden: editing.hidden,
        external: editing.external,
        status: editing.status,
      })
    } else {
      const parent = defaultParentId ? findNode(tree, defaultParentId) : undefined
      form.reset({
        type: !parent ? 'dir' : parent.type === 'dir' ? 'menu' : 'button',
        parentId: defaultParentId ?? '',
        title: '',
        icon: '',
        path: '',
        component: '',
        perm: '',
        sort: ((parent?.children?.length ?? tree.length) + 1) * 10,
        keepAlive: false,
        hidden: false,
        external: false,
        status: '1',
      })
    }
  }, [open, editing, defaultParentId, tree, form])

  const type = form.watch('type')

  // 上级候选：去掉按钮；编辑时排除自己及下级。
  // 「不能选的不显示」：非法父级直接不上树（而不是灰显）——目录/菜单的父级只能是目录
  // （菜单是叶子，只挂按钮），按钮的父级只能是菜单；非法节点若有可选子级，子级上提到当前层。
  const parentData = useMemo(() => {
    const blocked = editing ? collectIds(editing) : new Set<string>()
    const strip = (nodes: SysMenu[]): Record<string, unknown>[] =>
      nodes
        .filter((n) => n.type !== 'button')
        .map((n) => ({ id: n.id, title: tr(n.title), type: n.type, children: strip(n.children ?? []) }))
    const validKind = (n: Record<string, unknown>) => (type === 'button' ? n.type === 'menu' : n.type === 'dir')
    const build = (nodes: Record<string, unknown>[], out: Record<string, unknown>[]) => {
      for (const n of nodes) {
        if (blocked.has(n.id as string)) continue // 编辑时自己与后代整体不可选：子树剔除
        const kids = (n.children as Record<string, unknown>[]) ?? []
        if (validKind(n)) {
          const copy = { ...n, children: [] as Record<string, unknown>[] }
          build(kids, copy.children as Record<string, unknown>[])
          out.push(copy)
        } else {
          build(kids, out) // 类型不合法：节点不上树，可选子级上提
        }
      }
    }
    const out: Record<string, unknown>[] = []
    build(strip(tree), out)
    return out
  }, [tree, editing, type, tr])

  // 类型切换后，原上级可能不再合法（如目录挂在了菜单下）：回落顶级让用户重选，
  // 不然提交的会是列表里已经不存在的选项。
  useEffect(() => {
    const pid = form.getValues('parentId')
    if (!pid) return
    const node = findNode(tree, pid)
    const ok = !node || (type === 'button' ? node.type === 'menu' : node.type === 'dir')
    if (!ok) form.setValue('parentId', '')
  }, [type, tree, form])

  const pending = create.isPending || update.isPending

  const onSubmit = form.handleSubmit(async (v) => {
    const body = {
      type: v.type,
      parentId: v.parentId || null,
      title: v.title.trim(),
      icon: v.type === 'button' ? undefined : v.icon || undefined,
      path: v.type === 'button' ? undefined : v.path.trim() || undefined,
      component: v.type === 'menu' && !v.external ? v.component.trim() || undefined : undefined,
      perm: v.type === 'dir' ? undefined : v.perm.trim() || undefined,
      sort: v.sort,
      keepAlive: v.type === 'menu' ? v.keepAlive : false,
      hidden: v.type === 'button' ? false : v.hidden,
      external: v.type === 'menu' ? v.external : false,
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

  const formId = 'sys-menu-form'

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={editing ? t('sysMenu.action.edit') : t('sysMenu.action.add')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={!isPlatform || pending}>
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
              name="type"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('sysMenu.field.type')}</FormLabel>
                  <FormControl>
                    <DictSelect dict="menu_type" value={field.value} onChange={(v) => field.onChange(v || 'dir')} allowClear={false} />
                  </FormControl>
                  <FormDescription>{t('sysMenu.field.typeHint')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="parentId"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('sysMenu.field.parent')}</FormLabel>
                  <FormControl>
                    <TreeSelect
                      data={parentData}
                      labelKey="title"
                      value={field.value}
                      onChange={(v) => field.onChange(v ?? '')}
                      placeholder={t('sysMenu.field.parentPlaceholder')}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormGridFull>
              <FormField
                control={form.control}
                name="title"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysMenu.field.title')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormDescription>{t('sysMenu.field.titleHint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </FormGridFull>
            {type !== 'button' && (
              <FormField
                control={form.control}
                name="icon"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysMenu.field.icon')}</FormLabel>
                    <FormControl>
                      <IconPicker value={field.value} onChange={field.onChange} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {type !== 'button' && (
              <FormField
                control={form.control}
                name="path"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysMenu.field.path')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {type === 'menu' && !form.watch('external') && (
              <FormField
                control={form.control}
                name="component"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysMenu.field.component')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            {type !== 'dir' && (
              <FormField
                control={form.control}
                name="perm"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysMenu.field.perm')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormDescription>{t('sysMenu.field.permHint')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            <FormField
              control={form.control}
              name="sort"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('sysMenu.field.sort')}</FormLabel>
                  <FormControl>
                    <Input type="number" inputMode="numeric" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {type !== 'button' && (
              <FormGridFull>
                <div className="divide-y divide-border rounded-lg border border-border">
                  {type === 'menu' && (
                    <SwitchField control={form.control} name="keepAlive" label={t('sysMenu.field.keepAlive')} bare className="p-3" />
                  )}
                  <SwitchField control={form.control} name="hidden" label={t('sysMenu.field.hidden')} bare className="p-3" />
                  {type === 'menu' && (
                    <SwitchField control={form.control} name="external" label={t('sysMenu.field.external')} bare className="p-3" />
                  )}
                </div>
              </FormGridFull>
            )}
            <FormGridFull>
              <SwitchField control={form.control} name="status" label={t('sysMenu.field.status')} valueOn="1" valueOff="0" />
            </FormGridFull>
          </FormGrid>
        </FormLayout>
      </Form>
    </ResponsiveDialog>
  )
}
