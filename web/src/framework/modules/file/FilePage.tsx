import { useState } from 'react'
import { ChevronLeft, ChevronRight, FolderInput, LayoutGrid, List, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { EmptyState } from '@/framework/components/EmptyState'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { formatSize } from '@/framework/upload/precheck'
import { cn } from '@/lib/utils'
import { FileActionsMenu, FilePreviewDialog, MoveDialog } from './FileActions'
import { FileThumb } from './FileThumb'
import { GROUP_ALL, GroupPanel } from './GroupPanel'
import { UploadDialog } from './UploadDialog'
import { useDeleteFiles, useFileList, useMoveFiles, useUpdateFile, type FileItem } from './api'

const PAGE_SIZE_GRID = 24
const CATEGORIES = ['image', 'video', 'doc', 'other'] as const

interface Filters {
  keyword: string
  category: string
  range: DateRangeValue
}
const emptyFilters: Filters = { keyword: '', category: 'all', range: {} }

export default function FilePage() {
  const { t, i18n } = useTranslation()
  const { has } = usePermission()
  const confirm = useConfirm()
  const [view, setView] = useState<'grid' | 'list'>('grid')
  const [group, setGroup] = useState(GROUP_ALL)
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [listSize, setListSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])
  const [uploadOpen, setUploadOpen] = useState(false)
  const [preview, setPreview] = useState<FileItem | null>(null)
  const [moving, setMoving] = useState<string[] | null>(null)

  const pageSize = view === 'grid' ? PAGE_SIZE_GRID : listSize
  const { data, isLoading } = useFileList({
    page,
    pageSize,
    keyword: applied.keyword || undefined,
    category: applied.category === 'all' ? undefined : applied.category,
    groupId: group === GROUP_ALL ? undefined : group,
    from: applied.range.from,
    to: applied.range.to,
  })
  const update = useUpdateFile()
  const move = useMoveFiles()
  const del = useDeleteFiles()

  const rows = data?.list ?? []
  const total = data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const canManage = has('file:upload')
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false })

  const toggleVisibility = async (f: FileItem) => {
    try {
      await update.mutateAsync({ id: f.id, patch: { visibility: f.visibility === 'public' ? 'private' : 'public' } })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const remove = async (ids: string[]) => {
    if (!(await confirm({ description: t('fileMgr.deleteConfirm', { count: ids.length }), destructive: true }))) return
    try {
      await del.mutateAsync(ids)
      setSelected((s) => s.filter((x) => !ids.includes(x)))
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const doMove = async (groupId: string) => {
    if (!moving) return
    try {
      await move.mutateAsync({ ids: moving, groupId })
      toast.success(t('fileMgr.moveSuccess'))
      setSelected([])
      setMoving(null)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const search = () => {
    setApplied(draft)
    setPage(1)
    setSelected([])
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
    setSelected([])
  }

  const visBadge = (f: FileItem) => (
    <Badge variant={f.visibility === 'public' ? 'success' : 'outline'}>{t(f.visibility === 'public' ? 'fileMgr.public' : 'fileMgr.private')}</Badge>
  )

  const actions = (f: FileItem) => (
    <FileActionsMenu file={f} onPreview={setPreview} onToggleVisibility={(x) => void toggleVisibility(x)} onMove={(x) => setMoving([x.id])} onDelete={(x) => void remove([x.id])} />
  )

  const columns: Column<FileItem>[] = [
    {
      key: 'name',
      title: t('fileMgr.name'),
      mobile: 'title',
      render: (f) => (
        <div className="flex min-w-0 items-center gap-3">
          <div className="size-10 shrink-0 overflow-hidden rounded-md border border-border">
            <FileThumb file={f} />
          </div>
          <span className="min-w-0 truncate font-medium">{f.name}</span>
        </div>
      ),
    },
    { key: 'category', title: t('fileMgr.filterType'), mobile: 'sub', render: (f) => t(`fileMgr.kind.${f.category}`) },
    { key: 'size', title: t('fileMgr.size'), align: 'end', render: (f) => <span className="num">{formatSize(f.size)}</span> },
    { key: 'visibility', title: t('fileMgr.visibility'), render: visBadge },
    { key: 'uploader', title: t('fileMgr.uploader') },
    { key: 'createdAt', title: t('fileMgr.createdAt'), render: (f) => <span className="num">{fmtTime(f.createdAt)}</span> },
  ]

  const batchBar = selected.length > 0 && (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-muted/50 px-3 py-2">
      <span className="text-sm">{t('fileMgr.selectedCount', { count: selected.length })}</span>
      {canManage && (
        <Button variant="outline" size="sm" className="h-11 md:h-8" onClick={() => setMoving(selected)}>
          <FolderInput className="me-1 size-4" aria-hidden="true" />
          {t('fileMgr.moveSelected')}
        </Button>
      )}
      <Can perm="file:delete">
        <Button variant="destructive" size="sm" className="h-11 md:h-8" onClick={() => void remove(selected)}>
          <Trash2 className="me-1 size-4" aria-hidden="true" />
          {t('fileMgr.deleteSelected')}
        </Button>
      </Can>
      <Button variant="ghost" size="sm" className="h-11 md:h-8" onClick={() => setSelected([])}>
        {t('common.clear')}
      </Button>
    </div>
  )

  return (
    <PageContainer>
      <PageHeader
        title={t('fileMgr.title')}
        description={t('fileMgr.description')}
        actions={
          <>
            <div role="group" aria-label={t('fileMgr.viewGrid')} className="flex rounded-md border border-border p-0.5">
              {(['grid', 'list'] as const).map((v) => (
                <Button
                  key={v}
                  variant={view === v ? 'secondary' : 'ghost'}
                  size="icon"
                  aria-pressed={view === v}
                  aria-label={t(v === 'grid' ? 'fileMgr.viewGrid' : 'fileMgr.viewList')}
                  className="size-11 md:size-8"
                  onClick={() => {
                    setView(v)
                    setPage(1)
                  }}
                >
                  {v === 'grid' ? <LayoutGrid className="size-4" aria-hidden="true" /> : <List className="size-4" aria-hidden="true" />}
                </Button>
              ))}
            </div>
            <Can perm="file:upload">
              <Button onClick={() => setUploadOpen(true)}>
                <Upload className="me-2 size-4" aria-hidden="true" />
                {t('fileMgr.upload')}
              </Button>
            </Can>
          </>
        }
      />

      <div className="flex flex-col gap-4 md:flex-row md:items-start">
        <Card className="p-3 md:w-52 md:shrink-0 lg:w-60">
          <GroupPanel
            value={group}
            onChange={(g) => {
              setGroup(g)
              setPage(1)
              setSelected([])
            }}
          />
        </Card>

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <FilterBar onSearch={search} onReset={reset}>
            <div className="flex flex-col gap-1.5 md:w-44">
              <Label>{t('common.keyword')}</Label>
              <Input value={draft.keyword} placeholder={t('fileMgr.keywordPlaceholder')} className="text-base md:text-sm" onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))} />
            </div>
            <div className="flex flex-col gap-1.5 md:w-40">
              <Label>{t('fileMgr.filterType')}</Label>
              <Select value={draft.category} onValueChange={(v) => setDraft((d) => ({ ...d, category: v }))}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{t('fileMgr.allTypes')}</SelectItem>
                  {CATEGORIES.map((c) => (
                    <SelectItem key={c} value={c}>
                      {t(`fileMgr.kind.${c}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5 md:w-64">
              <Label>{t('fileMgr.timeRange')}</Label>
              <DateRangePicker value={draft.range} onChange={(range) => setDraft((d) => ({ ...d, range }))} />
            </div>
          </FilterBar>

          {batchBar}

          {view === 'list' ? (
            <DataTable
              columns={columns}
              columnSettingsKey="file"
              data={rows}
              rowKey="id"
              loading={isLoading}
              selection={{ selected, onChange: setSelected }}
              rowActions={actions}
              onRowClick={setPreview}
              pagination={{
                page,
                pageSize,
                total,
                onChange: (p, s) => {
                  setPage(p)
                  setListSize(s)
                },
              }}
            />
          ) : isLoading ? (
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
              {Array.from({ length: 8 }, (_, i) => (
                <Skeleton key={i} className="aspect-[4/5] w-full" />
              ))}
            </div>
          ) : rows.length === 0 ? (
            <EmptyState title={t('fileMgr.empty')} />
          ) : (
            <>
              <ul className="grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
                {rows.map((f) => {
                  const checked = selected.includes(f.id)
                  return (
                    <li key={f.id}>
                      <Card className={cn('group overflow-hidden transition-shadow hover:glow motion-reduce:transition-none', checked && 'ring-2 ring-primary')}>
                        <div className="relative aspect-square overflow-hidden border-b border-border">
                          <button type="button" aria-label={`${t('common.preview')} ${f.name}`} className="block size-full" onClick={() => setPreview(f)}>
                            <FileThumb file={f} />
                          </button>
                          <div className="absolute start-1 top-1 flex size-11 items-center justify-center md:size-8">
                            <Checkbox
                              checked={checked}
                              aria-label={t('fileMgr.selectFile', { name: f.name })}
                              className="bg-background"
                              onCheckedChange={() => setSelected((s) => (checked ? s.filter((x) => x !== f.id) : [...s, f.id]))}
                            />
                          </div>
                          <div className="absolute end-1 top-1">{visBadge(f)}</div>
                        </div>
                        <div className="flex items-start gap-1 p-2 ps-3">
                          <div className="min-w-0 flex-1 py-1">
                            <p className="truncate text-sm font-medium" title={f.name}>
                              {f.name}
                            </p>
                            <p className="num truncate text-xs text-muted-foreground">
                              {formatSize(f.size)} · {f.uploader}
                            </p>
                            <p className="num truncate text-xs text-muted-foreground">{fmtTime(f.createdAt)}</p>
                          </div>
                          {actions(f)}
                        </div>
                      </Card>
                    </li>
                  )
                })}
              </ul>
              <div className="flex items-center justify-between gap-2">
                <span className="num text-sm text-muted-foreground">{t('fileMgr.pageInfo', { page, pages, total })}</span>
                <div className="flex gap-2">
                  <Button variant="outline" size="icon" className="size-11 md:size-9" aria-label={t('common.prev')} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                    <ChevronLeft className="size-4 rtl:rotate-180" aria-hidden="true" />
                  </Button>
                  <Button variant="outline" size="icon" className="size-11 md:size-9" aria-label={t('common.next')} disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>
                    <ChevronRight className="size-4 rtl:rotate-180" aria-hidden="true" />
                  </Button>
                </div>
              </div>
            </>
          )}
        </div>
      </div>

      <UploadDialog open={uploadOpen} onOpenChange={setUploadOpen} defaultGroupId={group === GROUP_ALL || group === 'none' ? '' : group} />
      <FilePreviewDialog file={preview} onClose={() => setPreview(null)} />
      <MoveDialog key={moving?.join(',') ?? 'closed'} open={!!moving} onOpenChange={(o) => !o && setMoving(null)} count={moving?.length ?? 0} onConfirm={(g) => void doMove(g)} />
    </PageContainer>
  )
}
