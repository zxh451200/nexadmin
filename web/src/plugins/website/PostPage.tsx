import { useCallback, useState } from 'react'
import type { ReactNode } from 'react'
import { Copy, Download, ExternalLink, Eye, History, Pencil, Plus, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Can } from '@/framework/auth/Can'
import { EmptyState } from '@/framework/components/EmptyState'
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
import { RequiredMark } from '@/framework/components/RequiredMark'
import { DateTimePicker } from '@/framework/components/DateTimePicker'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { ImageUrlField } from '@/framework/components/ImageUrlField'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { EditorSchemaGate } from './EditorSchemaGate'
import TranslationStatus from './TranslationStatus'
import { downloadCsv } from '@/framework/components/export/csv'
import { formatTime } from './format'
import {
  MAX_POST_VERSIONS,
  fetchPostPreview,
  fetchPostVersion,
  fetchPostsForExport,
  translateContent,
  useCategories,
  useCopyPost,
  useDeleteCategory,
  useDeletePost,
  useDeletePosts,
  useDeleteTag,
  useI18nSchema,
  useLanguages,
  usePosts,
  usePostVersions,
  useRollbackPost,
  useSaveCategory,
  useSavePost,
  useSaveTag,
  useSetPostStatus,
  useTags,
  type ContentQuery,
  type PostCategory,
  type PostTag,
  type PostVersionDetail,
  type SitePost,
} from './api'

const ALL = '__all'

/** 一篇新闻的标签上限，与后端 maxTagsPerPost 同值：界面先挡住，别等人保存才报错。 */
const MAX_TAGS = 20

/** 栏目 slug 的口径与后端 slugReg 一致：小写字母、数字与连字符，长度 1-64。 */
const slugReg = /^[a-z0-9][a-z0-9-]{0,63}$/

/**
 * 后端只给标签按名字派生 slug，栏目不会（公开侧 /news?category= 走的是同一套 slug 校验），
 * 所以栏目那行的「留空自动生成」得由界面做完：拉丁名压成小写连字符，压不出来就要求手填。
 */
function latinSlug(s: string): string {
  let out = ''
  let prevDash = false
  for (const r of s.toLowerCase()) {
    if (/[a-z0-9]/.test(r)) {
      out += r
      prevDash = false
    } else if (!prevDash && out) {
      out += '-'
      prevDash = true
    }
  }
  return out.replace(/-$/, '')
}

/** 定时发布：整篇已置发布、但发布时间还在未来——到点前官网不展示，列表里要标出来。 */
function isScheduled(r: SitePost): boolean {
  return r.published && !!r.publishedAt && new Date(r.publishedAt).getTime() > Date.now()
}

function statusBadge(r: SitePost, t: (k: string) => string) {
  if (isScheduled(r)) return <Badge variant="info">{t('plugin_website.post.scheduled')}</Badge>
  return r.published
    ? <Badge variant="success">{t('plugin_website.publishedOn')}</Badge>
    : <Badge variant="outline">{t('plugin_website.publishedOff')}</Badge>
}

/**
 * 分组外壳：与 SharedFieldShell 同款标签行，但外层是 div。
 * 栏目面板里第一颗复选框会被 label 认成「本标签的控件」，点标题就误勾一个栏目。
 */
function SharedGroup({ labelKey, children }: { labelKey: string; children: ReactNode }) {
  const { t } = useTranslation()
  const { isSource } = useLocaleForm()
  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className={cn('inline-flex min-h-7 items-center gap-1.5', !isSource && 'text-muted-foreground')}>
        <span className="inline-flex items-baseline">{t(labelKey)}</span>
        {!isSource ? (
          <span className="rounded border px-1 text-[10px] font-semibold">{t('localeForm.sharedTag')}</span>
        ) : null}
      </span>
      {children}
    </div>
  )
}

/**
 * 表单主体：整份表单只有一套，切语言换的是数据源。
 * 可翻译字段交给 LocaleFormProvider，slug/封面/分类/标签/关键词这类共用字段留在本地 state。
 * 弹窗外壳也由本组件渲染：底部按钮要用这里的 submit 与 state，必须留在 Provider 内。
 */
