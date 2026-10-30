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
import { DateTimePicker } from '@/framework/components/DateTimePicker'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Uploader, type UploadedFile } from '@/framework/components/Uploader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import {
  translateContent,
  useDeletePost,
  useI18nSchema,
  useLanguages,
  usePosts,
  useSavePost,
  type ContentQuery,
  type SitePost,
} from './api'

const ALL = '__all'

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * 可翻译字段交给 LocaleFormProvider，slug/封面这类共用字段留在本地 state。
 * 弹窗外壳也由本组件渲染：底部按钮要用这里的 submit 与 state，必须留在 Provider 内。
 */
function PostFormBody({ onOpenChange, editing }: { onOpenChange: (v: boolean) => void; editing: SitePost | null }) {
  const { t } = useTranslation()
  const save = useSavePost()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [slug, setSlug] = useState(editing?.slug ?? '')
  const [coverUrl, setCoverUrl] = useState(editing?.coverUrl ?? '')
  const [coverOpen, setCoverOpen] = useState(false)
  const [coverFiles, setCoverFiles] = useState<UploadedFile[]>([])
  const [publishedAt, setPublishedAt] = useState(editing?.publishedAt ?? '')
  // slug 是所有语言共用的，但每门语言有各自的路径前缀；提示按当前编辑的语言拼。
  const pathPrefix = lf.locales.find((l) => l.code === lf.active)?.prefix
  const articlePath = `${pathPrefix ? `/${pathPrefix}` : ''}/news/${slug || '…'}`
  // 站内相对路径或绝对地址；别的一律拦住，否则存下去就是死链（公开页还有 og:image 跟着指向它）。
  const coverInvalid = coverUrl.trim() !== '' && !/^(https?:\/\/|\/)/i.test(coverUrl.trim())

  const submit = async () => {
    const base = lf.baseValues()
    if (!(base.title ?? '').trim() || !slug.trim()) {
      toast.error(t('plugin_website.post.required'))
      return
    }
    if (coverInvalid) {
      toast.error(t('plugin_website.fields.coverInvalid'))
      return
    }
    try {
      await save.mutateAsync({
        id: editing?.id,
        data: {
          slug,
          coverUrl,
          published: lf.publishedOf(lf.defaultLocale),
          // 只有发布态才有发布时间；空串表示交给服务端按「首次发布写当前时间」处理
          publishedAt: lf.publishedOf(lf.defaultLocale) && publishedAt ? publishedAt : undefined,
          ...lf.serialize(),
          // 行上的裸列永远存原文
          title: base.title ?? '',
          summary: base.summary ?? '',
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
      title={editing ? t('plugin_website.post.edit') : t('plugin_website.post.create')}
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
              maxLength={64}
              value={slug}
              disabled={sharedDisabled}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="hello-nexadmin"
              className="font-mono"
            />
            <span className="text-xs text-muted-foreground">{articlePath}</span>
          </SharedFieldShell>
        </div>
        <SharedFieldShell labelKey="plugin_website.fields.coverUrl">
          <div className="flex items-center gap-2">
            <Input
              value={coverUrl}
              maxLength={255}
              disabled={sharedDisabled}
              aria-invalid={coverInvalid}
              onChange={(e) => setCoverUrl(e.target.value)}
              placeholder={t('plugin_website.fields.coverPlaceholder')}
            />
            <Button type="button" variant="outline" size="sm" className="shrink-0" disabled={sharedDisabled} onClick={() => setCoverOpen(true)}>
              {t('common.upload')}
            </Button>
            {coverUrl ? (
              <Button type="button" variant="ghost" size="sm" className="shrink-0" disabled={sharedDisabled} onClick={() => setCoverUrl('')}>
                {t('common.clear')}
              </Button>
            ) : null}
          </div>
          {coverUrl ? (
            <img src={coverUrl} alt="" loading="lazy" className="mt-2 h-24 w-40 rounded-md border border-border object-cover" />
          ) : null}
          {coverInvalid ? (
            <span className="text-xs text-destructive">{t('plugin_website.fields.coverInvalid')}</span>
          ) : null}
        </SharedFieldShell>
        <LocalizedField fieldKey="summary" />
        <LocalizedField fieldKey="seoDesc" />
        <LocalizedField fieldKey="content" className="min-h-40" />
        <LocaleFormActions />
        <SwitchRow
          label={t('plugin_website.fields.published')}
          checked={lf.publishedOf(lf.defaultLocale)}
          onCheckedChange={(on) => lf.setPublishedOf(lf.defaultLocale, on)}
        />
        {/* 发布时间：只在发布时生效，留空则由服务端在首次发布时写当前时间 */}
        {lf.publishedOf(lf.defaultLocale) ? (
          <SharedFieldShell labelKey="plugin_website.fields.publishedAt">
            <DateTimePicker
              value={publishedAt}
              allowClear
              onChange={(v) => setPublishedAt(v)}
              placeholder={t('plugin_website.fields.publishedAtHint')}
            />
          </SharedFieldShell>
        ) : null}
      </div>
      <Dialog open={coverOpen} onOpenChange={setCoverOpen}>
        <DialogContent className="w-[calc(100vw-1.5rem)] max-w-md">
          <DialogHeader className="text-start">
            <DialogTitle>{t('plugin_website.fields.coverUpload')}</DialogTitle>
            <DialogDescription>{t('plugin_website.fields.coverHint')}</DialogDescription>
          </DialogHeader>
          <Uploader
            kind="image"
            visibility="public"
            crop={{ width: 1200, height: 630 }}
            value={coverFiles}
            onChange={(files) => {
              setCoverFiles([])
              const last = files[files.length - 1]
              if (last) {
                setCoverUrl(last.url)
                setCoverOpen(false)
              }
            }}
          />
        </DialogContent>
      </Dialog>
    </ResponsiveDialog>
  )
}

function PostFormDialog({
  onOpenChange, editing, initialLocale,
}: {
  onOpenChange: (v: boolean) => void
  editing: SitePost | null
  initialLocale?: string
}) {
  const { data: languages = [] } = useLanguages()
  const { data: schema, isError, refetch } = useI18nSchema('post')
  const defaultLocale = languages.find((l) => l.default)?.code ?? ''
  // 传输函数交给框架组件：它不认识插件路由，只负责按格式分批调用。
  const translate = useCallback<TranslateBatch>(
    (texts, to, format) => translateContent('post', texts, defaultLocale, to, format),
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
      base={{ title: editing?.title ?? '', summary: editing?.summary ?? '', seoDesc: editing?.seoDesc ?? '', content: editing?.content ?? '' }}
      i18nJson={editing?.i18nJson}
      publishedJson={editing?.publishedJson}
      sourcePublished={editing?.published ?? false}
      initialLocale={initialLocale}
      translate={translate}
    >
      <PostFormBody onOpenChange={onOpenChange} editing={editing} />
    </LocaleFormProvider>
  )
}

export default function PostPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('post')
  const { data: languages = [] } = useLanguages()
  const [draft, setDraft] = useState<ContentQuery>({})
  const [filters, setFilters] = useState<ContentQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = usePosts({ ...filters, page, pageSize })
  const extraLocales = languages.filter((l) => !l.default)
  const remove = useDeletePost()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SitePost | null>(null)
  // 点译文徽标直接定位到那门语言；nonce 让「重复点同一个语言」也能重新触发定位。
  const [focus, setFocus] = useState<{ code: string; nonce: number } | null>(null)

  const openAt = (row: SitePost | null, code?: string) => {
    setEditing(row)
    setFocus(code ? { code, nonce: Date.now() } : null)
    setFormOpen(true)
  }

  const onDelete = async (row: SitePost) => {
    if (await confirm({ destructive: true, description: t('plugin_website.post.deleteConfirm', { name: row.title }) })) {
      try {
        await remove.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<SitePost>[] = [
    { key: 'title', title: t('plugin_website.fields.postTitle'), mobile: 'title', render: (r) => <span className="font-medium text-foreground">{r.title}</span> },
    { key: 'slug', title: t('plugin_website.fields.slug'), mobile: 'sub', render: (r) => <span className="font-mono text-xs">/news/{r.slug}</span> },
    {
      key: 'i18n',
      title: t('plugin_website.i18n.status'),
      render: (r) => <TranslationStatus value={r.i18nJson} fields={['title', 'summary', 'seoDesc', 'content']} onFocus={(code) => openAt(r, code)} />,
    },
    { key: 'published', title: t('plugin_website.fields.published'), render: (r) => (r.published ? <Badge variant="success">{t('plugin_website.publishedOn')}</Badge> : <Badge variant="outline">{t('plugin_website.publishedOff')}</Badge>) },
    {
      key: 'link',
      title: '',
      align: 'end',
      render: (r) => (r.published ? (
        <Button asChild variant="ghost" size="icon" aria-label={t('plugin_website.viewSite')}>
          <a href={`/news/${r.slug}`} target="_blank" rel="noreferrer">
            <ExternalLink aria-hidden="true" />
          </a>
        </Button>
      ) : null),
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.post.title')} description={t('plugin_website.post.desc')} />
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
        {extraLocales.length ? (
          <Select value={draft.missing ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, missing: v === ALL ? undefined : v }))}>
            <SelectTrigger className="md:w-48" aria-label={t('plugin_website.i18n.filterLabel')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('plugin_website.all')}</SelectItem>
              {extraLocales.map((l) => (
                <SelectItem key={l.code} value={l.code}>
                  {t('plugin_website.i18n.filterNoTranslation', { lang: l.label })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-website-posts"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => { setPage(p); setPageSize(s) },
        }}
        emptyText={t('plugin_website.empty')}
        toolbar={
          <Can perm="website:post">
            <Button onClick={() => openAt(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_website.post.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:post">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openAt(row)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && (
        <PostFormDialog
          key={`${editing?.id ?? 'new'}-${focus?.code ?? ''}-${focus?.nonce ?? 0}`}
          onOpenChange={setFormOpen}
          editing={editing}
          initialLocale={focus?.code}
        />
      )}
    </PageContainer>
  )
}
