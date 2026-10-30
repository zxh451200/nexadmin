import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Pencil, Plus, Search, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DictSelect } from '@/framework/components/DictSelect'
import { FormGrid, FormGridFull, FormLayout } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { cn } from '@/lib/utils'
import { useDeleteDictType, useDictTypes, useSaveDictType, useSortDictTypes, type DictTypeRow } from './api'

const schema = z.object({
  name: z.string().trim().min(1).max(50),
  code: z
    .string()
    .trim()
    .min(1)
    .max(64)
    .regex(/^[a-z][a-z0-9_]*$/),
  sort: z.number().int().min(0),
  status: z.string().min(1),
  remark: z.string().max(200).optional(),
})
type FormValues = z.infer<typeof schema>

const empty: FormValues = { name: '', code: '', sort: 0, status: '1', remark: '' }

interface Props {
  selectedId: string | null
  /** 需保持引用稳定（useCallback） */
  onSelect: (row: DictTypeRow) => void
  onDeleted: (id: string) => void
}

export function DictTypePanel({ selectedId, onSelect, onDeleted }: Props) {
  const { t } = useTranslation()
  const { has } = usePermission()
  const confirm = useConfirm()
  const [input, setInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [editing, setEditing] = useState<DictTypeRow | null>(null)
  const [open, setOpen] = useState(false)
  const { data = [], isLoading } = useDictTypes(keyword)
  const save = useSaveDictType()
  const del = useDeleteDictType()
  const sort = useSortDictTypes()

  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: empty })
  useEffect(() => {
    if (open) {
      form.reset(
        editing
          ? { name: editing.name, code: editing.code, sort: editing.sort, status: editing.status, remark: editing.remark ?? '' }
          : empty,
      )
    }
  }, [open, editing, form])

  const openForm = (row: DictTypeRow | null) => {
    setEditing(row)
    setOpen(true)
  }

  const submit = form.handleSubmit(async (values) => {
    try {
      await save.mutateAsync({ id: editing?.id, body: values })
      toast.success(t('common.operateSuccess'))
      setOpen(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  })

  const remove = async (row: DictTypeRow) => {
    if (!(await confirm({ destructive: true, description: t('dict.type.deleteConfirm', { name: row.name }) }))) return
    try {
      await del.mutateAsync(row.id)
      toast.success(t('common.operateSuccess'))
      onDeleted(row.id)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<DictTypeRow>[] = [
    {
      key: 'name',
      title: t('dict.type.name'),
      mobile: 'title',
      render: (r) => (
        <div className="min-w-0">
          <div className={cn('truncate font-medium', r.id === selectedId && 'text-primary')}>{r.name}</div>
          <div className="num truncate text-xs text-muted-foreground">
            {r.code}
            {r.builtin && <span className="ms-2 rounded bg-muted px-1.5 py-0.5">{t('dict.type.builtin')}</span>}
          </div>
        </div>
      ),
    },
  ]

  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={t('dict.type.title')}>
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-base font-semibold text-foreground">{t('dict.type.title')}</h2>
        <Can perm="dict:create">
          <Button type="button" size="sm" onClick={() => openForm(null)}>
            <Plus className="me-1 h-4 w-4" aria-hidden="true" />
            {t('common.add')}
          </Button>
        </Can>
      </div>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          setKeyword(input.trim())
        }}
      >
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={t('dict.type.searchPlaceholder')}
          aria-label={t('common.search')}
        />
        <Button type="submit" variant="outline" size="icon" aria-label={t('common.search')} className="shrink-0">
          <Search className="h-4 w-4" aria-hidden="true" />
        </Button>
      </form>
      <DataTable<DictTypeRow>
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        onRowClick={onSelect}
        sortable={keyword || !has('dict:update') ? undefined : { onReorder: (ids) => sort.mutateAsync(ids) }}
        rowActions={(row) => (
          <>
            <Can perm="dict:update">
              <Button type="button" variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(row)}>
                <Pencil className="h-4 w-4" aria-hidden="true" />
              </Button>
            </Can>
            {!row.builtin && (
              <Can perm="dict:delete">
                <Button type="button" variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => remove(row)}>
                  <Trash2 className="h-4 w-4 text-destructive" aria-hidden="true" />
                </Button>
              </Can>
            )}
          </>
        )}
      />

      <ResponsiveDialog
        open={open}
        onOpenChange={setOpen}
        title={editing ? t('dict.type.edit') : t('dict.type.create')}
        footer={
          <>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" form="dict-type-form" disabled={save.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Form {...form}>
          <FormLayout id="dict-type-form" onSubmit={submit}>
            <FormGrid>
              <FormField
                control={form.control}
                name="name"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('dict.type.name')}</FormLabel>
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
                    <FormLabel>{t('dict.type.code')}</FormLabel>
                    <FormControl>
                      <Input {...field} disabled={!!editing} placeholder="sys_status" />
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
