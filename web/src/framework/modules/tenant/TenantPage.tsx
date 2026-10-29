import { useEffect, useMemo, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Ban, CalendarClock, CircleCheck, Pencil, Plus, Trash2 } from 'lucide-react'
import i18n from 'i18next'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DatePicker } from '@/framework/components/DatePicker'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { FormGrid, FormLayout, FormSection } from '@/framework/components/FormLayout'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RemoteSelect } from '@/framework/components/RemoteSelect'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Switch } from '@/framework/ui/switch'
import {
  useDeleteTenants,
  useRenewTenant,
  useSaveTenant,
  useSetTenantStatus,
  useTenants,
  type TenantRow,
} from './api'
import { ExpireBadge, TenantUsageCell } from './TenantUsage'

const quota = z.number().int().min(0).nullable()
const schema = z
  .object({
    isNew: z.boolean(),
    name: z.string().trim().min(1).max(60),
    domain: z
      .string()
      .trim()
      .min(1)
      .max(120)
      .regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/i),
    packageId: z.string().min(1),
    expireAt: z.string().min(1),
    status: z.string().min(1),
    maxUsers: quota,
    maxRoles: quota,
    maxStorageMb: quota,
    aiEnabled: z.boolean(),
    aiDailyQuota: quota,
    createAdmin: z.boolean(),
    adminUsername: z.string().trim().max(64),
    adminPassword: z.string().max(72).optional(),
    adminEmail: z.string().trim().max(100).optional(),
  })
  .superRefine((v, ctx) => {
    if (!v.isNew || !v.createAdmin) return
    const bad = (path: string, key: string) =>
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: [path], message: i18n.t(key) })
    if (!/^[A-Za-z0-9_.@-]{2,64}$/.test(v.adminUsername)) bad('adminUsername', 'tenant.err.username')
    if (!v.adminPassword) bad('adminPassword', 'tenant.err.password')
    if (v.adminEmail && !/^[^@\s]+@[^@\s]+$/.test(v.adminEmail)) bad('adminEmail', 'tenant.err.email')
  })
type FormValues = z.infer<typeof schema>

const dateOnly = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
const yearsFromNow = (base: Date, years: number) => {
  const d = new Date(base)
  d.setFullYear(d.getFullYear() + years)
  return dateOnly(d)
}

const empty = (): FormValues => ({
  isNew: true,
  name: '',
  domain: '',
  packageId: '',
  expireAt: yearsFromNow(new Date(), 1),
  status: 'active',
  maxUsers: null,
  maxRoles: null,
  maxStorageMb: null,
  aiEnabled: true,
  aiDailyQuota: null,
  createAdmin: true,
  adminUsername: '',
  adminPassword: '',
  adminEmail: '',
})

