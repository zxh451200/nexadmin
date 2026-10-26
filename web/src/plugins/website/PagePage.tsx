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
import TranslationsEditor from './TranslationsEditor'
import { useDeletePage, usePages, useSavePage, type ContentQuery, type SitePage } from './api'

const ALL = '__all'

function PageFormDialog({
  onOpenChange, editing,
}: {
  onOpenChange: (v: boolean) => void
  editing: SitePage | null
}) {
  const { t } = useTranslation()
  const save = useSavePage()
  const [slug, setSlug] = useState(editing?.slug ?? '')
  const [title, setTitle] = useState(editing?.title ?? '')
  const [seoDesc, setSeoDesc] = useState(editing?.seoDesc ?? '')
  const [content, setContent] = useState(editing?.content ?? '')
  const [published, setPublished] = useState(editing?.published ?? false)
  const [i18nJson, setI18nJson] = useState(editing?.i18nJson ?? '')

  const submit = async () => {
    if (!title.trim() || !slug.trim()) {
      toast.error(t('plugin_website.page.required'))
      return
    }
    try {
      await save.mutateAsync({ id: editing?.id, data: { slug, title, seoDesc, content, published, i18nJson } })
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
          <DialogTitle>{editing ? t('plugin_website.page.edit') : t('plugin_website.page.create')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="flex flex-col gap-1 text-sm">
              {t('plugin_website.fields.pageTitle')}
              <Input value={title} onChange={(e) => setTitle(e.target.value)} />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              {t('plugin_website.fields.slug')}
              <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="about" className="font-mono" />
              <span className="text-xs text-muted-foreground">/p/{slug || '…'}</span>
            </label>
          </div>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_website.fields.seoDesc')}
            <Input value={seoDesc} onChange={(e) => setSeoDesc(e.target.value)} />
          </label>
          <div className="flex min-h-40 flex-col gap-1 text-sm">
            {t('plugin_website.fields.content')}
            <RichTextEditor value={content} onChange={setContent} />
          </div>
          <TranslationsEditor
            value={i18nJson}
            onChange={setI18nJson}
            fields={[
              { key: 'title', label: t('plugin_website.fields.pageTitle') },
              { key: 'seoDesc', label: t('plugin_website.fields.seoDesc') },
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

export default function PagePage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
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
    { key: 'published', title: t('plugin_website.fields.published'), render: (r) => (r.published ? <Badge variant="success">{t('plugin_website.publishedOn')}</Badge> : <Badge variant="outline">{t('plugin_website.publishedOff')}</Badge>) },
    {
      key: 'link',
      title: '',
      align: 'end',
      render: (r) => (r.published ? (
        <a
          className="inline-flex text-muted-foreground transition-colors hover:text-foreground"
          href={`/p/${r.slug}`}
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
