import { useCallback, useEffect, useRef, useState, type MouseEvent, type PointerEvent } from 'react'
import { createPortal } from 'react-dom'
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
import { OptionPicker } from '@/framework/components/OptionPicker'
import { UploadDialog } from './UploadDialog'
import { downloadFilesZip, useDeleteFiles, useFileList, useMoveFiles, useUpdateFile, type FileItem, type FileSort } from './api'

const PAGE_SIZE_GRID = 24
const CATEGORIES = ['image', 'video', 'doc', 'other'] as const
/** 卡片最小 190px 左右；再往上加列会让文件名和三行元信息全被截断 */
const GRID_CLASS = 'grid grid-cols-2 gap-3 md:gap-4 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5'

interface Filters {
  keyword: string
  category: string[]
  range: DateRangeValue
  sort: FileSort
  desc: boolean
}
const emptyFilters: Filters = { keyword: '', category: [], range: {}, sort: 'created_at', desc: true }

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
    category: applied.category.length ? applied.category.join(',') : undefined,
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

  // ── 网格视图的快速多选 ─────────────────────────────────────────────
  // 按住拖出选框（框选）＋ Shift 连选 / Ctrl(⌘) 点选；单击卡片是选中，双击是预览。
  // 起手区是整个内容行：表头那一条、卡片本身、卡片外那圈空白（分组卡下方、网格右侧）都算——
  // 只挂宫格内层时，这些地方按下去什么都不会发生，而它们恰恰是最容易下手的落点。
  // 指针没动 = 交给卡片自己的点击，动了 = 框选。
  // 移动与抬起挂在 window 上而不是用 setPointerCapture：捕获会把 click 的目标改成容器，
  // 卡片上的点选/双击就再也收不到了。
  const anchorRef = useRef<string | null>(null)
  const pointerKind = useRef<'mouse' | 'touch'>('mouse')
  const gridRef = useRef<HTMLDivElement>(null)
  /** 拖拽结束后紧跟着的那次 click 要吞掉，否则框选会把落点处的卡片再点一下 */
  const suppressClick = useRef(false)
  const dragCleanup = useRef<(() => void) | null>(null)
  /** 选框本体：拖动时直接改 style，不走 state——每帧 setState 会把 24 张卡片整体重渲染，手感立刻变卡 */
  const marqueeRef = useRef<HTMLDivElement>(null)

  useEffect(() => () => dragCleanup.current?.(), [])

  const selectWithModifier = (f: FileItem, e: MouseEvent<HTMLButtonElement>) => {
    const index = rows.findIndex((r) => r.id === f.id)
    const anchor = anchorRef.current ? rows.findIndex((r) => r.id === anchorRef.current) : -1
    const toggle = e.ctrlKey || e.metaKey
    if (e.shiftKey && anchor >= 0 && index >= 0) {
      const [from, to] = index < anchor ? [index, anchor] : [anchor, index]
      const range = rows.slice(from, to + 1).map((r) => r.id)
      setSelected((prev) => (toggle ? [...new Set([...prev, ...range])] : range))
      return
    }
    anchorRef.current = f.id
    setSelected((prev) => (prev.includes(f.id) ? prev.filter((x) => x !== f.id) : [...prev, f.id]))
  }

  const startMarquee = (e: PointerEvent<HTMLDivElement>) => {
    if (view !== 'grid') return // 列表视图有多选列，不掺和
    if (e.button !== 0 || e.pointerType !== 'mouse') return
    const target = e.target as HTMLElement
    // 抓手覆盖整个内容区（表头、卡片、卡片外的空白都算），只有真正的控件让给它们自己：
    // 分组面板、勾选框、卡片右下角菜单、筛选控件、分页与工具栏按钮。
    // 缩略图按钮例外——卡片上按下再拖是最自然的手势，单击/双击仍由它自己的 click 处理。
    if (target.closest('a, input, select, textarea, label, [role="checkbox"], [role="menuitem"], [role="combobox"], [data-no-marquee]')) return
    if (target.closest('button') && !target.closest('li[data-file-id]')) return
    suppressClick.current = false
    const additive = e.ctrlKey || e.metaKey || e.shiftKey
    const drag = { x: e.clientX, y: e.clientY, base: additive ? selected : [], additive, moved: false, onCard: !!target.closest('li[data-file-id]') }
    // 卡片位置在拖动期间不会变：起手时量一次，之后每次移动只做区间比较，
    // 不再每个 pointermove 都对每张卡片调一次 getBoundingClientRect（那是强制布局，最费的一步）
    const tiles: { id: string; left: number; top: number; right: number; bottom: number }[] = []
    gridRef.current?.querySelectorAll<HTMLElement>('li[data-file-id]').forEach((li) => {
      const r = li.getBoundingClientRect()
      tiles.push({ id: li.dataset.fileId as string, left: r.left, top: r.top, right: r.right, bottom: r.bottom })
    })
    /** 上一次命中集合的指纹：没跨过卡片边界时不要 setSelected，否则整片网格白重渲染一次 */
    let lastKey = ''

    const onMove = (ev: globalThis.PointerEvent) => {
      const dx = ev.clientX - drag.x
      const dy = ev.clientY - drag.y
      if (!drag.moved && Math.abs(dx) + Math.abs(dy) < 4) return // 手抖阈值，避免一按就闪框
      drag.moved = true
      const left = Math.min(drag.x, ev.clientX)
      const top = Math.min(drag.y, ev.clientY)
      const width = Math.abs(dx)
      const height = Math.abs(dy)

      const box = marqueeRef.current
      if (box) {
        box.style.display = 'block'
        box.style.left = `${left}px`
        box.style.top = `${top}px`
        box.style.width = `${width}px`
        box.style.height = `${height}px`
      }

      const hits: string[] = []
      for (const t of tiles) {
        if (t.left < left + width && t.right > left && t.top < top + height && t.bottom > top) hits.push(t.id)
      }
      const key = `${drag.additive}|${hits.join(',')}`
      if (key !== lastKey) {
        lastKey = key
        setSelected(drag.additive ? [...new Set([...drag.base, ...hits])] : hits)
      }
    }

    const finish = (cancelled: boolean) => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onCancel)
      dragCleanup.current = null
      if (marqueeRef.current) marqueeRef.current.style.display = 'none'
      if (drag.moved && !cancelled) {
        suppressClick.current = true
        // 落在宫格留白上时 click 不会到卡片，标记留到下次按下时再清（startMarquee 开头会重置）
      } else if (!cancelled && !drag.additive && !drag.onCard) {
        setSelected([]) // 空白处单击：清空选择，与桌面文件管理器一致
      }
    }
    const onUp = () => finish(false)
    const onCancel = () => finish(true)

    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onCancel)
    dragCleanup.current = () => finish(true)
  }

  const toggleVisibility = async (f: FileItem) => {
    try {
      await update.mutateAsync({ id: f.id, patch: { visibility: f.visibility === 'public' ? 'private' : 'public' } })
      toast.success(t('common.operateSuccess'))
    } catch {
      // 失败原因由 http 拦截器统一弹出
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
      // 失败原因由 http 拦截器统一弹出
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
      // 失败原因由 http 拦截器统一弹出
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

  // 卡片菜单的回调必须保持身份稳定：每张卡片各挂一个 Radix 下拉，选中集一变就整片重渲染，
  // 框选时就会一顿一顿的（FileActionsMenu 已是 memo，props 恒等才跳得掉）。
  const menuHandlers = useRef<{ visibility: (f: FileItem) => void; remove: (f: FileItem) => void }>({
    visibility: () => {},
    remove: () => {},
  })
  menuHandlers.current = {
    visibility: (f) => void toggleVisibility(f),
    remove: (f) => void remove([f.id]),
  }
  const onMenuVisibility = useCallback((f: FileItem) => menuHandlers.current.visibility(f), [])
  const onMenuRemove = useCallback((f: FileItem) => menuHandlers.current.remove(f), [])
  const onMenuMove = useCallback((f: FileItem) => setMoving([f.id]), [])

  const actions = (f: FileItem, triggerClassName?: string, vertical?: boolean) => (
    <FileActionsMenu
      file={f}
      triggerClassName={triggerClassName}
      verticalIcon={vertical}
      onPreview={setPreview}
      onToggleVisibility={onMenuVisibility}
      onMove={onMenuMove}
      onRename={setRenaming}
      onDelete={onMenuRemove}
    />
  )

  const columns: Column<FileItem>[] = [
    {
      key: 'name',
      title: t('fileMgr.name'),
      sortKey: 'name',
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
    { key: 'size', title: t('fileMgr.size'), sortKey: 'size', align: 'end', render: (f) => <span className="num">{formatSize(f.size)}</span> },
    { key: 'visibility', title: t('fileMgr.visibility'), render: visBadge },
    { key: 'uploader', title: t('fileMgr.uploader') },
    { key: 'createdAt', title: t('fileMgr.createdAt'), sortKey: 'created_at', render: (f) => <span className="num">{fmtTime(f.createdAt)}</span> },
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
      <OptionPicker
        title={t('fileMgr.filterType')}
        value={draft.category}
        onChange={(v) => setDraft((d) => ({ ...d, category: v }))}
        options={CATEGORIES.map((c) => ({ value: c, label: t(`fileMgr.kind.${c}`) }))}
        className="md:w-40 md:shrink-0"
        placeholder={t('fileMgr.filterType')}
      />
      <DateRangePicker value={draft.range} onChange={(range) => setDraft((d) => ({ ...d, range }))} ariaLabel={t('fileMgr.timeRange')} className="md:w-52 md:shrink-0" placeholder={t('fileMgr.timeRange')} />
    </FilterBar>
  )

  const viewToggle = () => (
    // 高度跟同一行的排序下拉、列设置按钮对齐（移动 40 / 桌面 36）：分组容器自带 border+p-0.5，
    // 条目撑到容器内高，三者外框才一样高；条目自带 min-h-10/min-h-8，必须置 0 才让 h-full 生效。
    <Segmented
      ariaLabel={t('fileMgr.viewGrid')}
      value={view}
      onChange={(v) => {
        setView(v as 'grid' | 'list')
        setPage(1)
      }}
      className="h-10 md:h-9"
      options={[
        {
          value: 'grid',
          label: <LayoutGrid className="size-4" aria-hidden="true" />,
          ariaLabel: t('fileMgr.viewGrid'),
          className: 'h-full w-11 min-h-0! px-0 md:w-9',
        },
        {
          value: 'list',
          label: <List className="size-4" aria-hidden="true" />,
          ariaLabel: t('fileMgr.viewList'),
          className: 'h-full w-11 min-h-0! px-0 md:w-9',
        },
      ]}
    />
  )

  // 排序属于「结果怎么展示」，不进筛选表单；改了就立即重排，不必再点搜索。
  // 列表视图点表头（DataTable 的 sort）与这个下拉共用同一份状态，两边永远一致。
  const applySort = (sort: FileSort, desc: boolean) => {
    const next = { sort, desc }
    setDraft((d) => ({ ...d, ...next }))
    setApplied((a) => ({ ...a, ...next }))
    setPage(1)
    setSelected([])
  }

  const sortSelect = (
    <Select
      value={`${draft.sort}:${draft.desc ? 'desc' : 'asc'}`}
      onValueChange={(v) => {
        const [sort, dir] = v.split(':')
        applySort(sort as FileSort, dir === 'desc')
      }}
    >
      <SelectTrigger className="w-44 shrink-0 md:h-9" aria-label={t('fileMgr.sortBy')}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {/* 三个可排字段 × 两个方向全列出来：表头点出来的任何一种组合，下拉里都要有对应项，
            否则受控 Select 找不到匹配项会显示成空白 */}
        <SelectItem value="created_at:desc">{t('fileMgr.sortNewest')}</SelectItem>
        <SelectItem value="created_at:asc">{t('fileMgr.sortOldest')}</SelectItem>
        <SelectItem value="name:asc">{t('fileMgr.sortNameAsc')}</SelectItem>
        <SelectItem value="name:desc">{t('fileMgr.sortNameDesc')}</SelectItem>
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
        <Upload aria-hidden="true" />
        {t('fileMgr.upload')}
      </Button>
    </Can>
  )

  return (
    <PageContainer>
      <PageHeader title={t('fileMgr.title')} description={t('fileMgr.description')} actions={uploadBtn} />

      {/* 框选的起手区放在整个内容行上：卡片外那圈空白（左侧分组卡下方、表头那一行）在真机上
          是最容易下手的落点，只挂宫格内层的话这些地方按下去什么都不会发生 */}
      <div
        className="flex flex-col gap-4 md:flex-row md:items-start"
        onPointerDown={startMarquee}
        onDragStart={(e) => e.preventDefault()}
      >
        <Card data-no-marquee className="p-3 md:w-52 md:shrink-0 lg:w-60">
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
              sort={{ key: applied.sort, desc: applied.desc, onChange: (key, desc) => applySort(key as FileSort, desc) }}
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
            <div className="flex flex-col gap-3">
              {/* 工具条与列表视图同构：不在卡片里、与卡片同宽、右侧留出列设置按钮的位置。
                  之前它压在卡片头部（多了 px-3 与 1px 边框），切视图时整排按钮会左右跳 31px。 */}
              <div className="flex flex-wrap items-center justify-between gap-2">
                {resultBar}
                <div aria-hidden="true" className="w-10 shrink-0 md:w-9" />
              </div>

              <Card className="overflow-hidden rounded-xl">
                <div ref={gridRef} className="select-none p-3">
                {isLoading ? (
                  <div className={cn('file-grid', GRID_CLASS)}>
                    {Array.from({ length: 10 }, (_, i) => (
                      <Skeleton key={i} className="aspect-4/3 w-full rounded-lg" />
                    ))}
                  </div>
                ) : rows.length === 0 ? (
                  <EmptyState title={t('fileMgr.empty')} />
                ) : (
                  <ul className={cn('file-grid', GRID_CLASS)}>
                    {rows.map((f) => {
                      const checked = selected.includes(f.id)
                      return (
                        <li key={f.id} data-file-id={f.id}>
                          <Card className={cn('group h-full overflow-hidden transition-shadow hover:shadow-md motion-reduce:transition-none', checked && 'ring-2 ring-primary')}>
                            <div className="relative aspect-4/3 overflow-hidden border-b border-border">
                              <button
                                type="button"
                                aria-label={t('fileMgr.selectFile', { name: f.name })}
                                className="block size-full"
                                onPointerDown={(e) => {
                                  pointerKind.current = e.pointerType === 'mouse' ? 'mouse' : 'touch'
                                }}
                                onClick={(e) => {
                                  if (suppressClick.current) {
                                    suppressClick.current = false // 刚拖完框，别把落点卡片再点一下
                                    return
                                  }
                                  // 触屏点按保持「预览」：手机上点一下就选、看还得再进菜单，太重
                                  if (pointerKind.current === 'touch') {
                                    setPreview(f)
                                    return
                                  }
                                  if (e.detail === 2) return // 双击的第二下不再切换，交给 onDoubleClick
                                  if (e.shiftKey || e.ctrlKey || e.metaKey) {
                                    e.preventDefault()
                                    selectWithModifier(f, e)
                                    return
                                  }
                                  anchorRef.current = f.id
                                  setSelected((s) => (checked ? s.filter((x) => x !== f.id) : [...s, f.id]))
                                }}
                                onDoubleClick={() => setPreview(f)}
                              >
                                <FileThumb file={f} />
                              </button>
                              <div data-no-marquee className="absolute inset-s-1 top-1 flex size-11 items-center justify-center md:size-8">
                                <Checkbox
                                  checked={checked}
                                  aria-label={t('fileMgr.selectFile', { name: f.name })}
                                  // 压在图片上，不能用卡片底色（深色主题下几乎是纯黑，看着像个洞），
                                  // 也不该用白底（白底图里就消失了）：统一成玻璃底 + 亮边，亮图暗图都看得见；
                                  // 选中态加一圈白色描边，把它从同样明亮的图片上剥出来。
                                  className={cn(
                                    'border-white/85 bg-black/30 shadow-sm backdrop-blur-sm',
                                    'hover:border-white hover:bg-black/50',
                                    'data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground',
                                    // 选中后是实心块，靠一圈深色发丝边把边缘钉住（白底图上一片白会糊）
                                    'data-[state=checked]:ring-1 data-[state=checked]:ring-black/25',
                                  )}
                                  onCheckedChange={() => {
                                    anchorRef.current = f.id
                                    setSelected((s) => (checked ? s.filter((x) => x !== f.id) : [...s, f.id]))
                                  }}
                                />
                              </div>
                              {f.visibility === 'private' && (
                                <Badge variant="outline" className="absolute inset-e-1.5 top-1.5 gap-1 border-border bg-background/90 px-1.5 py-0.5 text-[11px] font-medium shadow-xs backdrop-blur-xs">
                                  <Lock className="size-3" aria-hidden="true" />
                                  {t('fileMgr.private')}
                                </Badge>
                              )}
                            </div>
                            {/* 操作入口放文件名这一行的右端（竖三点），不再压在缩略图右下角：
                                图片区留给缩略图本身，菜单也不会挡住画面内容 */}
                            <div className="flex items-start gap-1 p-2.5">
                              <div className="min-w-0 flex-1">
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
                              <div data-no-marquee className="-me-1 -mt-1 shrink-0">
                                {actions(f, 'text-muted-foreground hover:text-foreground', true)}
                              </div>
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
            </div>
          )}
        </div>
      </div>

      {/* 框选选框：常驻一个隐藏的 div，拖动时直接改 style（见 startMarquee）。
          挂在 body 上是为了避开页面容器的进场动画（transform 会让 fixed 相对它定位，选框跟指针错位）。 */}
      {createPortal(
        <div
          ref={marqueeRef}
          aria-hidden="true"
          className="pointer-events-none fixed z-50 hidden rounded-sm border border-primary bg-primary/10"
        />,
        document.body,
      )}

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
