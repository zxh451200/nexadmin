import { useState } from 'react'
import { ChevronLeft, ChevronRight, Download, FolderInput, LayoutGrid, List, Lock, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { pageAfterDelete } from '@/framework/hooks/page-fallback'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { EmptyState } from '@/framework/components/EmptyState'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Segmented } from '@/framework/components/Segmented'
import { BatchBar } from '@/framework/components/table/BatchBar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Skeleton } from '@/framework/ui/skeleton'
import { formatSize } from '@/framework/upload/precheck'
import { cn } from '@/lib/utils'
import { FileActionsMenu, FilePreviewDialog, MoveDialog, RenameDialog } from './FileActions'
import { FileThumb } from './FileThumb'
import { GROUP_ALL, GroupPanel } from './GroupPanel'
import { UploadDialog } from './UploadDialog'
import { downloadFilesZip, useDeleteFiles, useFileList, useMoveFiles, useUpdateFile, type FileItem, type FileSort } from './api'

const PAGE_SIZE_GRID = 24
const CATEGORIES = ['image', 'video', 'doc', 'other'] as const
/** 卡片最小 190px 左右；再往上加列会让文件名和三行元信息全被截断 */
const GRID_CLASS = 'grid grid-cols-2 gap-3 md:gap-4 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5'

