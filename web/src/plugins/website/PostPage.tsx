import { useState } from 'react'
import { ExternalLink, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { RichTextEditor } from '@/framework/components/RichTextEditor'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { Textarea } from '@/framework/ui/textarea'
import TranslationsEditor from './TranslationsEditor'
import { useDeletePost, usePosts, useSavePost, type ContentQuery, type SitePost } from './api'

const ALL = '__all'

function PostFormDialog({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: SitePost | null
}) {
  const { t } = useTranslation()
  const save = useSavePost()
  const [slug, setSlug] = useState(editing?.slug ?? '')
  const [title, setTitle] = useState(editing?.title ?? '')
  const [coverUrl, setCoverUrl] = useState(editing?.coverUrl ?? '')
  const [summary, setSummary] = useState(editing?.summary ?? '')
  const [content, setContent] = useState(editing?.content ?? '')
  const [published, setPublished] = useState(editing?.published ?? false)
  const [i18nJson, setI18nJson] = useState(editing?.i18nJson ?? '')

  const submit = async () => {
    if (!title.trim() || !slug.trim()) {
      toast.error(t('plugin_website.post.required'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data: { slug, title, coverUrl, summary, content, published, i18nJson } })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{editing ? t('plugin_website.post.edit') : t('plugin_website.post.create')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm">
              {t('plugin_website.fields.postTitle')}
              <Input value={title} onChange={(e) => setTitle(e.target.value)} />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              {t('plugin_website.fields.slug')}
              <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="hello-nexadmin" className="font-mono" />
              <span className="text-xs text-muted-foreground">/news/{slug || '…'}</span>
            </label>
          </div>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.coverUrl')}
            <Input value={coverUrl} onChange={(e) => setCoverUrl(e.target.value)} />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.summary')}
            <Textarea rows={2} value={summary} onChange={(e) => setSummary(e.target.value)} />
          </label>
          <div className="flex min-h-40 flex-col gap-1 text-sm">
            {t('plugin_website.fields.content')}
            <RichTextEditor value={content} onChange={setContent} />
          </div>
          <TranslationsEditor
            value={i18nJson}
            onChange={setI18nJson}
            fields={[
              { key: 'title', label: t('plugin_website.fields.postTitle') },
              { key: 'summary', label: t('plugin_website.fields.summary'), multiline: true },
              { key: 'content', label: t('plugin_website.fields.content'), rich: true },
            ]}
          />
          <label className="flex items-center justify-between text-sm">
            {t('plugin_website.fields.published')}
            <Switch checked={published} onCheckedChange={setPublished} />
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default function PostPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<ContentQuery>({})
  const [filters, setFilters] = useState<ContentQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = usePosts({ ...filters, page, pageSize })
  const remove = useDeletePost()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SitePost | null>(null)

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
    { key: 'published', title: t('plugin_website.fields.published'), render: (r) => (r.published ? <Badge variant="success">{t('plugin_website.publishedOn')}</Badge> : <Badge variant="outline">{t('plugin_website.publishedOff')}</Badge>) },
    {
      key: 'link',
      title: '',
      align: 'end',
      render: (r) => (r.published ? (
        <a
          className="inline-flex text-muted-foreground transition-colors hover:text-foreground"
          href={`/news/${r.slug}`}
          target="_blank"
          rel="noreferrer"
          aria-label={t('plugin_website.viewSite')}
        >
          <ExternalLink aria-hidden="true" size={15} />
        </a>
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
            <SelectItem value={ALL}>{t('plugin_website.fields.published')}: {t('plugin_website.all')}</SelectItem>
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
            <Button onClick={() => { setEditing(null); setFormOpen(true) }}>
              <Plus aria-hidden="true" />
              {t('plugin_website.post.create')}
            </Button>
          </Can>
        }
        rowActions={(row) => (
          <Can perm="website:post">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => { setEditing(row); setFormOpen(true) }}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(row)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {formOpen && <PostFormDialog key={editing?.id ?? 'new'} onOpenChange={setFormOpen} editing={editing} />}
    </PageContainer>
  )
}
