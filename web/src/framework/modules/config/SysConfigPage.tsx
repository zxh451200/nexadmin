import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchField } from '@/framework/components/SwitchField'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import {
  CONFIG_VALUE_TYPES,
  useConfigs,
  useDeleteConfigs,
  useRefreshConfigCache,
  useSaveConfig,
  type ConfigRow,
} from './api'

const schema = z.object({
  name: z.string().trim().min(1).max(50),
  key: z
    .string()
    .trim()
    .min(1)
    .max(100)
    .regex(/^[a-z][a-z0-9_.]*$/),
  value: z.string().max(2000),
  type: z.enum(['string', 'number', 'boolean', 'json']),
  isPublic: z.boolean(),
  remark: z.string().max(200).optional(),
})
type FormValues = z.infer<typeof schema>
const empty: FormValues = { name: '', key: '', value: '', type: 'string', isPublic: false, remark: '' }
const ALL = 'all'

export function ParamsSection() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [draft, setDraft] = useState({ keyword: '', type: ALL })
  const [filters, setFilters] = useState({ keyword: '', type: ALL })
  const [selected, setSelected] = useState<string[]>([])
  const [editing, setEditing] = useState<ConfigRow | null>(null)
  const [open, setOpen] = useState(false)

  const { data, isLoading } = useConfigs({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    type: filters.type === ALL ? undefined : filters.type,
  })
  const save = useSaveConfig()
  const del = useDeleteConfigs()
  const refresh = useRefreshConfigCache()

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  const type = form.watch('type')
  useEffect(() => {
    if (!open) return
    form.reset(
      editing
        ? {
            name: editing.name,
            key: editing.key,
            value: editing.value,
            type: editing.type,
            isPublic: editing.isPublic,
            remark: editing.remark ?? '',
          }
        : empty,
    )
  }, [open, editing, form])

  const openForm = (row: ConfigRow | null) => {
    setEditing(row)
    setOpen(true)
  }
  const fail = (e: unknown) => toast.error(e instanceof Error ? e.message : t('common.operateFailed'))

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
    if (!(await confirm({ destructive: true, description: t('sysConfig.deleteConfirm', { count: ids.length }) }))) return
    try {
      await del.mutateAsync(ids)
      toast.success(t('common.operateSuccess'))
      setSelected((s) => s.filter((id) => !ids.includes(id)))
    } catch (e) {
      fail(e)
    }
  }

  const refreshCache = async () => {
    try {
      await refresh.mutateAsync()
      toast.success(t('sysConfig.refreshed'))
    } catch (e) {
      fail(e)
    }
  }

  // 出厂参数目录的名称/说明按参数键取词条（双语）；管理员新建或改过文案的行回落到库里文本。
  const cfgText = (key: string, field: 'name' | 'remark', fallback?: string | null) =>
    t(`sysConfig.item.${key.replaceAll('.', '_')}.${field}`, { defaultValue: fallback ?? '' })

  const columns: Column<ConfigRow>[] = [
    { key: 'name', title: t('sysConfig.name'), mobile: 'title', render: (r) => cfgText(r.key, 'name', r.name) },
    { key: 'key', title: t('sysConfig.key'), mobile: 'sub', render: (r) => <span className="num break-all">{r.key}</span> },
    {
      key: 'value',
      title: t('sysConfig.value'),
      // 值可能很长（JSON / 白名单），列表里截两行，悬停看全文
      render: (r) => (
        <span className="num line-clamp-2 max-w-64 break-all" title={r.value}>
          {r.value}
        </span>
      ),
    },
    { key: 'type', title: t('sysConfig.type'), render: (r) => t(`sysConfig.types.${r.type}`) },
    {
      key: 'isPublic',
      title: t('sysConfig.isPublic'),
      render: (r) => <DictTag dict="yes_no" value={r.isPublic ? '1' : '0'} />,
    },
    { key: 'remark', title: t('common.remark'), mobile: 'hidden', render: (r) => cfgText(r.key, 'remark', r.remark) },
  ]

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">{t('sysConfig.description')}</p>

      <FilterBar
        onSearch={() => {
          setPage(1)
          setFilters(draft)
        }}
        onReset={() => {
          const init = { keyword: '', type: ALL }
          setDraft(init)
          setFilters(init)
          setPage(1)
        }}
      >
        <Input
          className="md:w-64"
          value={draft.keyword}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('sysConfig.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.type} onValueChange={(v) => setDraft((d) => ({ ...d, type: v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('sysConfig.type')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('sysConfig.allTypes')}</SelectItem>
            {CONFIG_VALUE_TYPES.map((v) => (
              <SelectItem key={v} value={v}>
                {t(`sysConfig.types.${v}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>

      <DataTable<ConfigRow>
        toolbar={
          <>
            <Can perm="config:update">
              <Button type="button" variant="outline" onClick={refreshCache} disabled={refresh.isPending}>
                <RefreshCw className="me-2 h-4 w-4" aria-hidden="true" />
                {t('sysConfig.refreshCache')}
              </Button>
            </Can>
            <Can perm="config:create">
              <Button type="button" onClick={() => openForm(null)}>
                <Plus className="me-2 h-4 w-4" aria-hidden="true" />
                {t('common.add')}
              </Button>
            </Can>
          </>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="sysConfig"
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
          <Can perm="config:delete">
            <Button type="button" variant="destructive" size="sm" onClick={() => remove(ids)}>
              <Trash2 className="me-1 h-4 w-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <>
            <Can perm="config:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(row)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="config:delete">
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
        title={editing ? t('sysConfig.edit') : t('sysConfig.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form="sys-config-form" disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Form {...form}>
          <FormLayout id="sys-config-form" onSubmit={submit}>
            <FormGrid>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysConfig.name')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="key"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysConfig.key')}</FormLabel>
                    <FormControl>
                      <Input {...field} disabled={!!editing} placeholder="site.name" />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="type"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('sysConfig.type')}</FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {CONFIG_VALUE_TYPES.map((v) => (
                          <SelectItem key={v} value={v}>
                            {t(`sysConfig.types.${v}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormGridFull>
                <SwitchField control={form.control} name="isPublic" label={t('sysConfig.isPublic')} />
              </FormGridFull>
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="value"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('sysConfig.value')}</FormLabel>
                      <FormControl>
                        {type === 'boolean' ? (
                          <div className="flex min-h-11 items-center md:min-h-9">
                            <Switch
                              checked={field.value === 'true'}
                              onCheckedChange={(c) => field.onChange(c ? 'true' : 'false')}
                            />
                          </div>
                        ) : type === 'json' ? (
                          <Textarea rows={4} className="num" {...field} />
                        ) : (
                          <Input {...field} type={type === 'number' ? 'number' : 'text'} />
                        )}
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
                        <Textarea rows={2} {...field} value={field.value ?? ''} />
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
    </div>
  )
}
