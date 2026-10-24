import { useState } from 'react'
import { ArrowLeft, Download, Eye, FileDown, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { EmptyState } from '@/framework/components/EmptyState'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  downloadXlsx,
  useDeleteSubmission,
  useExportJob,
  useSubmissionMeta,
  useSubmissions,
  type SubmissionQuery,
  type SubmissionRow,
} from './api'
import { formatTime } from './format'
import { displayValue, hasValue } from './schema'
import { SubmissionDetailDialog } from './SubmissionDetailDialog'

const ALL = '__all'
const VALUE_COLUMNS = 3

interface Filters {
  version?: string
  username?: string
  field?: string
  value?: string
  range?: DateRangeValue
}

/** 把筛选条件转成接口参数（不含分页）；列表与导出共用同一份，保证导出的就是看到的 */
function toQuery(f: Filters): SubmissionQuery {
  return {
    version: f.version,
    username: f.username?.trim() || undefined,
    field: f.field,
    value: f.field ? f.value?.trim() || undefined : undefined,
    from: f.range?.from,
    to: f.range?.to,
  }
}

export default function SubmissionsPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const [params] = useSearchParams()
  const id = params.get('id') ?? undefined
  const [draft, setDraft] = useState<Filters>({})
  const [filters, setFilters] = useState<Filters>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detailId, setDetailId] = useState<string | null>(null)

  const { data: meta, isError } = useSubmissionMeta(id)
  const { data, isLoading } = useSubmissions(id, { ...toQuery(filters), page, pageSize })
  const remove = useDeleteSubmission()
  const exportJob = useExportJob()
  const fields = (meta?.definition.fields ?? []).filter(hasValue)
  const back = () => navigate('/system/formbuilder')

  const onDelete = async (row: SubmissionRow) => {
    if (!id || !(await confirm({ destructive: true, description: t('plugin_formbuilder.submissions.deleteConfirm', { name: row.username }) }))) return
    try {
      await remove.mutateAsync({ formId: id, id: row.id })
      toast.success(t('common.operateSuccess'))
    } catch {
      /* 已提示 */
    }
  }
  const onExport = async () => {
    if (!id) return
    try {
      await downloadXlsx(id, meta?.name ?? '', toQuery(filters))
    } catch {
      /* 已提示 */
    }
  }
  const onExportJob = async () => {
    if (!id) return
    try {
      await exportJob.mutateAsync({ id, query: toQuery(filters) })
      toast.success(t('plugin_formbuilder.submissions.exportJobSubmitted'))
    } catch {
      /* 已提示 */
    }
  }

  if (!id || isError) {
    return (
      <PageContainer>
        <EmptyState
          title={t('plugin_formbuilder.designer.notFound')}
          action={
            <Button variant="outline" onClick={back}>
              <ArrowLeft aria-hidden="true" />
              {t('common.back')}
            </Button>
          }
        />
      </PageContainer>
    )
  }

  const yes = t('common.yes')
  const no = t('common.no')
  const columns: Column<SubmissionRow>[] = [
    { key: 'username', title: t('plugin_formbuilder.submissions.submitter'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.username}</span> },
    { key: 'createdAt', title: t('plugin_formbuilder.submissions.submittedAt'), mobile: 'sub', render: (r) => <span className="num">{formatTime(r.createdAt, i18n.language)}</span> },
    { key: 'version', title: t('plugin_formbuilder.fields.version'), render: (r) => <span className="num">v{r.version}</span> },
    ...fields.slice(0, VALUE_COLUMNS).map(
      (f): Column<SubmissionRow> => ({
        key: `f_${f.key}`,
        title: f.title,
        render: (r) => {
          const text = displayValue(f, r.data[f.key], yes, no)
          return text ? <span className="line-clamp-2 max-w-64 break-words">{text}</span> : <span className="text-muted-foreground">-</span>
        },
      }),
    ),
  ]

  return (
    <PageContainer>
      <PageHeader
        title={meta?.name ?? t('plugin_formbuilder.btn.submission')}
        description={t('plugin_formbuilder.submissions.description')}
        actions={
          <Button variant="outline" onClick={back}>
            <ArrowLeft aria-hidden="true" />
            {t('common.back')}
          </Button>
        }
      />
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
        <Select value={draft.version ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, version: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-36" aria-label={t('plugin_formbuilder.fields.version')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>
              {t('plugin_formbuilder.fields.version')}: {t('plugin_formbuilder.all')}
            </SelectItem>
            {(meta?.versions ?? []).map((v) => (
              <SelectItem key={v} value={String(v)}>
                v{v}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="md:w-44"
          value={draft.username ?? ''}
          maxLength={64}
          placeholder={t('plugin_formbuilder.submissions.submitter')}
          aria-label={t('plugin_formbuilder.submissions.submitter')}
          onChange={(e) => setDraft((d) => ({ ...d, username: e.target.value }))}
        />
        <Select value={draft.field ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, field: v === ALL ? undefined : v, value: undefined }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_formbuilder.submissions.filterField')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_formbuilder.submissions.filterField')}</SelectItem>
            {fields.map((f) => (
              <SelectItem key={f.key} value={f.key}>
                {f.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="md:w-44"
          value={draft.value ?? ''}
          maxLength={200}
          disabled={!draft.field}
          placeholder={t('plugin_formbuilder.submissions.filterValue')}
          aria-label={t('plugin_formbuilder.submissions.filterValue')}
          onChange={(e) => setDraft((d) => ({ ...d, value: e.target.value }))}
        />
        <DateRangePicker value={draft.range} onChange={(range) => setDraft((d) => ({ ...d, range }))} placeholder={t('plugin_formbuilder.submissions.submittedAt')} />
      </FilterBar>
      {data?.truncated ? (
        <p role="status" className="rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
          {t('plugin_formbuilder.submissions.truncated')}
        </p>
      ) : null}
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-formbuilder-submissions"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_formbuilder.submissions.empty')}
        toolbar={
          <Can perm="formbuilder:export">
            <Button variant="outline" onClick={() => void onExport()}>
              <Download aria-hidden="true" />
              {t('plugin_formbuilder.submissions.exportXlsx')}
            </Button>
            <Button variant="outline" disabled={exportJob.isPending} onClick={() => void onExportJob()}>
              <FileDown aria-hidden="true" />
              {t('plugin_formbuilder.submissions.exportJob')}
            </Button>
          </Can>
        }
        rowActions={(r) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('plugin_formbuilder.submissions.detail')} title={t('plugin_formbuilder.submissions.detail')} onClick={() => setDetailId(r.id)}>
              <Eye aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} title={t('common.delete')} onClick={() => void onDelete(r)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </>
        )}
      />
      <SubmissionDetailDialog formId={id} submissionId={detailId} onClose={() => setDetailId(null)} />
    </PageContainer>
  )
}
