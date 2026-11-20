import { useEffect, useState } from 'react'
import { CircleAlert, CircleCheck, Download, Loader2, Pencil, Plus, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { FilterBar } from '@/framework/components/FilterBar'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { FileDropField } from '@/framework/components/FileDropField'
import { saveBlob } from '@/framework/components/export/csv'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Textarea } from '@/framework/ui/textarea'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import {
  createFaqExportJob,
  FAQ_IMPORT_SOURCE,
  fetchProductImportJob,
  fetchProductImportTemplate,
  useDeleteFaqs,
  useFaqs,
  useReorderFaqs,
  useRunProductImport,
  useSaveFaq,
  type FaqItem,
  type ProductImportJob,
} from './api'

/** FAQ 编辑弹窗：问题 + 答案（纯文本，公开页换行保留）+ 排序 + 启用。 */
function FaqFormBody({ editing, onOpenChange }: { editing: FaqItem | null; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const save = useSaveFaq()
  const [question, setQuestion] = useState(editing?.question ?? '')
  const [answer, setAnswer] = useState(editing?.answer ?? '')
  const [sort, setSort] = useState(String(editing?.sort ?? 0))
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)

  const submit = async () => {
    if (!question.trim()) return
    try {
      await save.mutateAsync({ id: editing?.id, question: question.trim(), answer, sort: Number(sort) || 0, enabled })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={editing ? t('plugin_website.faq.editTitle') : t('plugin_website.faq.addTitle')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()} disabled={save.isPending || !question.trim()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="faq-question">{t('plugin_website.faq.fields.question')}</Label>
          <Input
            id="faq-question"
            value={question}
            maxLength={300}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder={t('plugin_website.faq.questionPlaceholder')}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="faq-answer">{t('plugin_website.faq.fields.answer')}</Label>
          <Textarea
            id="faq-answer"
            rows={6}
            value={answer}
            onChange={(e) => setAnswer(e.target.value)}
            placeholder={t('plugin_website.faq.answerPlaceholder')}
          />
          <p className="text-xs text-muted-foreground">{t('plugin_website.faq.answerHint')}</p>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="faq-sort">{t('plugin_website.faq.fields.sort')}</Label>
          <Input id="faq-sort" type="number" value={sort} onChange={(e) => setSort(e.target.value)} />
        </div>
        <SwitchRow label={t('plugin_website.faq.fields.enabled')} checked={enabled} onCheckedChange={setEnabled} />
      </div>
    </ResponsiveDialog>
  )
}

/**
 * FAQ 导入弹窗（页内完成，不跳导入中心）：模板下载、文件上传、进度与逐行失败清单。
 * 执行走通用导入中心接口（source=website_faqs）：行数少同步完成，多时后台跑、这里轮询。
 * 与 ProductImportDialog 同一形态；导入执行/模板/轮询是通用导入中心接口，按 source 复用。
 */
function FaqImportDialog({ onOpenChange }: { onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const run = useRunProductImport()
  const [file, setFile] = useState<File | null>(null)
  const [job, setJob] = useState<ProductImportJob | null>(null)
  const running = job?.status === 'pending' || job?.status === 'running'
  // 后台任务未结束前每 2s 轮询台账；结束时把最终数据换上（错误明细随单条任务返回）。
  useEffect(() => {
    if (!running || !job) return
    const timer = setInterval(() => {
      void fetchProductImportJob(job.id)
        .then(setJob)
        .catch(() => undefined)
    }, 2000)
    return () => clearInterval(timer)
  }, [job, running])

  const downloadTemplate = async () => {
    try {
      const blob = await fetchProductImportTemplate(FAQ_IMPORT_SOURCE)
      saveBlob(`${t('plugin_website.faq.import.templateFilename')}.csv`, blob)
    } catch {
      // 服务端原因由 http 拦截器统一弹出
    }
  }

  const start = async () => {
    if (!file) return
    try {
      setJob(await run.mutateAsync({ file, source: FAQ_IMPORT_SOURCE }))
    } catch {
      // 服务端原因（文件不合法、无导入权限等）由 http 拦截器统一弹出
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      size="md"
      title={t('plugin_website.faq.import.title')}
      description={t('plugin_website.faq.import.description')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.close')}</Button>
          <Button onClick={() => void start()} disabled={!file || run.isPending || running}>
            {run.isPending || running ? t('common.loading') : t('plugin_website.faq.import.start')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label htmlFor="faq-import-file">{t('plugin_website.faq.import.file')}</Label>
            <Button type="button" variant="ghost" size="sm" className="-me-2 text-muted-foreground" onClick={() => void downloadTemplate()}>
              <Download aria-hidden="true" />
              {t('plugin_website.faq.import.template')}
            </Button>
          </div>
          <FileDropField
            accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            labelledBy="faq-import-file"
            files={file ? [file] : []}
            onChange={(fs) => {
              setFile(fs[0] ?? null)
              setJob(null)
            }}
          />
          <p className="text-xs leading-relaxed text-muted-foreground">{t('plugin_website.faq.import.fileHint')}</p>
        </div>
        {job ? (
          <div className="flex flex-col gap-2.5 rounded-lg border border-border bg-muted/50 p-3" role="status">
            <p className="flex items-center gap-2 text-sm font-medium text-foreground">
              {running ? (
                <Loader2 className="size-4 shrink-0 animate-spin" aria-hidden="true" />
              ) : (job.errors?.length ?? 0) > 0 || job.status === 'failed' ? (
                <CircleAlert className="size-4 shrink-0 text-warning" aria-hidden="true" />
              ) : (
                <CircleCheck className="size-4 shrink-0 text-success" aria-hidden="true" />
              )}
              {running
                ? t('plugin_website.faq.import.running')
                : job.status === 'failed'
                  ? t('plugin_website.faq.import.failed')
                  : t('plugin_website.faq.import.result', { ok: job.okRows, failed: job.failRows })}
            </p>
            {!running && (job.errors?.length ?? 0) > 0 ? (
              <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto text-sm text-destructive">
                {job.errors!.map((e) => (
                  <li key={e.row}>{t('plugin_website.faq.import.errorRow', { row: e.row, message: e.message })}</li>
                ))}
              </ul>
            ) : null}
          </div>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}

/** 常见问题管理：首页 FAQ 板块的数据源，条目按 sort 排序展示，enabled 关掉即整条隐藏。 */
export default function FaqPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { start: startExportJob } = useAsyncJob()
  // FAQ 是首页板块条目，量级小：整表拉取（服务端单页上限 200），不做服务端分页——
  // DataTable 的拖动排序只在无分页时可用（拖完要能按全量顺序落库）。
  const page = 1
  // 表头排序：与筛选分开存；默认按 sort 升序（与官网展示顺序、后端回退一致）
  const { sort, desc, sortProp } = useHeaderSort('sort', false)
  const [draft, setDraft] = useState<{ keyword?: string; enabled?: string }>({})
  const [filters, setFilters] = useState<{ keyword?: string; enabled?: string }>({})
  const [selected, setSelected] = useState<string[]>([])
  const { data, isLoading } = useFaqs({
    page,
    pageSize: 200,
    keyword: filters.keyword || undefined,
    enabled: filters.enabled,
    sort,
    desc,
  })
  const remove = useDeleteFaqs()
  const reorder = useReorderFaqs()
  const [editing, setEditing] = useState<FaqItem | null>(null)
  const [open, setOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)

  // 拖动排序只在「默认视图」开放：带筛选/表头排序时列表是查询结果，
  // 拖完落库的顺序语义说不清，索性不给把手。
  const canDrag = !filters.keyword && !filters.enabled && sort === 'sort' && !desc
  const onReorder = (ids: string[]) => reorder.mutateAsync(ids)

  const openAdd = () => {
    setEditing(null)
    setOpen(true)
  }
  const openEdit = (f: FaqItem) => {
    setEditing(f)
    setOpen(true)
  }

  const [exporting, setExporting] = useState(false)
  // 导出沿用当前筛选（不是当前页）：统一走异步任务，进度与下载都在导出中心看。
  const onExport = async () => {
    setExporting(true)
    try {
      await startExportJob(() => createFaqExportJob({ keyword: filters.keyword, enabled: filters.enabled }))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }

  const onDelete = async (rows: FaqItem[]) => {
    const ok = await confirm({
      destructive: true,
      description: rows.length === 1
        ? t('plugin_website.faq.deleteConfirm', { question: rows[0].question })
        : t('plugin_website.faq.deleteBatchConfirm', { count: rows.length }),
    })
    if (!ok) return
    try {
      await remove.mutateAsync(rows.map((r) => r.id))
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const selectedRows = (data?.list ?? []).filter((r) => selected.includes(r.id))

  const columns: Column<FaqItem>[] = [
    {
      key: 'question',
      title: t('plugin_website.faq.fields.question'),
      mobile: 'title',
      sortKey: 'question',
      render: (f) => (
        <div className="min-w-0">
          <span className="block max-w-[24rem] truncate font-medium text-foreground" title={f.question}>{f.question}</span>
          <span className="block max-w-[24rem] truncate text-xs text-muted-foreground" title={f.answer}>{f.answer}</span>
        </div>
      ),
    },
    {
      key: 'sort',
      title: t('plugin_website.faq.fields.sort'),
      align: 'end',
      sortKey: 'sort',
      render: (f) => <span className="num">{f.sort}</span>,
    },
    {
      key: 'status',
      title: t('common.status'),
      render: (f) => (
        <Badge variant={f.enabled ? 'success' : 'outline'}>{f.enabled ? t('plugin_website.faq.status.on') : t('plugin_website.faq.status.off')}</Badge>
      ),
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.faq.title')} description={t('plugin_website.faq.description')} />
      <FilterBar
        onSearch={() => {
          setFilters({ keyword: draft.keyword, enabled: draft.enabled })
          setSelected([])
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setSelected([])
        }}
      >
        <Input
          className="md:w-64"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_website.faq.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <OptionPicker
          title={t('common.status')}
          value={draft.enabled ? [draft.enabled] : []}
          onChange={(v) => setDraft((d) => ({ ...d, enabled: v.length ? v[0] : undefined }))}
          options={[
            { value: 'true', label: t('plugin_website.faq.status.on') },
            { value: 'false', label: t('plugin_website.faq.status.off') },
          ]}
          className="md:w-40"
          placeholder={t('common.status')}
        />
      </FilterBar>
      <DataTable
        sort={sortProp}
        toolbar={
          <Can perm="website:faq">
            <Button onClick={openAdd}>
              <Plus aria-hidden="true" />
              {t('plugin_website.faq.add')}
            </Button>
            {/* 导入在页内弹窗完成（模板下载/上传/失败清单），与产品导入同一套导入中心接口。 */}
            <Button variant="outline" onClick={() => setImportOpen(true)}>
              <Upload aria-hidden="true" />
              {t('plugin_website.faq.importEntry')}
            </Button>
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download aria-hidden="true" />
              {t('common.export')}
            </Button>
          </Can>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="website-faqs"
        sortable={canDrag ? { onReorder } : undefined}
        pagination={false}
        emptyText={t('plugin_website.faq.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={() => (
          <Can perm="website:faq">
            <Button type="button" variant="destructive" size="sm" onClick={() => void onDelete(selectedRows)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <Can perm="website:faq">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openEdit(row)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => void onDelete([row])}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {open ? <FaqFormBody editing={editing} onOpenChange={setOpen} /> : null}
      {importOpen ? <FaqImportDialog onOpenChange={setImportOpen} /> : null}
    </PageContainer>
  )
}