function PostFormBody({
  onOpenChange, editing, onManageCategories,
}: {
  onOpenChange: (v: boolean) => void
  editing: SitePost | null
  onManageCategories: () => void
}) {
  const { t } = useTranslation()
  const save = useSavePost()
  const saveCategory = useSaveCategory()
  const { data: categories = [], isLoading: categoriesLoading } = useCategories()
  const { data: knownTags = [] } = useTags()
  const lf = useLocaleForm()
  const { disabled: sharedDisabled } = useSharedField()
  const [slug, setSlug] = useState(editing?.slug ?? '')
  const [coverUrl, setCoverUrl] = useState(editing?.coverUrl ?? '')
  const [publishedAt, setPublishedAt] = useState(editing?.publishedAt ?? '')
  const [seoKeywords, setSeoKeywords] = useState(editing?.seoKeywords ?? '')
  const [noindex, setNoindex] = useState(editing?.noindex ?? false)
  const [pinned, setPinned] = useState(editing?.pinned ?? false)
  const [author, setAuthor] = useState(editing?.author ?? '')
  const [catIds, setCatIds] = useState<string[]>(() => (editing?.categories ?? []).map((c) => c.id))
  const [tags, setTags] = useState<string[]>(() => (editing?.tags ?? []).map((x) => x.name))
  const [tagInput, setTagInput] = useState('')
  const [newCatTitle, setNewCatTitle] = useState('')
  const [newCatSlug, setNewCatSlug] = useState('')
  const [catFormOpen, setCatFormOpen] = useState(false)
  // 一个栏目都没有时新建入口直接摊开：否则用户面对空面板只剩「分类与标签…」一条路。
  const catFormShown = catFormOpen || (!categories.length && !categoriesLoading)
  const atTagLimit = tags.length >= MAX_TAGS
  // slug 是所有语言共用的，但每门语言有各自的路径前缀；提示按当前编辑的语言拼。
  const pathPrefix = lf.locales.find((l) => l.code === lf.active)?.prefix
  const articlePath = `${pathPrefix ? `/${pathPrefix}` : ''}/news/${slug || '…'}`
  // 站内相对路径或绝对地址；别的一律拦住，否则存下去就是死链（公开页还有 og:image 跟着指向它）。
  const coverInvalid = coverUrl.trim() !== '' && !/^(https?:\/\/|\/)/i.test(coverUrl.trim())

  const addCategory = async () => {
    const title = newCatTitle.trim()
    if (!title) {
      toast.error(t('plugin_website.post.categoryRequired'))
      return
    }
    const slugValue = newCatSlug.trim() || latinSlug(title)
    if (!slugReg.test(slugValue)) {
      toast.error(t('plugin_website.post.categorySlugInvalid'))
      return
    }
    try {
      const row = await saveCategory.mutateAsync({ data: { title, slug: slugValue, sort: 0 } })
      // 建完就挂上：栏目是共用字段，译文态下这条入口已经置灰，不会走到这里
      setCatIds((ids) => (ids.includes(row.id) ? ids : [...ids, row.id]))
      setNewCatTitle('')
      setNewCatSlug('')
      setCatFormOpen(false)
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  /** 逗号/回车提交：名字按忽略大小写去重，超出上限的部分丢掉（输入框那时已经置灰）。 */
  const commitTags = (raw: string) => {
    const picked: string[] = []
    for (const part of raw.split(',')) {
      const name = part.trim().replace(/\s+/g, ' ').slice(0, 64)
      if (!name) continue
      if ([...tags, ...picked].some((x) => x.toLowerCase() === name.toLowerCase())) continue
      picked.push(name)
    }
    setTagInput('')
    if (picked.length) setTags((cur) => [...cur, ...picked].slice(0, MAX_TAGS))
  }

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
          // 只有发布态才有发布时间；空串表示交给服务端按「首次发布写当前时间」处理，
          // 设为未来时间即定时发布：到点前公开端不展示。
          publishedAt: lf.publishedOf(lf.defaultLocale) && publishedAt ? publishedAt : undefined,
          seoKeywords,
          noindex,
          pinned,
          author,
          categoryIds: catIds,
          tags,
          ...lf.serialize(),
          // 行上的裸列永远存原文
          title: base.title ?? '',
          summary: base.summary ?? '',
          seoTitle: base.seoTitle ?? '',
          seoDesc: base.seoDesc ?? '',
          content: base.content ?? '',
        },
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
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
        <SharedFieldShell labelKey="plugin_website.fields.coverUrl">
          <ImageUrlField
            value={coverUrl}
            onChange={setCoverUrl}
            disabled={sharedDisabled}
            title={t('plugin_website.fields.coverUpload')}
            hint={t('plugin_website.fields.coverHint')}
            placeholder={t('plugin_website.fields.coverPlaceholder')}
            crop={{ width: 1200, height: 630 }}
            previewClassName="h-24 w-40 object-cover p-0"
            aria-label={t('plugin_website.fields.coverUrl')}
          />
          {coverInvalid ? (
            <span className="text-xs text-destructive">{t('plugin_website.fields.coverInvalid')}</span>
          ) : null}
        </SharedFieldShell>
        <LocalizedField fieldKey="summary" />
        <LocalizedField fieldKey="seoTitle" />
        <LocalizedField fieldKey="seoDesc" />
        <LocalizedField fieldKey="content" className="min-h-40" />
        <LocaleFormActions />
        <SharedGroup labelKey="plugin_website.post.categories">
          <div className="flex flex-col gap-1.5" role="group" aria-label={t('plugin_website.post.categories')}>
            <div
              className={cn(
                'flex flex-col rounded-md border border-border p-1',
                categories.length > 6 ? 'max-h-40 overflow-y-auto' : undefined,
              )}
            >
              {categories.length ? categories.map((c) => (
                <label key={c.id} className="flex min-h-9 cursor-pointer items-center gap-2 rounded-sm px-2 text-sm hover:bg-accent/60">
                  <Checkbox
                    checked={catIds.includes(c.id)}
                    disabled={sharedDisabled}
                    onCheckedChange={(on) => setCatIds((ids) => (on ? [...ids, c.id] : ids.filter((x) => x !== c.id)))}
                    aria-label={c.title}
                  />
                  <span className="min-w-0 flex-1 truncate">{c.title}</span>
                  <span className="num text-xs text-muted-foreground">{c.postCount}</span>
                </label>
              )) : (
                <p className="px-2 py-3 text-xs text-muted-foreground">{t('plugin_website.post.categoriesEmpty')}</p>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setCatFormOpen((v) => !v)}>
                <Plus aria-hidden="true" />
                {t('plugin_website.post.categoryAdd')}
              </Button>
              {/* 分类与标签是全局清单，译文态下也能进去维护——它不动这篇新闻的挂载 */}
              <Button variant="link" size="sm" className="ms-auto h-auto p-0" onClick={onManageCategories}>
                {t('plugin_website.post.manageCategories')}
              </Button>
            </div>
            {catFormShown ? (
              <div className="flex flex-col gap-1 border-t border-border pt-2">
                {/* 行内输入没有 label 可挂星号：组提示行承担「标题必填」的标注 */}
                <p className="flex items-center gap-1 text-xs text-muted-foreground">
                  <RequiredMark /> {t('plugin_website.post.categoryTitleRequiredHint')}
                </p>
                <div className="flex flex-wrap items-center gap-2">
                  <Input
                    value={newCatTitle}
                    onChange={(e) => setNewCatTitle(e.target.value)}
                    placeholder={t('plugin_website.post.categoryTitle')}
                    aria-label={t('plugin_website.post.categoryTitle')}
                    className="w-40"
                    maxLength={100}
                    disabled={sharedDisabled}
                  />
                  <Input
                    value={newCatSlug}
                    onChange={(e) => setNewCatSlug(e.target.value)}
                    placeholder={t('plugin_website.post.categorySlugAuto')}
                    aria-label={t('plugin_website.post.categorySlug')}
                    className="w-40 font-mono"
                    maxLength={64}
                    disabled={sharedDisabled}
                  />
                  <Button size="sm" onClick={() => void addCategory()} disabled={sharedDisabled || saveCategory.isPending}>
                    <Plus aria-hidden="true" />
                    {t('plugin_website.post.categoryCreate')}
                  </Button>
                </div>
              </div>
            ) : null}
          </div>
        </SharedGroup>
        <SharedGroup labelKey="plugin_website.post.tags">
          <div className="flex flex-col gap-1.5" role="group" aria-label={t('plugin_website.post.tags')}>
            {tags.length ? (
              <div className="flex flex-wrap gap-1.5">
                {tags.map((name) => (
                  <Badge key={name} variant="secondary" className="ps-2.5 pe-0.5">
                    <span className="max-w-40 truncate">{name}</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-6"
                      disabled={sharedDisabled}
                      aria-label={t('plugin_website.post.tagRemove', { name })}
                      onClick={() => setTags((cur) => cur.filter((x) => x !== name))}
                    >
                      <X aria-hidden="true" />
                    </Button>
                  </Badge>
                ))}
              </div>
            ) : null}
            <div className="flex items-center gap-2">
              <Input
                value={tagInput}
                maxLength={64}
                list="post-tag-options"
                disabled={sharedDisabled || atTagLimit}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ',') {
                    e.preventDefault()
                    commitTags(tagInput)
                  }
                }}
                placeholder={t('plugin_website.post.tagInputPlaceholder')}
                aria-label={t('plugin_website.post.tags')}
              />
              <Button
                variant="outline"
                size="sm"
                disabled={sharedDisabled || atTagLimit || !tagInput.trim()}
                onClick={() => commitTags(tagInput)}
              >
                <Plus aria-hidden="true" />
                {t('plugin_website.post.tagAdd')}
              </Button>
            </div>
            {atTagLimit ? (
              <span className="text-xs text-muted-foreground">{t('plugin_website.post.tagLimit', { max: MAX_TAGS })}</span>
            ) : null}
            {/* 补全走已有标签：同一个主题词长成新名字，公开侧就多出两个入口 */}
            <datalist id="post-tag-options">
              {knownTags.map((x) => (
                <option key={x.id} value={x.name} />
              ))}
            </datalist>
          </div>
        </SharedGroup>
        <SharedFieldShell labelKey="plugin_website.fields.postSeoKeywords">
          <Input
            value={seoKeywords}
            maxLength={300}
            disabled={sharedDisabled}
            onChange={(e) => setSeoKeywords(e.target.value)}
            placeholder={t('plugin_website.fields.seoKeywords')}
          />
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
        <SwitchRow
          label={t('plugin_website.fields.pinned')}
          checked={pinned}
          onCheckedChange={setPinned}
        />
        <SharedFieldShell labelKey="plugin_website.fields.author">
          <Input
            value={author}
            maxLength={64}
            disabled={sharedDisabled}
            onChange={(e) => setAuthor(e.target.value)}
            placeholder={t('plugin_website.fields.authorPlaceholder')}
          />
        </SharedFieldShell>
        {/* 发布时间：只在发布时生效；设为未来时间即定时发布 */}
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
    </ResponsiveDialog>
  )
}

