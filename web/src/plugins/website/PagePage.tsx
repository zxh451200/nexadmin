import { useCallback, useState } from 'react'
import { ExternalLink, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import {
  LocaleFormActions,
  LocaleFormProvider,
  LocalizedField,
  LocaleSwitcher,
  SharedFieldShell,
  useLocaleForm,
  useSharedField,
  type TranslateBatch,
} from '@/framework/components/i18n'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  translateContent,
  useDeletePage,
  useI18nSchema,
  useLanguages,
  usePages,
  useSavePage,
  type ContentQuery,
  type SitePage,
} from './api'

const ALL = '__all'

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * title/seoDesc/content 归 LocaleFormProvider，slug 这类共用字段留在本地 state。
 * 弹窗外壳也由本组件渲染：底部按钮要用这里的 submit 与 state，必须留在 Provider 内。
 */
function PageFormBody({ onOpenChange, editing }: { onOpenChange: (v: boolean) => void; editing: SitePage | null }) {
  const { t } = useTranslation()
  const save = useSavePage()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [slug, setSlug] = useState(editing?.slug ?? '')

  const submit = async () => {
    const base = lf.baseValues()
    if (!(base.title ?? '').trim() || !slug.trim()) {
      toast.error(t('plugin_website.page.required'))
      return
    }
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          slug,
          published: lf.publishedOf(lf.defaultLocale),
          ...lf.serialize(),
          // 行上的裸列永远存原文
          title: base.title ?? '',
          seoDesc: base.seoDesc ?? '',
          content: base.content ?? '',
        },
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={editing ? t('plugin_website.page.edit') : t('plugin_website.page.create')}
      size="lg"
      className="space-y-4"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex items-center justify-between gap-2 pb-1">
        <LocaleSwitcher />
        <span className="text-xs text-muted-foreground">{t('plugin_website.i18n.switchHint')}</span>
      </div>
      <div className="flex flex-col gap-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <LocalizedField fieldKey="title" required />
          <SharedFieldShell labelKey="plugin_website.fields.slug" required>
            <Input
              required
              value={slug}
              disabled={sharedDisabled}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="about"
              className="font-mono"
            />
            <span className="text-xs text-muted-foreground">/p/{slug || '…'}</span>
          </SharedFieldShell>
        </div>
        <LocalizedField fieldKey="seoDesc" />
        <LocalizedField fieldKey="content" className="min-h-40" />
        <LocaleFormActions />
        <SwitchRow
          label={t('plugin_website.fields.published')}
          checked={lf.publishedOf(lf.defaultLocale)}
          onCheckedChange={(on) => lf.setPublishedOf(lf.defaultLocale, on)}
        />
      </div>
    </ResponsiveDialog>
  )
}

function PageFormDialog({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: SitePage | null
}) {
  const { data: languages = [] } = useLanguages()
  const { data: schema, isError, refetch } = useI18nSchema('page')
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('page', texts, defaultLocale, to, format),
    [defaultLocale],
  )

  // schema 与语言清单是表单的前提，没到货前不给渲染——否则会闪出一个没有字段的空表单。
  if (!schema || !defaultLocale) {
    return <EditorSchemaGate onOpenChange={onOpenChange} error={isError} onRetry={() => void refetch()} />
  }

  return (
    <LocaleFormProvider
      schema={schema}
      locales={languages}
      defaultLocale={defaultLocale}
      base={{ title: editing?.title ?? '', seoDesc: editing?.seoDesc ?? '', content: editing?.content ?? '' }}
      i18nJson={editing?.i18nJson}
      publishedJson={editing?.publishedJson}
      sourcePublished={editing?.published ?? false}
      translate={translate}
    >
      <PageFormBody onOpenChange={onOpenChange} editing={editing} />
    </LocaleFormProvider>
  )
}

export default function PagePage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('page')
  const [draft, setDraft] = useState<ContentQuery>({})
  const [filters, setFilters] = useState<ContentQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = usePages({ ...filters, page, pageSize })
  const remove = useDeletePage()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SitePage | null>(null)

  const onDelete = async (row: SitePage) => {
    if (await confirm({ destructive: true, description: t('plugin_website.page.deleteConfirm', { name: row.title }) })) {
      try {
        await remove.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<SitePage>[] = [
    { key: 'title', title: t('plugin_website.fields.pageTitle'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.title}</span> },
    { key: 'slug', title: t('plugin_website.fields.slug'), mobile: 'sub', render: (r) => <span className="font-mono text-xs">/p/{r.slug}</span> },
    { key: 'i18n', title: t('plugin_website.i18n.status'), render: (r) => <TranslationStatus value={r.i18nJson} fields={['title', 'seoDesc', 'content']} /> },
    { key: 'published', title: t('plugin_website.fields.published'), render: (r) => (r.published ? <Badge variant="success">{t('plugin_website.publishedOn')}</Badge> : <Badge variant="outline">{t('plugin_website.publishedOff')}</Badge>) },
    {
      key: 'link',
      title: '',
      align: 'end',
      render: (r) => (r.published ? (
        <Button asChild variant="ghost" size="icon" aria-label={t('plugin_website.viewSite')}>
          <a href={`/p/${r.slug}`} target="_blank" rel="noreferrer">
            <ExternalLink aria-hidden="true" />
          </a>
        </Button>
      ) : null),
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.page.title')} description={t('plugin_website.page.desc')} />
      <FilterBar
        onSearch={() => { setFilters(draft); setPage(1) }}
        onReset={() => { setDraft({}); setFilters({}); setPage(1) }}
      >
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_website.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.status ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, status: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_website.fields.published')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_website.all')}</SelectItem>
            <SelectItem value="published">{t('plugin_website.publishedOn')}</SelectItem>
            <SelectItem value="draft">{t('plugin_website.publishedOff')}</SelectItem>
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-pages"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => { setPage(p); setPageSize(s) },
        }}
        emptyText={t('plugin_website.empty')}
        toolbar={
          <Can perm="website:page">
            <Button onClick={() => { setEditing(null); setFormOpen(true) }}>
              <Plus aria-hidden="true" />
              {t('plugin_website.page.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:page">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => { setEditing(row); setFormOpen(true) }}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && <PageFormDialog key={editing?.id ?? 'new'} onOpenChange={setFormOpen} editing={editing} />}
    </PageContainer>
  )
}
