import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { ArrowLeft, Pencil, Plus, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictSelect } from '@/framework/components/DictSelect'
import { DictTag, tagVariant } from '@/framework/components/DictTag'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { useLanguages } from '@/framework/i18n/useLanguages'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import {
  TAG_VARIANTS,
  useDeleteDictData,
  useDictData,
  useSaveDictData,
  useSortDictData,
  type DictDataRow,
  type DictTypeRow,
} from './api'

const schema = z.object({
  label: z.string().trim().min(1).max(50),
  value: z.string().trim().min(1).max(50),
  tag: z.enum(['default', 'success', 'warning', 'destructive', 'info']),
  sort: z.number().int().min(0),
  status: z.string().min(1),
  remark: z.string().max(200).optional(),
})
type FormValues = z.infer<typeof schema>

const empty: FormValues = { label: '', value: '', tag: 'default', sort: 0, status: '1', remark: '' }

/** i18nJson → {语言: label}；坏数据当没有，不能因为一条脏数据让表单打不开。 */
function labelsOf(raw?: string): Record<string, string> {
  if (!raw) return {}
  try {
    const doc = JSON.parse(raw) as Record<string, { label?: string } | undefined>
    const out: Record<string, string> = {}
    for (const [code, fields] of Object.entries(doc)) if (fields?.label) out[code] = fields.label
    return out
  } catch {
    return {}
  }
}

/** {语言: label} → i18nJson；留空的语言不写，全空存空串（服务端按未翻译处理）。 */
function buildI18nJSON(map: Record<string, string>): string {
  const doc: Record<string, { label: string }> = {}
  for (const [code, label] of Object.entries(map)) {
    if (label.trim()) doc[code] = { label: label.trim() }
  }
  return Object.keys(doc).length ? JSON.stringify(doc) : ''
}

interface Props {
  type: DictTypeRow | null
  /** 手机上返回类型列表 */
  onBack?: () => void
}