interface Filters {
  keyword: string
  category: string
  range: DateRangeValue
  sort: FileSort
  desc: boolean
}
const emptyFilters: Filters = { keyword: '', category: 'all', range: {}, sort: 'created_at', desc: true }

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
  const [renaming, setRenaming] = useState<FileItem | null>(null)

  const pageSize = view === 'grid' ? PAGE_SIZE_GRID : listSize
  const { data, isLoading } = useFileList({
    page,
    pageSize,
    keyword: applied.keyword || undefined,
    category: applied.category === 'all' ? undefined : applied.category,
    groupId: group === GROUP_ALL ? undefined : group,
    from: applied.range.from,
    to: applied.range.to,
    sort: applied.sort,
    desc: applied.desc,
  })
  const update = useUpdateFile()
  const move = useMoveFiles()
  const del = useDeleteFiles()

  const rows = data?.list ?? []
  const total = data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / pageSize))
  const canManage = has('file:upload')
  const fmtTime = (s: string) => new Date(s).toLocaleString(i18n.language, { hour12: false })
  const fmtDate = (s: string) => new Date(s).toLocaleDateString(i18n.language)

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
      const removed = rows.filter((r) => ids.includes(r.id)).length
      await del.mutateAsync(ids)
      setPage(pageAfterDelete(page, rows.length, removed))
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

  const actions = (f: FileItem, triggerClassName?: string) => (
    <FileActionsMenu
      file={f}
      triggerClassName={triggerClassName}
      onPreview={setPreview}
      onToggleVisibility={(x) => void toggleVisibility(x)}
      onMove={(x) => setMoving([x.id])}
      onRename={setRenaming}
      onDelete={(x) => void remove([x.id])}
    />
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

  const batchActions = (ids: string[]) => (
    <>
      <Button variant="outline" size="sm" className="h-11 md:h-8" onClick={() => void downloadFilesZip(ids)}>
        <Download className="me-1 size-4" aria-hidden="true" />
        {t('fileMgr.zipSelected')}
      </Button>
      {canManage && (
        <Button variant="outline" size="sm" className="h-11 md:h-8" onClick={() => setMoving(ids)}>
          <FolderInput className="me-1 size-4" aria-hidden="true" />
          {t('fileMgr.moveSelected')}
        </Button>
      )}
      <Can perm="file:delete">
        <Button variant="destructive" size="sm" className="h-11 md:h-8" onClick={() => void remove(ids)}>
          <Trash2 className="me-1 size-4" aria-hidden="true" />
          {t('fileMgr.deleteSelected')}
        </Button>
      </Can>
    </>
  )

  const filterBar = (
    <FilterBar onSearch={search} onReset={reset}>
      <Input value={draft.keyword} placeholder={t('fileMgr.keywordPlaceholder')} className="text-base md:min-w-40 md:flex-1 md:text-sm" onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))} aria-label={t('common.keyword')} />
      <Select value={draft.category} onValueChange={(v) => setDraft((d) => ({ ...d, category: v }))}>
        <SelectTrigger className="md:w-36 md:shrink-0" aria-label={t('fileMgr.filterType')}>
          <SelectValue placeholder={t('fileMgr.filterType')} />
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
      <DateRangePicker value={draft.range} onChange={(range) => setDraft((d) => ({ ...d, range }))} ariaLabel={t('fileMgr.timeRange')} className="md:w-52 md:shrink-0" placeholder={t('fileMgr.timeRange')} />
    </FilterBar>
  )

  const viewToggle = () => (
    <Segmented
      ariaLabel={t('fileMgr.viewGrid')}
      value={view}
      onChange={(v) => {
        setView(v as 'grid' | 'list')
        setPage(1)
      }}
      options={[
        {
          value: 'grid',
          label: <LayoutGrid className="size-4" aria-hidden="true" />,
          ariaLabel: t('fileMgr.viewGrid'),
          className: 'size-11 px-0 md:size-8',
        },
        {
          value: 'list',
          label: <List className="size-4" aria-hidden="true" />,
          ariaLabel: t('fileMgr.viewList'),
          className: 'size-11 px-0 md:size-8',
        },
      ]}
    />
  )

  // 排序属于「结果怎么展示」，不进筛选表单；改了就立即重排，不必再点搜索
  const sortSelect = (
    <Select
      value={`${draft.sort}:${draft.desc ? 'desc' : 'asc'}`}
      onValueChange={(v) => {
        const [sort, dir] = v.split(':')
        const next = { sort: sort as FileSort, desc: dir === 'desc' }
        setDraft((d) => ({ ...d, ...next }))
        setApplied((a) => ({ ...a, ...next }))
        setPage(1)
        setSelected([])
      }}
    >
      <SelectTrigger className="w-40 shrink-0" aria-label={t('fileMgr.sortBy')}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="created_at:desc">{t('fileMgr.sortNewest')}</SelectItem>
        <SelectItem value="created_at:asc">{t('fileMgr.sortOldest')}</SelectItem>
        <SelectItem value="name:asc">{t('fileMgr.sortNameAsc')}</SelectItem>
        <SelectItem value="size:desc">{t('fileMgr.sortSizeDesc')}</SelectItem>
        <SelectItem value="size:asc">{t('fileMgr.sortSizeAsc')}</SelectItem>
      </SelectContent>
    </Select>
  )

  const resultBar = (
    <>
      <span className="num text-sm text-muted-foreground">{t('common.total', { count: total })}</span>
      <div className="ms-auto flex items-center gap-2">
        {sortSelect}
        {viewToggle()}
      </div>
    </>
  )

  const uploadBtn = (
    <Can perm="file:upload">
      <Button onClick={() => setUploadOpen(true)}>
        <Upload className="me-2 size-4" aria-hidden="true" />
        {t('fileMgr.upload')}
      </Button>
    </Can>
  )

  return (
    <PageContainer>
      <PageHeader title={t('fileMgr.title')} description={t('fileMgr.description')} actions={uploadBtn} />

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
          {filterBar}

          {view === 'list' ? (
            <DataTable
              toolbar={resultBar}
              columns={columns}
              columnSettingsKey="file"
              data={rows}
              rowKey="id"
              loading={isLoading}
              selection={{ selected, onChange: setSelected }}
              batchActions={batchActions}
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
          ) : (
            <Card className="overflow-hidden rounded-xl">
              {/* 与列表视图同构：一张卡片 = 表头（计数+视图切换）/ 内容 / 页脚，页面不再散着三段裸元素 */}
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">{resultBar}</div>

              <div className="p-3">
                {isLoading ? (
                  <div className={GRID_CLASS}>
                    {Array.from({ length: 10 }, (_, i) => (
                      <Skeleton key={i} className="aspect-4/3 w-full rounded-lg" />
                    ))}
                  </div>
                ) : rows.length === 0 ? (
                  <EmptyState title={t('fileMgr.empty')} />
                ) : (
                  <ul className={GRID_CLASS}>
                    {rows.map((f) => {
                      const checked = selected.includes(f.id)
                      return (
                        <li key={f.id}>
                          <Card className={cn('group h-full overflow-hidden transition-shadow hover:shadow-md motion-reduce:transition-none', checked && 'ring-2 ring-primary')}>
                            <div className="relative aspect-4/3 overflow-hidden border-b border-border">
                              <button type="button" aria-label={`${t('common.preview')} ${f.name}`} className="block size-full" onClick={() => setPreview(f)}>
                                <FileThumb file={f} />
                              </button>
                              <div className="absolute inset-s-1 top-1 flex size-11 items-center justify-center md:size-8">
                                <Checkbox
                                  checked={checked}
                                  aria-label={t('fileMgr.selectFile', { name: f.name })}
                                  className="border-border bg-background shadow-xs"
                                  onCheckedChange={() => setSelected((s) => (checked ? s.filter((x) => x !== f.id) : [...s, f.id]))}
                                />
                              </div>
                              {f.visibility === 'private' && (
                                <Badge variant="outline" className="absolute inset-e-1.5 top-1.5 gap-1 border-border bg-background/90 px-1.5 py-0.5 text-[11px] font-medium shadow-xs backdrop-blur-xs">
                                  <Lock className="size-3" aria-hidden="true" />
                                  {t('fileMgr.private')}
                                </Badge>
                              )}
                              <div className="absolute inset-e-1 bottom-1 transition-opacity motion-reduce:transition-none md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100">
                                {actions(f, 'bg-background/90 shadow-xs backdrop-blur-xs hover:bg-background')}
                              </div>
                            </div>
                            <div className="min-w-0 p-2.5">
                              <p className="truncate text-sm font-medium" title={f.name}>
                                {f.name}
                              </p>
                              <p
                                className="mt-0.5 truncate text-xs text-muted-foreground tabular-nums"
                                title={`${formatSize(f.size)} · ${f.uploader} · ${fmtTime(f.createdAt)}`}
                              >
                                {formatSize(f.size)} · {fmtDate(f.createdAt)}
                              </p>
                            </div>
                          </Card>
                        </li>
                      )
                    })}
                  </ul>
                )}
              </div>

              {/* 只有一页时不占一行页脚；总数已在表头，页脚只说页码 */}
              {pages > 1 ? (
                <div className="flex items-center justify-between gap-2 border-t border-border px-3 py-2">
                  <span className="num text-sm text-muted-foreground">{t('dataTable.pageInfo', { page, pages })}</span>
                  <div className="flex gap-2">
                    <Button variant="outline" size="icon" className="size-11 md:size-9" aria-label={t('common.prev')} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                      <ChevronLeft className="size-4 rtl:rotate-180" aria-hidden="true" />
                    </Button>
                    <Button variant="outline" size="icon" className="size-11 md:size-9" aria-label={t('common.next')} disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>
                      <ChevronRight className="size-4 rtl:rotate-180" aria-hidden="true" />
                    </Button>
                  </div>
                </div>
              ) : null}

              {/* 批量操作条与列表视图同一套：浮在卡片底部，勾选不再把宫格下推 */}
              {selected.length > 0 ? (
                <BatchBar count={selected.length} onClear={() => setSelected([])}>
                  {batchActions(selected)}
                </BatchBar>
              ) : null}
            </Card>
          )}
        </div>
      </div>

      <UploadDialog open={uploadOpen} onOpenChange={setUploadOpen} defaultGroupId={group === GROUP_ALL || group === 'none' ? '' : group} />
      <FilePreviewDialog file={preview} images={rows} onClose={() => setPreview(null)} />
      <MoveDialog key={moving ? `move-${moving.join(',')}` : 'move-closed'} open={!!moving} onOpenChange={(o) => !o && setMoving(null)} count={moving?.length ?? 0} onConfirm={(g) => void doMove(g)} />
      <RenameDialog
        key={renaming ? `rename-${renaming.id}` : 'rename-closed'}
        file={renaming}
        onOpenChange={(o) => !o && setRenaming(null)}
        onRenamed={() => setSelected([])}
      />
    </PageContainer>
  )
}
