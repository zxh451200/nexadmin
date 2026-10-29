import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { TreeSelect } from '@/framework/components/TreeSelect'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import {
  useDeleteTenantPackages,
  useMenuOptions,
  useSaveTenantPackage,
  useTenantPackages,
  type TenantPackageRow,
} from './api'

const nonNeg = z.number().int().min(0)
const schema = z.object({
  name: z.string().trim().min(1).max(50),
  menuIds: z.array(z.string()),
  maxUsers: nonNeg,
  maxRoles: nonNeg,
  maxStorageMb: nonNeg,
  aiDailyQuota: nonNeg,
  status: z.string().min(1),
  remark: z.string().max(200).optional(),
})
type FormValues = z.infer<typeof schema>
const empty: FormValues = { name: '', menuIds: [], maxUsers: 0, maxRoles: 0, maxStorageMb: 0, aiDailyQuota: 0, status: '1', remark: '' }

function formatMb(mb: number, unlimited: string) {
  if (!mb) return unlimited
  return mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`
}

export default function TenantPackagePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [draft, setDraft] = useState({ keyword: '', status: '' })
  const [filters, setFilters] = useState({ keyword: '', status: '' })
  const [selected, setSelected] = useState<string[]>([])
  const [editing, setEditing] = useState<TenantPackageRow | null>(null)
  const [open, setOpen] = useState(false)

  const { data, isLoading } = useTenantPackages({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    status: filters.status || undefined,
  })
  const menus = useMenuOptions()
  const save = useSaveTenantPackage()
  const del = useDeleteTenantPackages()

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  useEffect(() => {
    if (!open) return
    form.reset(
      editing
        ? {
            name: editing.name,
            menuIds: editing.menuIds,
            maxUsers: editing.maxUsers,
            maxRoles: editing.maxRoles ?? 0,
            maxStorageMb: editing.maxStorageMb,
            aiDailyQuota: editing.aiDailyQuota ?? 0,
            status: editing.status,
            remark: editing.remark ?? '',
          }
        : empty,
    )
  }, [open, editing, form])

  const fail = (e: unknown) => toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
  const openForm = (row: TenantPackageRow | null) => {
    setEditing(row)
    setOpen(true)
  }

  const submit = form.handleSubmit(async (values) => {
    try {
      await save.mutateAsync({ id: editing?.id, body: values })
      toast.success(t('common.operateSuccess'))
      setOpen(false)
    } catch (e) {
      fail(e)
    }
  })

  const remove = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('tenantPkg.deleteConfirm', { count: ids.length }) }))) return
    try {
      await del.mutateAsync(ids)
      toast.success(t('common.operateSuccess'))
      setSelected((s) => s.filter((id) => !ids.includes(id)))
    } catch (e) {
      fail(e)
    }
  }

  const unlimited = t('tenantPkg.unlimited')
  const columns: Column<TenantPackageRow>[] = [
    { key: 'name', title: t('tenantPkg.name'), mobile: 'title' },
    {
      key: 'status',
      title: t('common.status'),
      mobile: 'sub',
      render: (r) => <DictTag dict="sys_status" value={r.status} />,
    },
    {
      key: 'menuIds',
      title: t('tenantPkg.menus'),
      render: (r) => <span className="num">{t('tenantPkg.menuCount', { count: r.menuIds.length })}</span>,
    },
    {
      key: 'maxUsers',
      title: t('tenantPkg.maxUsers'),
      align: 'end',
      render: (r) => <span className="num">{r.maxUsers || unlimited}</span>,
    },
    {
      key: 'maxRoles',
      title: t('tenantPkg.maxRoles'),
      align: 'end',
      mobile: 'hidden',
      render: (r) => <span className="num">{r.maxRoles || unlimited}</span>,
    },
    {
      key: 'maxStorageMb',
      title: t('tenantPkg.maxStorage'),
      align: 'end',
      render: (r) => <span className="num">{formatMb(r.maxStorageMb, unlimited)}</span>,
    },
    {
      key: 'aiDailyQuota',
      title: t('tenantPkg.aiDailyQuota'),
      align: 'end',
      mobile: 'hidden',
      render: (r) => <span className="num">{r.aiDailyQuota || unlimited}</span>,
    },
    { key: 'remark', title: t('common.remark'), mobile: 'hidden' },
    {
      key: 'createdAt',
      title: t('common.createdAt'),
      mobile: 'hidden',
      render: (r) => <span className="num">{new Date(r.createdAt).toLocaleDateString(i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('tenantPkg.title')} description={t('tenantPkg.description')} />

      <FilterBar
        onSearch={() => {
          setPage(1)
          setFilters(draft)
        }}
        onReset={() => {
          const init = { keyword: '', status: '' }
          setDraft(init)
          setFilters(init)
          setPage(1)
        }}
      >
        <Input
          className="md:w-64"
          value={draft.keyword}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('tenantPkg.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <DictSelect
          className="md:w-40"
          dict="sys_status"
          value={draft.status}
          onChange={(v) => setDraft((d) => ({ ...d, status: v }))}
          placeholder={t('common.status')}
        />
      </FilterBar>

      <DataTable<TenantPackageRow>
        toolbar={
          <Can perm="tenantPkg:create">
            <Button type="button" onClick={() => openForm(null)}>
              <Plus className="me-2 h-4 w-4" aria-hidden="true" />
              {t('common.add')}
            </Button>
          </Can>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="tenantPkg"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="tenantPkg:delete">
            <Button type="button" variant="destructive" size="sm" onClick={() => remove(ids)}>
              <Trash2 className="me-1 h-4 w-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <>
            <Can perm="tenantPkg:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(row)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="tenantPkg:delete">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => remove([row.id])}>
                <Trash2 className="h-4 w-4 text-destructive" aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />

      <ResponsiveDialog
        open={open}
        onOpenChange={setOpen}
        size="lg"
        title={editing ? t('tenantPkg.edit') : t('tenantPkg.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form="tenant-pkg-form" disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Form {...form}>
          <FormLayout id="tenant-pkg-form" onSubmit={submit}>
            <FormGrid>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('tenantPkg.name')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="status"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('common.status')}</FormLabel>
                    <FormControl>
                      <DictSelect dict="sys_status" value={field.value} onChange={field.onChange} allowClear={false} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="maxUsers"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('tenantPkg.maxUsers')}</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        value={Number.isNaN(field.value) ? '' : field.value}
                        onChange={(e) => field.onChange(e.target.valueAsNumber)}
                      />
                    </FormControl>
                    <FormDescription>{t('tenantPkg.zeroUnlimited')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="maxRoles"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('tenantPkg.maxRoles')}</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        value={Number.isNaN(field.value) ? '' : field.value}
                        onChange={(e) => field.onChange(e.target.valueAsNumber)}
                      />
                    </FormControl>
                    <FormDescription>{t('tenantPkg.zeroUnlimited')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="maxStorageMb"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('tenantPkg.maxStorageMb')}</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        value={Number.isNaN(field.value) ? '' : field.value}
                        onChange={(e) => field.onChange(e.target.valueAsNumber)}
                      />
                    </FormControl>
                    <FormDescription>{t('tenantPkg.zeroUnlimited')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="aiDailyQuota"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('tenantPkg.aiDailyQuota')}</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        min={0}
                        value={Number.isNaN(field.value) ? '' : field.value}
                        onChange={(e) => field.onChange(e.target.valueAsNumber)}
                      />
                    </FormControl>
                    <FormDescription>{t('tenantPkg.zeroUnlimited')}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="menuIds"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('tenantPkg.menus')}</FormLabel>
                      <FormControl>
                        <TreeSelect
                          multiple
                          data={menus.data ?? []}
                          labelKey="label"
                          value={field.value}
                          onChange={field.onChange}
                          placeholder={t('common.pleaseSelect')}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGridFull>
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="remark"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('common.remark')}</FormLabel>
                      <FormControl>
                        <Textarea rows={3} {...field} value={field.value ?? ''} />
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
    </PageContainer>
  )
}