function PostFormDialog({
  onOpenChange, editing, initialLocale, onManageCategories,
}: {
  onOpenChange: (v: boolean) => void
  editing: SitePost | null
  initialLocale?: string
  onManageCategories: () => void
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
      base={{ title: editing?.title ?? '', summary: editing?.summary ?? '', seoTitle: editing?.seoTitle ?? '', seoDesc: editing?.seoDesc ?? '', content: editing?.content ?? '' }}
      i18nJson={editing?.i18nJson}
      publishedJson={editing?.publishedJson}
      sourcePublished={editing?.published ?? false}
      initialLocale={initialLocale}
      translate={translate}
    >
      <PostFormBody onOpenChange={onOpenChange} editing={editing} onManageCategories={onManageCategories} />
    </LocaleFormProvider>
  )
}

/** 分类与标签管理：紧凑的内联编辑弹窗，不值得单独开一个菜单页。 */
function TaxonomyManager({ onOpenChange }: { onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data: categories = [] } = useCategories()
  const { data: tags = [] } = useTags()
  const saveCategory = useSaveCategory()
  const removeCategory = useDeleteCategory()
  const saveTag = useSaveTag()
  const removeTag = useDeleteTag()
  const [newTitle, setNewTitle] = useState('')
  const [newSlug, setNewSlug] = useState('')

  const create = async () => {
    if (!newTitle.trim()) {
      toast.error(t('plugin_website.post.categoryRequired'))
      return
    }
    try {
      await saveCategory.mutateAsync({ data: { title: newTitle.trim(), slug: newSlug.trim(), sort: 0 } })
      setNewTitle('')
      setNewSlug('')
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  const update = async (row: PostCategory, title: string, slug: string) => {
    if (!title.trim()) {
      toast.error(t('plugin_website.post.categoryRequired'))
      return
    }
    if (title === row.title && slug === row.slug) return
    try {
      await saveCategory.mutateAsync({ id: row.id, data: { title: title.trim(), slug: slug.trim(), sort: row.sort } })
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  const onDeleteCategory = async (row: PostCategory) => {
    if (
      await confirm({
        destructive: true,
        description: t('plugin_website.post.categoryDeleteConfirm', { name: row.title, count: row.postCount }),
      })
    ) {
      try {
        await removeCategory.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        // 同上：拦截器已经把服务端原因说过了
      }
    }
  }

  const renameTag = async (row: PostTag, name: string) => {
    const trimmed = name.trim()
    if (!trimmed) {
      toast.error(t('plugin_website.post.tagRequired'))
      return
    }
    if (trimmed === row.name) return
    try {
      // 改名要回传原 slug：后端只在 slug 空着时按名字重新派生，换 slug 等于把分享出去的链接打断
      await saveTag.mutateAsync({ id: row.id, data: { name: trimmed, slug: row.slug } })
      toast.success(t('common.operateSuccess'))
    } catch {
      // 同上：拦截器已经把服务端原因说过了
    }
  }

  const onDeleteTag = async (row: PostTag) => {
    if (
      await confirm({
        destructive: true,
        description: t('plugin_website.post.tagDeleteConfirm', { name: row.name, count: row.postCount }),
      })
    ) {
      try {
        await removeTag.mutateAsync(row.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        // 同上：拦截器已经把服务端原因说过了
      }
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={t('plugin_website.post.taxonomyTitle')}
      size="md"
      footer={<Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.close')}</Button>}
    >
      <div className="flex flex-col gap-2">
        <p className="text-sm font-medium">{t('plugin_website.post.categories')}</p>
        {categories.map((c) => (
          <CategoryRow key={c.id} row={c} onSave={update} onDelete={() => void onDeleteCategory(c)} />
        ))}
        {!categories.length ? <p className="text-sm text-muted-foreground">{t('plugin_website.post.categoriesEmpty')}</p> : null}
        <div className="mt-2 flex flex-col gap-1 border-t border-border pt-3">
          {/* 行内输入没有 label 可挂星号：组提示行承担「标题必填」的标注 */}
          <p className="flex items-center gap-1 text-xs text-muted-foreground">
            <RequiredMark /> {t('plugin_website.post.categoryTitleRequiredHint')}
          </p>
          <div className="flex items-center gap-2">
            <Input
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              placeholder={t('plugin_website.post.categoryTitle')}
              aria-label={t('plugin_website.post.categoryTitle')}
              className="w-40"
              maxLength={100}
            />
            <Input
              value={newSlug}
              onChange={(e) => setNewSlug(e.target.value)}
              placeholder={t('plugin_website.post.categorySlug')}
              aria-label={t('plugin_website.post.categorySlug')}
              className="w-40 font-mono"
              maxLength={64}
            />
            <Button size="sm" onClick={() => void create()} disabled={!newTitle.trim()}>
              <Plus aria-hidden="true" />
              {t('plugin_website.post.categoryCreate')}
            </Button>
          </div>
        </div>
        <div className="mt-2 flex flex-col gap-2 border-t border-border pt-3">
          <p className="text-sm font-medium">{t('plugin_website.post.tags')}</p>
          {tags.map((x) => (
            <TagRow key={x.id} row={x} onSave={renameTag} onDelete={() => void onDeleteTag(x)} />
          ))}
          {/* 标签不需要在这里新建：新闻编辑器里打字即建，这一节只负责改名与清理 */}
          {!tags.length ? <p className="text-sm text-muted-foreground">{t('plugin_website.post.tagsEmpty')}</p> : null}
        </div>
      </div>
    </ResponsiveDialog>
  )
}

/** 一行分类：标题/slug 就地编辑，失焦或点保存提交；slug 改动要走保存按钮（slug 是路由标识，不能失焦误存）。 */
function CategoryRow({
  row, onSave, onDelete,
}: {
  row: PostCategory
  onSave: (row: PostCategory, title: string, slug: string) => Promise<void>
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const [title, setTitle] = useState(row.title)
  const [slug, setSlug] = useState(row.slug)

  return (
    <div className="flex items-center gap-2">
      <Input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onBlur={() => void onSave(row, title, slug)}
        aria-label={t('plugin_website.post.categoryTitle')}
        className="w-40"
        maxLength={100}
      />
      <Input
        value={slug}
        onChange={(e) => setSlug(e.target.value)}
        aria-label={t('plugin_website.post.categorySlug')}
        className="w-40 font-mono"
        maxLength={64}
      />
      <span className="num ms-auto shrink-0 text-xs text-muted-foreground">
        {t('plugin_website.post.postsCount', { count: row.postCount })}
      </span>
      <Button
        variant="ghost"
        size="icon"
        aria-label={t('common.save')}
        onClick={() => void onSave(row, title, slug)}
      >
        <Pencil aria-hidden="true" />
      </Button>
      <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={onDelete}>
        <Trash2 aria-hidden="true" />
      </Button>
    </div>
  )
}

/** 一行标签：只改名字，slug 不参与编辑——它是已分享链接的一部分。 */
function TagRow({
  row, onSave, onDelete,
}: {
  row: PostTag
  onSave: (row: PostTag, name: string) => Promise<void>
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(row.name)

  return (
    <div className="flex items-center gap-2">
      <Input
        value={name}
        onChange={(e) => setName(e.target.value)}
        onBlur={() => void onSave(row, name)}
        aria-label={t('plugin_website.post.tags')}
        className="w-40"
        maxLength={64}
      />
      <span className="num ms-auto shrink-0 text-xs text-muted-foreground">
        {t('plugin_website.post.postsCount', { count: row.postCount })}
      </span>
      <Button variant="ghost" size="icon" aria-label={t('common.save')} onClick={() => void onSave(row, name)}>
        <Pencil aria-hidden="true" />
      </Button>
      <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={onDelete}>
        <Trash2 aria-hidden="true" />
      </Button>
    </div>
  )
}

/**
 * 新闻版本历史弹窗（同 workflow 的 DefVersionsDialog 范式）：
 * 列表只有元数据；预览展开时单独拉那一条的正文，按「标题/摘要 + 字数」给信号，
 * 不把整篇 HTML 上屏。回滚是破坏性动作，确认后才走——它会把当前内容先归档，
 * 版本继续递增，回滚本身还能再回滚。
 */
function PostVersionsDialog({ post, onClose }: { post: SitePost | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const rollback = useRollbackPost()
  const { data: versions = [], isLoading } = usePostVersions(post?.id)
  const [detail, setDetail] = useState<PostVersionDetail | null>(null)
  const [loadingVer, setLoadingVer] = useState<number | null>(null)

  const onPreview = async (version: number) => {
    if (!post) return
    if (detail?.version === version) {
      setDetail(null)
      return
    }
    setLoadingVer(version)
    try {
      setDetail(await fetchPostVersion(post.id, version))
    } catch {
      // 服务端原因由 http 拦截器统一弹出
    } finally {
      setLoadingVer(null)
    }
  }

  const onRollback = async (version: number) => {
    if (!post) return
    if (!(await confirm({ destructive: true, description: t('plugin_website.post.rollbackConfirm', { version }) }))) return
    try {
      await rollback.mutateAsync({ id: post.id, version })
      setDetail(null)
      toast.success(t('plugin_website.post.rollbackDone', { version }))
    } catch {
      // 同上：拦截器已经把服务端原因说过了
    }
  }

  return (
    <ResponsiveDialog
      open={!!post}
      onOpenChange={(o) => !o && onClose()}
      title={`${t('plugin_website.post.versions')} · ${post?.title ?? ''}`}
      description={t('plugin_website.post.versionsHint', { max: MAX_POST_VERSIONS })}
      size="lg"
      footer={
        <Button variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {isLoading ? <p className="text-sm text-muted-foreground">{t('common.loading')}</p> : null}
      {!isLoading && versions.length === 0 ? (
        <EmptyState title={t('plugin_website.post.versionsEmpty')} />
      ) : null}
      <ul className="flex flex-col divide-y">
        {versions.map((v) => (
          <li key={v.version} className="flex flex-col gap-1 py-2 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <span className="num w-12 shrink-0 font-medium">v{v.version}</span>
              <span className="min-w-0 flex-1 truncate text-muted-foreground">{v.title}</span>
              {v.createdBy ? <span className="max-w-32 truncate text-xs text-muted-foreground">{v.createdBy}</span> : null}
              <span className="num text-xs text-muted-foreground">{formatTime(v.createdAt, i18n.language)}</span>
              <Button variant="ghost" size="sm" disabled={loadingVer === v.version} onClick={() => void onPreview(v.version)}>
                {detail?.version === v.version
                  ? t('plugin_website.post.hideVersion')
                  : loadingVer === v.version
                    ? t('common.loading')
                    : t('plugin_website.post.previewVersion')}
              </Button>
              <Button variant="outline" size="sm" disabled={rollback.isPending} onClick={() => void onRollback(v.version)}>
                {t('plugin_website.post.rollbackVersion')}
              </Button>
            </div>
            {detail?.version === v.version ? (
              <div className="rounded-md border border-border bg-muted/40 p-3 text-xs">
                <p className="font-medium text-foreground">{detail.title}</p>
                {detail.summary ? <p className="mt-1 text-muted-foreground">{detail.summary}</p> : null}
                <p className="num mt-1 text-muted-foreground">{t('plugin_website.post.versionWords', { count: detail.words })}</p>
              </div>
            ) : null}
          </li>
        ))}
      </ul>
    </ResponsiveDialog>
  )
}

export default function PostPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  // 预取字段 schema：点「新建/编辑」时直接命中缓存，不再先闪一个加载弹窗。
  useI18nSchema('post')
  const { data: languages = [] } = useLanguages()
  const { data: categories = [] } = useCategories()
  const [draft, setDraft] = useState<ContentQuery>({})
  const [filters, setFilters] = useState<ContentQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])
  const { data, isLoading } = usePosts({ ...filters, page, pageSize })
  const extraLocales = languages.filter((l) => !l.default)
  const remove = useDeletePost()
  const removeBatch = useDeletePosts()
  const setStatus = useSetPostStatus()
  const copy = useCopyPost()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SitePost | null>(null)
  const [catOpen, setCatOpen] = useState(false)
  // 版本历史弹窗挂在被点的那一行上；null = 关闭。
  const [versionsOf, setVersionsOf] = useState<SitePost | null>(null)
  const [exporting, setExporting] = useState(false)
  // 点译文徽标直接定位到那门语言；nonce 让「重复点同一个语言」也能重新触发定位。
  const [focus, setFocus] = useState<{ code: string; nonce: number } | null>(null)

  const openAt = (row: SitePost | null, code?: string) => {
    setEditing(row)
    setFocus(code ? { code, nonce: Date.now() } : null)
    setFormOpen(true)
  }

  const onDelete = async (rows: SitePost[]) => {
    if (
      await confirm({
        destructive: true,
        description: rows.length === 1
          ? t('plugin_website.post.deleteConfirm', { name: rows[0].title })
          : t('plugin_website.post.deleteBatchConfirm', { count: rows.length }),
      })
    ) {
      try {
        if (rows.length === 1) await remove.mutateAsync(rows[0].id)
        else await removeBatch.mutateAsync(rows.map((r) => r.id))
        setSelected([])
        toast.success(t('common.operateSuccess'))
      } catch {
        // 同上：拦截器已经把服务端原因说过了
      }
    }
  }

  // 批量发布/下线后要清选择：列表一刷新，选中的行可能已经不在了。
  const applyStatus = async (ids: string[], status: 'published' | 'draft') => {
    try {
      await setStatus.mutateAsync({ ids, status })
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  const onPreview = async (row: SitePost) => {
    try {
      const url = await fetchPostPreview(row.id)
      window.open(url, '_blank', 'noopener')
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  const onCopy = async (row: SitePost) => {
    try {
      const fresh = await copy.mutateAsync(row.id)
      toast.success(t('plugin_website.post.copied', { slug: fresh.slug }))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    }
  }

  // 导出沿用当前筛选（不是当前页）：后端按同一套条件取全量，前端生成带 BOM 的 CSV。
  const onExport = async () => {
    setExporting(true)
    try {
      const rows = await fetchPostsForExport({ keyword: filters.keyword, status: filters.status, categoryId: filters.categoryId })
      const stamp = new Date().toISOString().slice(0, 10).replace(/-/g, '')
      downloadCsv(`posts-${stamp}.csv`, [
        [
          t('plugin_website.fields.postTitle'),
          t('plugin_website.fields.slug'),
          t('plugin_website.post.categoryColumn'),
          t('plugin_website.fields.published'),
          t('plugin_website.fields.publishedAt'),
          t('plugin_website.fields.updatedAt'),
        ],
        ...rows.map((r) => [
          r.title,
          r.slug,
          (r.categories ?? []).map((c) => c.title).join('、'),
          isScheduled(r) ? t('plugin_website.post.scheduled') : r.published ? t('plugin_website.publishedOn') : t('plugin_website.publishedOff'),
          r.publishedAt ? formatTime(r.publishedAt, i18n.language) : '',
          formatTime(r.updatedAt, i18n.language),
        ]),
      ])
      toast.success(t('common.operateSuccess'))
    } catch {
      // 服务端原因由 http 拦截器统一弹出，这里再补一条「操作失败」就是同一件事说两遍
    } finally {
      setExporting(false)
    }
  }

  const selectedRows = (data?.list ?? []).filter((r) => selected.includes(r.id))

  const columns: Column<SitePost>[] = [
    {
      key: 'title',
      title: t('plugin_website.fields.postTitle'),
      mobile: 'title',
      render: (r) => (
        <span className="flex flex-wrap items-center gap-1.5 font-medium text-foreground">
          {r.pinned ? <Badge variant="info">{t('plugin_website.post.pinnedTag')}</Badge> : null}
          {r.title}
        </span>
      ),
    },
    { key: 'slug', title: t('plugin_website.fields.slug'), mobile: 'sub', render: (r) => <span className="font-mono text-xs">/news/{r.slug}</span> },
    {
      key: 'categories',
      title: t('plugin_website.post.categoryColumn'),
      render: (r) => {
        const list = r.categories ?? []
        if (!list.length) return <span className="text-muted-foreground">—</span>
        return (
          <div className="flex flex-wrap gap-1">
            {list.map((c) => (
              <Badge key={c.id} variant="outline">{c.title}</Badge>
            ))}
          </div>
        )
      },
    },
    {
      key: 'tags',
      title: t('plugin_website.post.tags'),
      // 标签自由生长（上限 20 个），手机卡片里两列徽标会把主标题挤没，只在桌面给出
      mobile: 'hidden',
      render: (r) => {
        const list = r.tags ?? []
        if (!list.length) return <span className="text-muted-foreground">—</span>
        return (
          <div className="flex flex-wrap gap-1">
            {list.map((x) => (
              <Badge key={x.id} variant="outline">{x.name}</Badge>
            ))}
          </div>
        )
      },
    },
    {
      key: 'i18n',
      title: t('plugin_website.i18n.status'),
      render: (r) => <TranslationStatus value={r.i18nJson} fields={['title', 'summary', 'seoTitle', 'seoDesc', 'content']} onFocus={(code) => openAt(r, code)} />,
    },
    {
      key: 'views',
      title: t('plugin_website.fields.views'),
      align: 'end',
      render: (r) => <span className="num text-muted-foreground">{r.views}</span>,
    },
    {
      key: 'published',
      title: t('plugin_website.fields.published'),
      render: (r) => (
        <div className="flex flex-col items-start gap-1">
          {statusBadge(r, t)}
          {isScheduled(r) ? <span className="text-xs text-muted-foreground">{t('plugin_website.post.scheduledHint')}</span> : null}
        </div>
      ),
    },
    {
      key: 'link',
      title: '',
      align: 'end',
      render: (r) => (r.published && !isScheduled(r) ? (
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
        onSearch={() => { setFilters(draft); setPage(1); setSelected([]) }}
        onReset={() => { setDraft({}); setFilters({}); setPage(1); setSelected([]) }}
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
            <SelectItem value={ALL}>{t('plugin_website.filter.allStatuses')}</SelectItem>
            <SelectItem value="published">{t('plugin_website.publishedOn')}</SelectItem>
            <SelectItem value="scheduled">{t('plugin_website.post.scheduled')}</SelectItem>
            <SelectItem value="draft">{t('plugin_website.publishedOff')}</SelectItem>
          </SelectContent>
        </Select>
        {categories.length ? (
          <Select value={draft.categoryId ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, categoryId: v === ALL ? undefined : v }))}>
            <SelectTrigger className="md:w-40" aria-label={t('plugin_website.post.categoryFilter')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('plugin_website.filter.allCategories')}</SelectItem>
              {categories.map((c) => (
                <SelectItem key={c.id} value={c.id}>{c.title}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        {extraLocales.length ? (
          <Select value={draft.missing ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, missing: v === ALL ? undefined : v }))}>
            <SelectTrigger className="md:w-48" aria-label={t('plugin_website.i18n.filterLabel')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t('plugin_website.filter.allLanguages')}</SelectItem>
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
            <Button variant="outline" onClick={() => setCatOpen(true)}>{t('plugin_website.post.manageCategories')}</Button>
            <Button variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download className="me-2 size-4" aria-hidden="true" />
              {t('common.export')}
            </Button>
            <Button onClick={() => openAt(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_website.post.create')}
            </Button>
          </Can>
        }
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="website:post">
            <Button variant="outline" size="sm" onClick={() => void applyStatus(ids, 'published')}>
              {t('plugin_website.post.publishBatch')}
            </Button>
            <Button variant="outline" size="sm" onClick={() => void applyStatus(ids, 'draft')}>
              {t('plugin_website.post.offlineBatch')}
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void onDelete(selectedRows)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <Can perm="website:post">
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.post.preview')} onClick={() => void onPreview(row)}>
              <Eye aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.post.copy')} onClick={() => void onCopy(row)}>
              <Copy aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openAt(row)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('plugin_website.post.versions')} onClick={() => setVersionsOf(row)}>
              <History aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete([row])}>
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
          onManageCategories={() => setCatOpen(true)}
        />
      )}
      {catOpen && <TaxonomyManager onOpenChange={setCatOpen} />}
      {/* 条件挂载：每次打开都是新状态，上一篇的正文预览不会串到下一篇 */}
      {versionsOf && <PostVersionsDialog post={versionsOf} onClose={() => setVersionsOf(null)} />}
    </PageContainer>
  )
}