export default function TenantPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [draft, setDraft] = useState({ keyword: '', status: '' })
  const [filters, setFilters] = useState({ keyword: '', status: '' })
  const [selected, setSelected] = useState<string[]>([])
  const [editing, setEditing] = useState<TenantRow | null>(null)
  const [open, setOpen] = useState(false)
  const [renewing, setRenewing] = useState<TenantRow | null>(null)
  const [renewDate, setRenewDate] = useState('')

  const { data, isLoading } = useTenants({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    status: filters.status || undefined,
  })
  const save = useSaveTenant()
  const renew = useRenewTenant()
  const setStatus = useSetTenantStatus()
  const del = useDeleteTenants()

  const pkgs = useQuery({
    queryKey: ['tenant', 'pkg-options'],
    queryFn: () => api.get<{ id: string; name: string }[]>('/tenant-packages/options'),
    staleTime: 60 * 1000,
  })
  const pkgName = useMemo(() => new Map((pkgs.data ?? []).map((p) => [p.id, p.name])), [pkgs.data])

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty() })
  useEffect(() => {
    if (!open) return
    form.reset(
      editing
        ? {
            isNew: false,
            name: editing.name,
            domain: editing.domain,
            packageId: editing.packageId,
            expireAt: editing.expireAt,
            status: editing.status,
            maxUsers: editing.maxUsers ?? null,
            maxRoles: editing.maxRoles ?? null,
            maxStorageMb: editing.maxStorageMb ?? null,
            aiEnabled: editing.aiEnabled ?? true,
            aiDailyQuota: editing.aiDailyQuota ?? null,
            createAdmin: false,
            adminUsername: editing.adminUsername,
            adminPassword: '',
            adminEmail: '',
          }
        : empty(),
    )
  }, [open, editing, form])

  const fail = (e: unknown) => toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
  const openForm = (row: TenantRow | null) => {
    setEditing(row)
    setOpen(true)
  }

  const submit = form.handleSubmit(async ({ isNew, ...body }) => {
    try {
      // 初始密码只在新建且勾选“同时创建管理员”时提交；编辑时不带管理员相关字段
      const payload =
        isNew && body.createAdmin
          ? body
          : { ...body, createAdmin: false, adminPassword: undefined, adminEmail: undefined, adminUsername: isNew ? '' : body.adminUsername }
      await save.mutateAsync({ id: editing?.id, body: payload })
      toast.success(t('common.operateSuccess'))
      setOpen(false)
    } catch (e) {
      fail(e)
    }
  })

  const openRenew = (row: TenantRow) => {
    const base = new Date(row.expireAt) > new Date() ? new Date(row.expireAt) : new Date()
    setRenewDate(yearsFromNow(base, 1))
    setRenewing(row)
  }
  const submitRenew = async () => {
    if (!renewing || !renewDate) return
    try {
      await renew.mutateAsync({ id: renewing.id, expireAt: renewDate })
      toast.success(t('common.operateSuccess'))
      setRenewing(null)
    } catch (e) {
      fail(e)
    }
  }

  const toggleStatus = async (row: TenantRow) => {
    const suspend = row.status === 'active'
    if (suspend && !(await confirm({ destructive: true, description: t('tenant.suspendConfirm', { name: row.name }) }))) return
    try {
      await setStatus.mutateAsync({ id: row.id, status: suspend ? 'suspended' : 'active' })
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      fail(e)
    }
  }

  const remove = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('tenant.deleteConfirm', { count: ids.length }) }))) return
    try {
      await del.mutateAsync(ids)
      toast.success(t('common.operateSuccess'))
      setSelected((s) => s.filter((id) => !ids.includes(id)))
    } catch (e) {
      fail(e)
    }
  }

  const columns: Column<TenantRow>[] = [
    { key: 'name', title: t('tenant.name'), mobile: 'title' },
    { key: 'domain', title: t('tenant.domain'), mobile: 'sub', render: (r) => <span className="num break-all">{r.domain}</span> },
    { key: 'packageId', title: t('tenant.package'), render: (r) => pkgName.get(r.packageId) ?? r.packageId },
    {
      key: 'expireAt',
      title: t('tenant.expireAt'),
      render: (r) => (
        <div className="flex flex-col items-start gap-1">
          <span className="num">{r.expireAt.slice(0, 10)}</span>
          <ExpireBadge expireAt={r.expireAt} status={r.status} />
        </div>
      ),
    },
    { key: 'usage', title: t('tenant.usage.title'), mobile: 'hidden', render: (r) => <TenantUsageCell id={r.id} /> },
    { key: 'status', title: t('common.status'), render: (r) => <DictTag dict="tenant_status" value={r.status} /> },
    { key: 'adminUsername', title: t('tenant.adminUsername'), mobile: 'hidden' },
    { key: 'createdAt', title: t('common.createdAt'), mobile: 'hidden', render: (r) => <span className="num">{r.createdAt.slice(0, 10)}</span> },
  ]

  const isNew = !editing
  const createAdmin = form.watch('createAdmin')

  return (
    <PageContainer>
      <PageHeader title={t('tenant.title')} description={t('tenant.description')} />

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
          placeholder={t('tenant.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <DictSelect
          className="md:w-40"
          dict="tenant_status"
          value={draft.status}
          onChange={(v) => setDraft((d) => ({ ...d, status: v }))}
          placeholder={t('common.status')}
        />
      </FilterBar>

      <DataTable<TenantRow>
        toolbar={
          <Can perm="tenant:create">
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
        columnSettingsKey="tenant"
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
          <Can perm="tenant:delete">
            <Button type="button" variant="destructive" size="sm" onClick={() => remove(ids)}>
              <Trash2 className="me-1 h-4 w-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <>
            <Can perm="tenant:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(row)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
              <Button type="button" variant="ghost" size="icon" aria-label={t('tenant.renew')} onClick={() => openRenew(row)}>
                <CalendarClock className="h-4 w-4" aria-hidden="true" />
              </Button>
              {row.status === 'active' ? (
                <Button type="button" variant="ghost" size="icon" aria-label={t('tenant.suspend')} onClick={() => toggleStatus(row)}>
                  <Ban className="h-4 w-4 text-warning" aria-hidden="true" />
                </Button>
              ) : row.status === 'suspended' ? (
                <Button type="button" variant="ghost" size="icon" aria-label={t('tenant.resume')} onClick={() => toggleStatus(row)}>
                  <CircleCheck className="h-4 w-4 text-success" aria-hidden="true" />
                </Button>
              ) : null}
            </Can>
            <Can perm="tenant:delete">
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
        title={editing ? t('tenant.edit') : t('tenant.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form="tenant-form" disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Form {...form}>
          <FormLayout id="tenant-form" onSubmit={submit}>
            <FormSection title={t('tenant.basicInfo')}>
              <FormGrid>
                <FormField
                  control={form.control}
                  name="name"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('tenant.name')}</FormLabel>
                      <FormControl>
                        <Input {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="domain"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('tenant.domain')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder="acme.example.com" inputMode="url" />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="packageId"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('tenant.package')}</FormLabel>
                      <FormControl>
                        <RemoteSelect url="/tenant-packages/options" value={field.value} onChange={field.onChange} allowClear={false} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name="expireAt"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('tenant.expireAt')}</FormLabel>
                      <FormControl>
                        <DatePicker value={field.value} onChange={field.onChange} allowClear={false} />
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
                        <DictSelect dict="tenant_status" value={field.value} onChange={field.onChange} allowClear={false} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGrid>
            </FormSection>
            <FormSection title={t('tenant.quotaInfo')}>
              <FormGrid>
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
                          min={0}
                          value={field.value ?? ''}
                          placeholder={t('tenant.quotaInherit')}
                          onChange={(e) => field.onChange(e.target.value === '' || Number.isNaN(e.target.valueAsNumber) ? null : e.target.valueAsNumber)}
                        />
                      </FormControl>
                      <FormDescription>{t('tenant.quotaHint')}</FormDescription>
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
                          min={0}
                          value={field.value ?? ''}
                          placeholder={t('tenant.quotaInherit')}
                          onChange={(e) => field.onChange(e.target.value === '' || Number.isNaN(e.target.valueAsNumber) ? null : e.target.valueAsNumber)}
                        />
                      </FormControl>
                      <FormDescription>{t('tenant.quotaHint')}</FormDescription>
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
                          min={0}
                          value={field.value ?? ''}
                          placeholder={t('tenant.quotaInherit')}
                          onChange={(e) => field.onChange(e.target.value === '' || Number.isNaN(e.target.valueAsNumber) ? null : e.target.valueAsNumber)}
                        />
                      </FormControl>
                      <FormDescription>{t('tenant.quotaHint')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGrid>
            </FormSection>
            <FormSection title={t('tenant.aiInfo')}>
              <FormField
                control={form.control}
                name="aiEnabled"
                render={({ field }) => (
                  <FormItem className="flex items-center justify-between gap-3">
                    <div>
                      <FormLabel>{t('tenant.aiEnabled')}</FormLabel>
                      <FormDescription>{t('tenant.aiEnabledHint')}</FormDescription>
                    </div>
                    <FormControl>
                      <Switch checked={field.value} onCheckedChange={field.onChange} aria-label={t('tenant.aiEnabled')} />
                    </FormControl>
                  </FormItem>
                )}
              />
              <FormGrid>
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
                          value={field.value ?? ''}
                          placeholder={t('tenant.quotaInherit')}
                          onChange={(e) => field.onChange(e.target.value === '' || Number.isNaN(e.target.valueAsNumber) ? null : e.target.valueAsNumber)}
                        />
                      </FormControl>
                      <FormDescription>{t('tenant.quotaHint')}</FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGrid>
            </FormSection>
            <FormSection title={t('tenant.adminInfo')}>
              {isNew ? (
                <FormField
                  control={form.control}
                  name="createAdmin"
                  render={({ field }) => (
                    <FormItem className="flex items-center justify-between gap-3">
                      <div>
                        <FormLabel>{t('tenant.createAdmin')}</FormLabel>
                        <FormDescription>{t('tenant.createAdminHint')}</FormDescription>
                      </div>
                      <FormControl>
                        <Switch checked={field.value} onCheckedChange={field.onChange} aria-label={t('tenant.createAdmin')} />
                      </FormControl>
                    </FormItem>
                  )}
                />
              ) : null}
              {!isNew || createAdmin ? (
                <FormGrid>
                  <FormField
                    control={form.control}
                    name="adminUsername"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('tenant.adminUsername')}</FormLabel>
                        <FormControl>
                          <Input {...field} disabled={!isNew} autoComplete="off" />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  {isNew ? (
                    <>
                      <FormField
                        control={form.control}
                        name="adminPassword"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('tenant.adminPassword')}</FormLabel>
                            <FormControl>
                              <Input {...field} type="password" autoComplete="new-password" />
                            </FormControl>
                            <FormDescription>{t('tenant.adminPasswordHint')}</FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name="adminEmail"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel>{t('tenant.adminEmail')}</FormLabel>
                            <FormControl>
                              <Input {...field} type="email" autoComplete="off" />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </>
                  ) : null}
                </FormGrid>
              ) : null}
            </FormSection>
          </FormLayout>
        </Form>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={!!renewing}
        onOpenChange={(o) => !o && setRenewing(null)}
        size="sm"
        title={t('tenant.renew')}
        description={renewing?.name}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setRenewing(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" disabled={renew.isPending || !renewDate} onClick={submitRenew}>
              {t('common.confirm')}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium text-foreground">{t('tenant.newExpireAt')}</span>
          <DatePicker value={renewDate} onChange={setRenewDate} allowClear={false} />
        </div>
      </ResponsiveDialog>
    </PageContainer>
  )
}
