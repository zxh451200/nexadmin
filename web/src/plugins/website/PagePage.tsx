import { useCallback, useState } from 'react'
import { Copy, ExternalLink, Eye, ListPlus, Pencil, Plus, Trash2 } from 'lucide-react'
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
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { EditorSchemaGate } from './EditorSchemaGate'
import { formatTime, slugify } from './format'
import TranslationStatus from './TranslationStatus'
import {
  fetchPagePreviewURL,
  translateContent,
  useCopyPage,
  useDeletePage,
  useDeletePages,
  useI18nSchema,
  useLanguages,
  useNavs,
  usePages,
  useSaveNav,
  useSavePage,
  useSetPagesStatus,
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
  const [ogImage, setOgImage] = useState(editing?.ogImage ?? '')
  const [noindex, setNoindex] = useState(editing?.noindex ?? false)
  const [sort, setSort] = useState(editing?.sort ?? 0)
  // 分享图与封面同一套规则：站内相对路径或绝对地址，别的拦住——og:image 跟着指向它。
  const ogInvalid = ogImage.trim() !== '' && !/^(https?:\/\/|\/)/i.test(ogImage.trim())

  const submit = async () => {
    const base = lf.baseValues()
    if (!(base.title ?? '').trim()) {
      toast.error(t('plugin_website.page.required'))
      return
    }
    if (ogInvalid) {
      toast.error(t('plugin_website.fields.coverInvalid'))
      return
    }
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          // slug 留空时前端按标题预填（与后端 pkg/slug 同一算法）；这里始终非空
          slug: slug.trim() || slugify(base.title ?? ''),
          published: lf.publishedOf(lf.defaultLocale),
          ogImage,
          noindex,
          sort,
          ...lf.serialize(),
          // 行上的裸列永远存原文
          title: base.title ?? '',
          seoTitle: base.seoTitle ?? '',
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
        <LocalizedField fieldKey="title" required />
        <SharedFieldShell labelKey="plugin_website.fields.slug">
          <Input
            value={slug}
            disabled={sharedDisabled}
            onChange={(e) => setSlug(e.target.value)}
            placeholder={t('plugin_website.fields.slugAuto')}
            className="font-mono"
          />
          <span className="text-xs text-muted-foreground">/p/{slug || '…'}</span>
        </SharedFieldShell>
        <LocalizedField fieldKey="seoTitle" />
        <LocalizedField fieldKey="seoDesc" />
        <LocalizedField fieldKey="content" className="min-h-40" />
        <LocaleFormActions />
        <SharedFieldShell labelKey="plugin_website.fields.ogImage">
          <ImageUrlField
            value={ogImage}
            onChange={setOgImage}
            disabled={sharedDisabled}
            title={t('plugin_website.fields.ogUpload')}
            hint={t('plugin_website.fields.ogHint')}
            placeholder={t('plugin_website.fields.coverPlaceholder')}
            crop={{ width: 1200, height: 630 }}
            previewClassName="h-24 w-40 object-cover p-0"
            aria-label={t('plugin_website.fields.ogImage')}
          />
          <span className="text-xs text-muted-foreground">{t('plugin_website.fields.ogImageHint')}</span>
          {ogInvalid ? (
            <span className="text-xs text-destructive">{t('plugin_website.fields.coverInvalid')}</span>
          ) : null}
        </SharedFieldShell>
        <SwitchRow
          label={t('plugin_website.fields.published')}
          checked={lf.publishedOf(lf.defaultLocale)}
          onCheckedChange={(on) => lf.setPublishedOf(lf.defaultLocale, on)}
        />
        <SwitchRow
          label={t('plugin_website.fields.noindex')}
          checked={noindex}
          onCheckedChange={setNoindex}
        />
        <label className="flex flex-col gap-1 text-sm">
          <span>{t('plugin_website.fields.sort')}</span>
          <Input
            type="number"
            min={0}
            value={sort || 0}
            onChange={(e) => setSort(Math.max(0, Math.min(99999, Number(e.target.value) || 0)))}
          />
          <span className="text-xs text-muted-foreground">{t('plugin_website.fields.sortHint')}</span>
        </label>
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
      base={{ title: editing?.title ?? '', seoTitle: editing?.seoTitle ?? '', seoDesc: editing?.seoDesc ?? '', content: editing?.content ?? '' }}
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
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('page')
  const [draft, setDraft] = useState<ContentQuery>({})
  const [filters, setFilters] = useState<ContentQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])
  const { data, isLoading } = usePages({ ...filters, page, pageSize })
  const remove = useDeletePage()
  const removeBatch = useDeletePages()
  const setStatus = useSetPagesStatus()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SitePage | null>(null)

  const rows = data?.list ?? []
  const selectedRows = rows.filter((r) => selected.includes(r.id))

  const onDelete = async (rowsToDelete: SitePage | SitePage[]) => {
    const list = Array.isArray(rowsToDelete) ? rowsToDelete : [rowsToDelete]
    const description = Array.isArray(rowsToDelete)
      ? t('plugin_website.page.deleteBatchConfirm', { count: list.length })
      : t('plugin_website.page.deleteConfirm', { name: list[0].title })
    if (await confirm({ destructive: true, description })) {
      try {
        if (Array.isArray(rowsToDelete)) await removeBatch.mutateAsync(list.map((r) => r.id))
        else await remove.mutateAsync(list[0].id)
        toast.success(t('common.operateSuccess'))
        setSelected((s) => s.filter((id) => !list.some((r) => r.id === id)))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  // 批量发布/下线：只动原文发布位，各语言发布开关不碰（与新闻批量同语义）。
  const applyStatus = async (ids: string[], status: 'published' | 'draft') => {
    try {
      const { updated } = await setStatus.mutateAsync({ ids, status })
      if (updated < ids.length) toast.info(t('plugin_website.page.statusPartial', { updated, total: ids.length }))
      else toast.success(t('common.operateSuccess'))
      setSelected([])
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  // 草稿预览：后端签发短时效令牌，新开标签看未发布页面的真实版式。
  const onPreview = async (row: SitePage) => {
    try {
      const url = await fetchPagePreviewURL(row.id)
      window.open(url, '_blank', 'noopener')
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  // 复制为草稿：slug 由后端去重成 <slug>-copy，副本一律是未发布。
  const copy = useCopyPage()
  const onCopy = async (row: SitePage) => {
    try {
      const created = await copy.mutateAsync(row.id)
      toast.success(t('plugin_website.page.copied', { slug: created.slug }))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  // 一键加入导航：顶级、启用，label 用页面标题。已在导航里就只提示，不再重复建。
  const { data: navs = [] } = useNavs()
  const saveNav = useSaveNav()
  const onAddToNav = async (row: SitePage) => {
    const href = `/p/${row.slug}`
    if (navs.some((n) => n.href === href)) {
      toast.info(t('plugin_website.page.navExists'))
      return
    }
    try {
      await saveNav.mutateAsync({ data: { parentId: '0', label: row.title, href, sort: 0, enabled: true, i18nJson: '' } })
      toast.success(t('plugin_website.page.navAdded'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<SitePage>[] = [
    { key: 'title', title: t('plugin_website.fields.pageTitle'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.title}</span> },
    { key: 'slug', title: t('plugin_website.fields.slug'), mobile: 'sub', render: (r) => <span className="font-mono text-xs">/p/{r.slug}</span> },
    { key: 'i18n', title: t('plugin_website.i18n.status'), render: (r) => <TranslationStatus value={r.i18nJson} fields={['title', 'seoTitle', 'seoDesc', 'content']} /> },
    { key: 'published', title: t('plugin_website.fields.published'), render: (r) => (r.published ? <Badge variant="success">{t('plugin_website.publishedOn')}</Badge> : <Badge variant="outline">{t('plugin_website.publishedOff')}</Badge>) },
    { key: 'updatedAt', title: t('plugin_website.fields.updatedAt'), render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.updatedAt, i18n.language)}</span> },
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
      ) : (
        <Button variant="ghost" size="icon" aria-label={t('plugin_website.page.preview')} onClick={() => void onPreview(r)}>
          <Eye aria-hidden="true" />
        </Button>
      )),
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
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="website:page">
            <Button variant="outline" size="sm" onClick={() => void applyStatus(ids, 'published')}>
              {t('plugin_website.page.publishBatch')}
            </Button>
            <Button variant="outline" size="sm" onClick={() => void applyStatus(ids, 'draft')}>
              {t('plugin_website.page.offlineBatch')}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void onDelete(selectedRows)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
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
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.page.copy')} onClick={() => void onCopy(row)}>
              <Copy aria-hidden="true" />
            </Button>
            <Can perm="website:nav">
              <Button variant="ghost" size="icon" aria-label={t('plugin_website.page.addToNav')} onClick={() => void onAddToNav(row)}>
                <ListPlus aria-hidden="true" />
              </Button>
            </Can>
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
