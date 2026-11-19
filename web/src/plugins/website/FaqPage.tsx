import { useState } from 'react'
import { Download, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { FilterBar } from '@/framework/components/FilterBar'
import { OptionPicker } from '@/framework/components/OptionPicker'
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
  useDeleteFaqs,
  useFaqs,
  useSaveFaq,
  type FaqItem,
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

/** 常见问题管理：首页 FAQ 板块的数据源，条目按 sort 排序展示，enabled 关掉即整条隐藏。 */
export default function FaqPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { start: startExportJob } = useAsyncJob()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  // 表头排序：与筛选分开存；默认按 sort 升序（与官网展示顺序、后端回退一致）
  const { sort, desc, sortProp } = useHeaderSort('sort', false, () => setPage(1))
  const [draft, setDraft] = useState<{ keyword?: string; enabled?: string }>({})
  const [filters, setFilters] = useState<{ keyword?: string; enabled?: string }>({})
  const [selected, setSelected] = useState<string[]>([])
  const { data, isLoading } = useFaqs({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    enabled: filters.enabled,
    sort,
    desc,
  })
  const remove = useDeleteFaqs()
  const [editing, setEditing] = useState<FaqItem | null>(null)
  const [open, setOpen] = useState(false)

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
          setPage(1)
          setSelected([])
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
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
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => { setPage(p); setPageSize(s) },
        }}
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
    </PageContainer>
  )
}