export function DictDataPanel({ type, onBack }: Props) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [editing, setEditing] = useState<DictDataRow | null>(null)
  const [open, setOpen] = useState(false)
  const { data = [], isLoading } = useDictData(type?.id ?? null)
  const save = useSaveDictData()
  const del = useDeleteDictData()
  const sort = useSortDictData()

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  // 译文按语言存在 i18nJson 里（与内容侧同一份契约）；这里只需要 label 一个短字段，
  // 用不到内容表单那套语言切换/发布/AI，所以就地渲染每个非默认语言一个输入框。
  const [i18n, setI18n] = useState<Record<string, string>>({})
  const { languages } = useLanguages()
  const extraLangs = languages.filter((l) => !l.default)
  useEffect(() => {
    if (!open) return
    if (editing) {
      form.reset({
        label: editing.label,
        value: editing.value,
        tag: editing.tag,
        sort: editing.sort,
        status: editing.status,
        remark: editing.remark ?? '',
      })
      setI18n(labelsOf(editing.i18nJson))
    } else {
      form.reset({ ...empty, sort: (data.length + 1) * 10 })
      setI18n({})
    }
  }, [open, editing, form, data.length])

  const openForm = (row: DictDataRow | null) => {
    setEditing(row)
    setOpen(true)
  }

  const submit = form.handleSubmit(async (values) => {
    if (!type) return
    try {
      await save.mutateAsync({ id: editing?.id, body: { ...values, typeId: type.id, i18nJson: buildI18nJSON(i18n) } })
      toast.success(t('common.operateSuccess'))
      setOpen(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  const remove = async (row: DictDataRow) => {
    if (!(await confirm({ destructive: true, description: t('dict.data.deleteConfirm', { name: row.label }) }))) return
    try {
      await del.mutateAsync(row.id)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<DictDataRow>[] = [
    {
      key: 'label',
      title: t('dict.data.label'),
      mobile: 'title',
      render: (r) => <Badge variant={tagVariant(r.tag)}>{r.label}</Badge>,
    },
    { key: 'value', title: t('dict.data.value'), mobile: 'sub', render: (r) => <span className="num">{r.value}</span> },
    { key: 'tag', title: t('dict.data.tag'), render: (r) => t(`dict.tag.${r.tag}`) },
    { key: 'sort', title: t('common.sort'), align: 'end', render: (r) => <span className="num">{r.sort}</span> },
    { key: 'status', title: t('common.status'), render: (r) => <DictTag dict="sys_status" value={r.status} /> },
    { key: 'remark', title: t('common.remark'), mobile: 'hidden' },
  ]

  if (!type) {
    return (
      <section className="flex min-h-40 items-center justify-center rounded-lg border border-border bg-card p-6 text-sm text-muted-foreground">
        {t('dict.data.selectType')}
      </section>
    )
  }

  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={t('dict.data.title')}>
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1">
          {onBack ? (
            <Button type="button" variant="ghost" size="icon" aria-label={t('common.back')} onClick={onBack}>
              <ArrowLeft className="h-4 w-4 rtl:rotate-180" aria-hidden="true" />
            </Button>
          ) : null}
          <div className="min-w-0">
            <h2 className="truncate text-base font-semibold text-foreground">{type.name}</h2>
            <p className="num truncate text-xs text-muted-foreground">{type.code}</p>
          </div>
        </div>
        <Can perm="dict:create">
          <Button type="button" size="sm" onClick={() => openForm(null)}>
            <Plus className="me-1 h-4 w-4" aria-hidden="true" />
            {t('common.add')}
          </Button>
        </Can>
      </div>
      <DataTable<DictDataRow>
        columns={columns}
        columnSettingsKey="dictData"
        data={data}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        sortable={{ onReorder: (ids) => sort.mutateAsync(ids) }}
        rowActions={(row) => (
          <>
            <Can perm="dict:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(row)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="dict:delete">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => remove(row)}>
                <Trash2 className="h-4 w-4 text-destructive" aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />

      <ResponsiveDialog
        open={open}
        onOpenChange={setOpen}
        title={editing ? t('dict.data.edit') : t('dict.data.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form="dict-data-form" disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Form {...form}>
          <FormLayout id="dict-data-form" onSubmit={submit}>
            <FormGrid>
              <FormGridFull>
                <FormField
                  control={form.control}
                  name="label"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('dict.data.label')}</FormLabel>
                      <FormControl>
                        <Input {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </FormGridFull>
              {extraLangs.length ? (
                <FormGridFull>
                  <div className="flex flex-col gap-2">
                    <span className="text-sm font-medium leading-none">{t('dict.data.translations')}</span>
                    {extraLangs.map((l) => (
                      <div key={l.code} className="flex flex-col gap-1">
                        <label className="text-xs text-muted-foreground" htmlFor={`dict-label-${l.code}`}>
                          {l.name}
                        </label>
                        <Input
                          id={`dict-label-${l.code}`}
                          maxLength={100}
                          value={i18n[l.code] ?? ''}
                          placeholder={t('dict.data.translationPlaceholder')}
                          onChange={(e) => setI18n((prev) => ({ ...prev, [l.code]: e.target.value }))}
                        />
                      </div>
                    ))}
                  </div>
                </FormGridFull>
              ) : null}
              <FormField
                control={form.control}
                name="value"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('dict.data.value')}</FormLabel>
                    <FormControl>
                      <Input {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="tag"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('dict.data.tag')}</FormLabel>
                    <Select value={field.value} onValueChange={field.onChange}>
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {TAG_VARIANTS.map((v) => (
                          <SelectItem key={v} value={v}>
                            <Badge variant={tagVariant(v)}>{t(`dict.tag.${v}`)}</Badge>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name="sort"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('common.sort')}</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        inputMode="numeric"
                        value={Number.isNaN(field.value) ? '' : field.value}
                        onChange={(e) => field.onChange(e.target.valueAsNumber)}
                      />
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
    </section>
  )
}
