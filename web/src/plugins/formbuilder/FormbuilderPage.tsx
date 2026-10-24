import { useRef, useState } from 'react'
import { Copy, Download, Inbox, MoreHorizontal, Pencil, Plus, Power, PowerOff, Send, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  downloadDefinition,
  useCopyForm,
  useDeleteForm,
  useForms,
  useImportForm,
  usePublish,
  useSetOnline,
  type DefinitionFile,
  type FormSummary,
} from './api'
import { CreateFormDialog } from './CreateFormDialog'
import { formatTime, statusVariant } from './format'

const ALL = '__all'
const MAX_IMPORT_BYTES = 64 * 1024

interface Filters {
  keyword?: string
  status?: string
}

export default function FormbuilderPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const fileRef = useRef<HTMLInputElement>(null)
  const [draft, setDraft] = useState<Filters>({})
  const [filters, setFilters] = useState<Filters>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [createOpen, setCreateOpen] = useState(false)

  const { data, isLoading } = useForms({ ...filters, page, pageSize })
  const copy = useCopyForm()
  const remove = useDeleteForm()
  const publish = usePublish()
  const setOnline = useSetOnline()
  const importForm = useImportForm()

  const openDesigner = (id: string) => navigate(`/system/formbuilder/design?id=${id}`)
  // 服务端的错误提示已由请求层弹出，这里只负责成功提示
  const attempt = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
    } catch {
      /* 已提示 */
    }
  }

  const onCopy = (f: FormSummary) => {
    const name = `${f.name.slice(0, 100 - t('plugin_formbuilder.copySuffix').length - 1)} ${t('plugin_formbuilder.copySuffix')}`
    void attempt(() => copy.mutateAsync({ id: f.id, name }))
  }
  const onPublish = async (f: FormSummary) => {
    if (await confirm({ title: t('plugin_formbuilder.btn.publish'), description: t('plugin_formbuilder.publishConfirm', { name: f.name }) })) {
      await attempt(() => publish.mutateAsync(f.id))
    }
  }
  const onOffline = async (f: FormSummary) => {
    if (await confirm({ title: t('plugin_formbuilder.offline'), description: t('plugin_formbuilder.offlineConfirm', { name: f.name }) })) {
      await attempt(() => setOnline.mutateAsync({ id: f.id, online: false }))
    }
  }
  const onDelete = async (f: FormSummary) => {
    if (await confirm({ destructive: true, description: t('plugin_formbuilder.deleteConfirm', { name: f.name, count: f.submissionCount }) })) {
      await attempt(() => remove.mutateAsync(f.id))
    }
  }
  const onExportDefinition = async (f: FormSummary) => {
    try {
      await downloadDefinition(f.id, f.name)
    } catch {
      /* 已提示 */
    }
  }
  const onImportFile = async (file: File | undefined) => {
    if (fileRef.current) fileRef.current.value = ''
    if (!file) return
    if (file.size > MAX_IMPORT_BYTES) {
      toast.error(t('plugin_formbuilder.import.tooLarge'))
      return
    }
    try {
      const parsed = JSON.parse(await file.text()) as Partial<DefinitionFile>
      if (parsed?.format !== 'nexadmin.formbuilder' || !parsed.definition) throw new Error('format')
      await attempt(() => importForm.mutateAsync(parsed as DefinitionFile))
    } catch {
      toast.error(t('plugin_formbuilder.import.invalid'))
    }
  }

  const columns: Column<FormSummary>[] = [
    {
      key: 'name',
      title: t('plugin_formbuilder.fields.name'),
      mobile: 'title',
      render: (f) => (
        <div className="flex min-w-0 flex-col">
          <span className="font-medium text-foreground">{f.name}</span>
          {f.description ? <span className="line-clamp-1 text-xs text-muted-foreground">{f.description}</span> : null}
        </div>
      ),
    },
    {
      key: 'status',
      title: t('plugin_formbuilder.fields.status'),
      mobile: 'sub',
      render: (f) => (
        <div className="flex flex-wrap items-center gap-1">
          <Badge variant={statusVariant(f.status)}>{t(`plugin_formbuilder.status.${f.status}`)}</Badge>
          {f.status !== 'draft' && f.hasUnpublished ? <Badge variant="info">{t('plugin_formbuilder.unpublished')}</Badge> : null}
        </div>
      ),
    },
    {
      key: 'publishedVersion',
      title: t('plugin_formbuilder.fields.version'),
      render: (f) => (f.publishedVersion ? <span className="num">v{f.publishedVersion}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'submissionCount', title: t('plugin_formbuilder.fields.submissionCount'), align: 'end', render: (f) => <span className="num">{f.submissionCount}</span> },
    {
      key: 'deadlineAt',
      title: t('plugin_formbuilder.fields.deadline'),
      render: (f) => (f.deadlineAt ? <span className="num">{formatTime(f.deadlineAt, i18n.language)}</span> : <span className="text-muted-foreground">-</span>),
    },
    { key: 'updatedAt', title: t('common.updatedAt'), render: (f) => <span className="num">{formatTime(f.updatedAt, i18n.language)}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_formbuilder.title')} description={t('plugin_formbuilder.description')} />
      <FilterBar
        onSearch={() => {
          setFilters(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
        }}
      >
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_formbuilder.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_formbuilder.fields.status')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>
              {t('plugin_formbuilder.fields.status')}: {t('plugin_formbuilder.all')}
            </SelectItem>
            {(['draft', 'published', 'offline'] as const).map((s) => (
              <SelectItem key={s} value={s}>
                {t(`plugin_formbuilder.status.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>

      <input ref={fileRef} type="file" accept=".json,application/json" className="hidden" aria-label={t('plugin_formbuilder.import.title')} onChange={(e) => void onImportFile(e.target.files?.[0])} />
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-formbuilder-forms"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_formbuilder.empty')}
        toolbar={
          <Can perm="formbuilder:design">
            <Button variant="outline" onClick={() => fileRef.current?.click()} disabled={importForm.isPending}>
              <Upload aria-hidden="true" />
              {t('plugin_formbuilder.import.title')}
            </Button>
            <Button onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_formbuilder.create')}
            </Button>
          </Can>
        }
        rowActions={(f) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('plugin_formbuilder.design')} title={t('plugin_formbuilder.design')} onClick={() => openDesigner(f.id)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Can perm="formbuilder:submission">
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('plugin_formbuilder.btn.submission')}
                title={t('plugin_formbuilder.btn.submission')}
                onClick={() => navigate(`/system/formbuilder/submissions?id=${f.id}`)}
              >
                <Inbox aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="formbuilder:publish">
              {f.hasUnpublished ? (
                <Button variant="ghost" size="icon" aria-label={t('plugin_formbuilder.btn.publish')} title={t('plugin_formbuilder.btn.publish')} onClick={() => void onPublish(f)}>
                  <Send aria-hidden="true" />
                </Button>
              ) : null}
            </Can>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={t('common.more')} title={t('common.more')}>
                  <MoreHorizontal aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => onCopy(f)}>
                  <Copy className="me-2 size-4" aria-hidden="true" />
                  {t('plugin_formbuilder.copy')}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => void onExportDefinition(f)}>
                  <Download className="me-2 size-4" aria-hidden="true" />
                  {t('plugin_formbuilder.import.export')}
                </DropdownMenuItem>
                <Can perm="formbuilder:publish">
                  {f.status === 'published' ? (
                    <DropdownMenuItem onSelect={() => void onOffline(f)}>
                      <PowerOff className="me-2 size-4" aria-hidden="true" />
                      {t('plugin_formbuilder.offline')}
                    </DropdownMenuItem>
                  ) : null}
                  {f.status === 'offline' ? (
                    <DropdownMenuItem onSelect={() => void attempt(() => setOnline.mutateAsync({ id: f.id, online: true }))}>
                      <Power className="me-2 size-4" aria-hidden="true" />
                      {t('plugin_formbuilder.online')}
                    </DropdownMenuItem>
                  ) : null}
                </Can>
                <DropdownMenuSeparator />
                <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => void onDelete(f)}>
                  <Trash2 className="me-2 size-4" aria-hidden="true" />
                  {t('common.delete')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )}
      />
      <CreateFormDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={(f) => openDesigner(f.id)} />
    </PageContainer>
  )
}
