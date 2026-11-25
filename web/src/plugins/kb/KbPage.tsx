import { useState } from 'react'
import { AlertTriangle, BotMessageSquare, CircleCheck, CircleX, Clock, FileText, Loader2, Pencil, RefreshCw, RotateCcw, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { FileDropField } from '@/framework/components/FileDropField'
import { FilePickerDialog, PickFromLibraryButton } from '@/framework/components/FilePickerDialog'
import type { FileItem } from '@/framework/modules/file/api'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { Textarea } from '@/framework/ui/textarea'
import { errMsg } from '@/lib/errMsg'
import MissPoolPanel from './MissPoolPanel'
import ChunksManageDialog from './ChunksManageDialog'
import {
  uploadDocument,
  uploadDocumentFromLibrary,
  useAsk,
  useDeleteDocument,
  useDocuments,
  useKbSettings,
  useKbStats,
  useKbStatus,
  useRenameDocument,
  useReembedDocument,
  useRetryDocument,
  useSaveKbSettings,
  type KbDocument,
  type KbSettings,
} from './api'

/** 单个文件在批量上传过程中的行状态：等待 → 上传中 → 成功/失败。 */
type UploadState = 'pending' | 'uploading' | 'done' | 'failed'

/** 上传弹窗：后端只做抽取与建档，切块/向量化在后台异步跑，投递即返回。
 * 支持两条来源：本地直传（FileDropField）与文件库选取（fileId 引用，不重复占存储），
 * 两条队列在同一个循环里提交，行状态共用一张表；列表会自动轮询到入库完成。 */
function UploadDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<File[]>([])
  const [libPicked, setLibPicked] = useState<FileItem[]>([])
  const [pickOpen, setPickOpen] = useState(false)
  const [states, setStates] = useState<Record<string, UploadState>>({})
  const [busy, setBusy] = useState(false)
  const ready = (files.length > 0 || libPicked.length > 0) && !busy
  const okCount = Object.values(states).filter((s) => s === 'done').length
  const failCount = Object.values(states).filter((s) => s === 'failed').length

  const submit = async () => {
    if (!ready) return
    setBusy(true)
    const failed = new Set<string>()
    setStates(Object.fromEntries([...files.map((f) => [f.name, 'pending' as const]), ...libPicked.map((p) => [p.name, 'pending' as const])]))
    let ok = 0
    let fail = 0
    const runOne = async (name: string, run: () => Promise<KbDocument>) => {
      setStates((s) => ({ ...s, [name]: 'uploading' }))
      try {
        const doc = await run()
        ok++
        setStates((s) => ({ ...s, [name]: 'done' }))
        toast.success(t('plugin_kb.uploadQueued', { name: doc.title }))
      } catch (e) {
        fail++
        // uploadDocument 走原生 fetch、不经 http 拦截器：这条 toast 是失败原因的唯一反馈。
        // 失败原因同时落在行上（超限 / 格式不支持 / 抽取失败一眼定位）与 toast 里
        failed.add(name)
        setStates((s) => ({ ...s, [name]: 'failed' }))
        toast.error(`${name}: ${errMsg(e, t('common.operateFailed'))}`)
      }
    }
    for (const file of files) {
      await runOne(file.name, () => uploadDocument(file))
    }
    for (const picked of libPicked) {
      await runOne(picked.name, () => uploadDocumentFromLibrary(picked.id, picked.name))
    }
    setBusy(false)
    if (ok > 0 && fail === 0) {
      setFiles([])
      setLibPicked([])
      onOpenChange(false)
    } else if (ok > 0) {
      // 部分成功：留下失败行在弹窗里，用户可以只补传失败的文件
      setFiles((fs) => fs.filter((f) => failed.has(f.name)))
      setLibPicked((ps) => ps.filter((p) => failed.has(p.name)))
    } else {
      toast.error(t('plugin_kb.uploadAllFailed', { fail }))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          setFiles([])
          setLibPicked([])
          setStates({})
          setPickOpen(false)
        }
        onOpenChange(o)
      }}
      title={t('plugin_kb.uploadTitle')}
      description={t('plugin_kb.uploadDesc')}
      size="md"
      footer={
        <>
          <Button variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={!ready}>
            {busy ? <Loader2 className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <Upload aria-hidden="true" />}
            {busy ? t('plugin_kb.uploadBusy', { done: okCount, fail: failCount }) : t('plugin_kb.upload')}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <FileDropField
          multiple
          accept=".txt,.md,.markdown,.pdf,.docx,text/plain,text/markdown,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document"
          files={files}
          onChange={setFiles}
          /* 从文件库选取：已传过的文档直接引用（fileId），不再重复占一份存储。
             内嵌在拖拽框底行——它是「拖拽/点选」之外的另一条入口，与主入口同框可视。 */
          extra={<PickFromLibraryButton onClick={() => setPickOpen(true)} />}
        />
        <p className="text-xs text-muted-foreground">{t('plugin_kb.uploadHint')}</p>
        {libPicked.length > 0 ? (
          <ul className="divide-y divide-border rounded-lg border border-border text-sm">
            {libPicked.map((p) => {
              const st = states[p.name]
              return (
                <li key={p.id} className="flex items-center gap-2 px-3 py-2">
                  {st === 'done' ? (
                    <CircleCheck aria-hidden="true" className="size-4 shrink-0 text-success" />
                  ) : st === 'failed' ? (
                    <CircleX aria-hidden="true" className="size-4 shrink-0 text-destructive" />
                  ) : st === 'uploading' ? (
                    <Loader2 aria-hidden="true" className="size-4 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" />
                  ) : (
                    <FileText aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="min-w-0 flex-1 truncate" title={p.name}>
                    {p.name}
                  </span>
                  {st ? <span className="shrink-0 text-xs text-muted-foreground">{t(`plugin_kb.uploadState.${st}`)}</span> : null}
                  {!st ? (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={t('common.delete')}
                      disabled={busy}
                      onClick={() => setLibPicked((ps) => ps.filter((x) => x.id !== p.id))}
                    >
                      <Trash2 className="size-4 text-destructive" aria-hidden="true" />
                    </Button>
                  ) : null}
                </li>
              )
            })}
          </ul>
        ) : null}
        {Object.keys(states).length > 0 ? (
          <ul className="divide-y divide-border rounded-lg border border-border text-sm">
            {files.map((f, i) => {
              const st = states[f.name] ?? 'pending'
              return (
                <li key={`${f.name}-${i}`} className="flex items-center gap-2 px-3 py-2">
                  {st === 'done' ? (
                    <CircleCheck aria-hidden="true" className="size-4 shrink-0 text-success" />
                  ) : st === 'failed' ? (
                    <CircleX aria-hidden="true" className="size-4 shrink-0 text-destructive" />
                  ) : st === 'uploading' ? (
                    <Loader2 aria-hidden="true" className="size-4 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" />
                  ) : (
                    <Clock aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                  )}
                  <span className="min-w-0 flex-1 truncate" title={f.name}>
                    {f.name}
                  </span>
                  <span className="shrink-0 text-xs text-muted-foreground">{t(`plugin_kb.uploadState.${st}`)}</span>
                </li>
              )
            })}
          </ul>
        ) : null}
        <FilePickerDialog
          open={pickOpen}
          onOpenChange={setPickOpen}
          kind="file"
          multiple
          value={[]}
          onConfirm={(_urls, items) => {
            // 只追加新勾选的：同一份文件重复选不进队列
            setLibPicked((ps) => {
              const have = new Set(ps.map((p) => p.id))
              return [...ps, ...items.filter((it) => !have.has(it.id))]
            })
          }}
        />
      </div>
    </ResponsiveDialog>
  )
}

/** 重命名弹窗：只改展示标题，切块与向量不动。 */
function RenameDialog({ doc, onOpenChange }: { doc: KbDocument; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const save = useRenameDocument()
  const [title, setTitle] = useState(doc.title)
  const submit = async () => {
    if (!title.trim()) return
    try {
      await save.mutateAsync({ id: doc.id, title })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }
  return (
    <ResponsiveDialog side="right"
      open
      onOpenChange={onOpenChange}
      title={t('plugin_kb.renameTitle')}
      description={t('plugin_kb.renameDesc', { name: doc.title })}
      size="sm"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending || !title.trim()}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="kb-rename">{t('plugin_kb.fields.title')}</Label>
        <Input
          id="kb-rename"
          value={title}
          maxLength={200}
          autoFocus
          onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && title.trim() && !save.isPending) void submit()
          }}
        />
      </div>
    </ResponsiveDialog>
  )
}

/** 检索设置弹窗：阈值 / TopK / 检索方式 / 分块参数 / 追加风格要求。 */
function SettingsDialog({
  open,
  onOpenChange,
  initial,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  initial?: KbSettings
}) {
  const { t } = useTranslation()
  const save = useSaveKbSettings()
  const [threshold, setThreshold] = useState(String(initial?.threshold ?? ''))
  const [topK, setTopK] = useState(String(initial?.topK ?? ''))
  const [mode, setMode] = useState<'hybrid' | 'vector'>(initial?.mode ?? 'hybrid')
  const [chunkSize, setChunkSize] = useState(String(initial?.chunkSize ?? ''))
  const [chunkOverlap, setChunkOverlap] = useState(String(initial?.chunkOverlap ?? ''))
  const [systemPrompt, setSystemPrompt] = useState(initial?.systemPrompt ?? '')
  const ready = threshold !== '' && topK !== '' && chunkSize !== '' && chunkOverlap !== ''

  const submit = async () => {
    try {
      await save.mutateAsync({
        threshold: Number(threshold),
        topK: Number(topK),
        systemPrompt,
        mode,
        chunkSize: Number(chunkSize),
        chunkOverlap: Number(chunkOverlap),
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  return (
    <ResponsiveDialog side="right"
      open={open}
      onOpenChange={onOpenChange}
      title={t('plugin_kb.settingsTitle')}
      description={t('plugin_kb.settingsDesc')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending || !ready}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4 text-sm">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="kb-mode">{t('plugin_kb.fields.mode')}</Label>
          <SimpleSelect
            className="w-full"
            aria-label={t('plugin_kb.fields.mode')}
            value={mode}
            onChange={(v) => setMode(v as 'hybrid' | 'vector')}
            options={[
              { value: 'hybrid', label: t('plugin_kb.fields.modeHybrid') },
              { value: 'vector', label: t('plugin_kb.fields.modeVector') },
            ]}
          />
          <p className="text-xs text-muted-foreground">{t('plugin_kb.fields.modeHint')}</p>
        </div>
        <div className="grid grid-cols-1 gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="kb-threshold">{t('plugin_kb.fields.threshold')}</Label>
            <Input id="kb-threshold" type="number" min="0.01" max="0.99" step="0.01" value={threshold} onChange={(e) => setThreshold(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t('plugin_kb.fields.thresholdHint')}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="kb-topk">{t('plugin_kb.fields.topK')}</Label>
            <Input id="kb-topk" type="number" min="1" max="10" step="1" value={topK} onChange={(e) => setTopK(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t('plugin_kb.fields.topKHint')}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="kb-chunksize">{t('plugin_kb.fields.chunkSize')}</Label>
            <Input id="kb-chunksize" type="number" min="100" max="2000" step="50" value={chunkSize} onChange={(e) => setChunkSize(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t('plugin_kb.fields.chunkSizeHint')}</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="kb-chunkoverlap">{t('plugin_kb.fields.chunkOverlap')}</Label>
            <Input id="kb-chunkoverlap" type="number" min="0" max="1000" step="10" value={chunkOverlap} onChange={(e) => setChunkOverlap(e.target.value)} />
            <p className="text-xs text-muted-foreground">{t('plugin_kb.fields.chunkOverlapHint')}</p>
          </div>
        </div>
        <p className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">{t('plugin_kb.fields.chunkNote')}</p>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="kb-prompt">{t('plugin_kb.fields.systemPrompt')}</Label>
          <Textarea id="kb-prompt" rows={4} value={systemPrompt} onChange={(e) => setSystemPrompt(e.target.value)} placeholder={t('plugin_kb.fields.systemPromptHint')} />
          <p className="text-xs text-muted-foreground">{t('plugin_kb.fields.systemPromptNote')}</p>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

function formatTime(v: string, lang: string) {
  return new Intl.DateTimeFormat(lang, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(v))
}

/** 体积展示：统计条用，超过 1KB 才进位，避免「0.8 MB」这种读不出量级的写法。 */
function formatSize(bytes: number) {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${i === 0 || v >= 10 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/**
 * 知识库页：文档列表 + 上传 + 检索设置 + 测试提问。
 * AI 是否已具备条件（对话模型 / Embedding 模型）由 status 接口给结论，顶部横幅给设置指引。
 */
export default function KbPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [uploadOpen, setUploadOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [chunksDoc, setChunksDoc] = useState<KbDocument | null>(null)
  const [renameDoc, setRenameDoc] = useState<KbDocument | null>(null)
  const [question, setQuestion] = useState('')
  // 状态筛选：ready/failed/stale 全部交后端（?status=），分页后前端过滤只在当前页生效，
  // 会出现「明明有需重嵌的文档却筛不出来」。
  const [statusFilter, setStatusFilter] = useState<'all' | 'ready' | 'failed' | 'stale' | 'queued'>('all')
  const [selected, setSelected] = useState<string[]>([])
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  // 表头排序：默认新文档在前（与后端回退一致）；排序变化回第一页（旧页码常越界）
  const { sort, desc, sortProp } = useHeaderSort('created_at', true, () => setPage(1))

  const docs = useDocuments(statusFilter === 'all' ? undefined : statusFilter, sort, desc, page, pageSize)
  const stats = useKbStats()
  const status = useKbStatus()
  const settings = useKbSettings()
  const remove = useDeleteDocument()
  const reembed = useReembedDocument()
  const retry = useRetryDocument()
  const ask = useAsk()

  const embedModel = status.data?.embedModel ?? ''
  // 排队/处理中由 worker 持有，不参与重嵌；否则会和后台写切块撞车。
  const isBusy = (d: KbDocument) => d.status === 'pending' || d.status === 'processing'
  const needReembed = (d: KbDocument) => !isBusy(d) && (d.status === 'failed' || (!!embedModel && !!d.model && d.model !== embedModel))
  const rows = docs.data?.list ?? []
  const total = docs.data?.total ?? 0

  const onDelete = async (d: KbDocument) => {
    const ok = await confirm({
      title: t('common.delete'),
      description: t('plugin_kb.deleteConfirm', { name: d.title }),
      destructive: true,
    })
    if (!ok) return
    try {
      await remove.mutateAsync(d.id)
      toast.success(t('common.operateSuccess'))
      // 删掉本页最后一条时回退一页，避免停在空白页
      if (rows.length === 1 && page > 1) setPage(page - 1)
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  // 批量删除：后端只有单条 DELETE，前端逐条循环；条数大时串行耗时可接受（文档量级小）
  const onBatchDelete = async (ids: string[]) => {
    const targets = rows.filter((d) => ids.includes(d.id))
    if (targets.length === 0) return
    const ok = await confirm({
      title: t('common.delete'),
      description: t('plugin_kb.batchDeleteConfirm', { count: targets.length }),
      destructive: true,
    })
    if (!ok) return
    let done = 0
    let fail = 0
    for (const d of targets) {
      try {
        await remove.mutateAsync(d.id)
        done++
      } catch {
        fail++
      }
    }
    if (fail > 0) toast.warning(t('plugin_kb.batchDeleteDone', { done, fail }))
    else toast.success(t('plugin_kb.batchDeleteDone', { done, fail }))
    setSelected([])
    if (fail === 0 && done === rows.length && page > 1) setPage(page - 1)
  }

  // 批量重新向量化：同样逐条调单文档接口，只挑真正「需重新向量化」的行
  const onBatchReembed = async (ids: string[]) => {
    const targets = rows.filter((d) => ids.includes(d.id) && needReembed(d))
    if (targets.length === 0) return
    let done = 0
    let fail = 0
    for (const d of targets) {
      try {
        await reembed.mutateAsync(d.id)
        done++
      } catch {
        fail++
      }
    }
    if (fail > 0) toast.warning(t('plugin_kb.batchReembedDone', { done, fail }))
    else toast.success(t('plugin_kb.batchReembedDone', { done, fail }))
    setSelected([])
  }

  const onReembed = async (d: KbDocument) => {
    try {
      await reembed.mutateAsync(d.id)
      toast.success(t('plugin_kb.reembedDone', { name: d.title }))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const onRetry = async (d: KbDocument) => {
    try {
      await retry.mutateAsync(d.id)
      toast.success(t('plugin_kb.retryDone', { name: d.title }))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const onAsk = async () => {
    if (!question.trim()) return
    try {
      const res = await ask.mutateAsync(question.trim())
      if (res.handoff) {
        toast.warning(t('plugin_kb.askHandoff'))
        return
      }
      const top = res.hits[0]
      toast.success(t('plugin_kb.askDone', { title: top?.title ?? '', score: top?.score ?? 0 }))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const columns: Column<KbDocument>[] = [
    {
      key: 'title',
      title: t('plugin_kb.fields.title'),
      sortKey: 'title',
      mobile: 'title',
      render: (d) => (
        <div className="min-w-0">
          <button
            type="button"
            className="block max-w-[20rem] truncate font-medium text-foreground underline-offset-4 hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
            title={t('plugin_kb.chunksOpen')}
            onClick={() => setChunksDoc(d)}
          >
            {d.title}
          </button>
          <div className="text-xs text-muted-foreground">{d.sourceName}</div>
        </div>
      ),
    },
    {
      key: 'status',
      title: t('plugin_kb.fields.status'),
      render: (d) => (
        <div className="flex min-w-28 flex-col gap-1">
          <Badge variant={d.status === 'ready' ? 'success' : d.status === 'failed' ? 'destructive' : 'warning'}>
            {t(`plugin_kb.status.${d.status}`)}
          </Badge>
          {isBusy(d) ? (
            <span className="max-w-56 text-xs text-muted-foreground">{d.stage || t('plugin_kb.stageQueued')}</span>
          ) : null}
          {d.status === 'failed' && d.error ? <span className="max-w-56 text-xs text-muted-foreground">{d.error}</span> : null}
        </div>
      ),
    },
    { key: 'chunks', title: t('plugin_kb.fields.chunks'), align: 'end', render: (d) => <span className="num">{d.chunkCount}</span> },
    {
      key: 'model',
      title: t('plugin_kb.fields.model'),
      render: (d) => (
        <div className="flex min-w-40 flex-col gap-1">
          <span className="truncate text-xs" title={d.model}>
            {d.model || '—'}
          </span>
          {needReembed(d) ? <Badge variant="warning">{t('plugin_kb.needReembed')}</Badge> : null}
        </div>
      ),
    },
    {
      key: 'createdAt',
      title: t('plugin_kb.fields.createdAt'),
      sortKey: 'created_at',
      render: (d) => <span className="num text-xs text-muted-foreground">{formatTime(d.createdAt, i18n.language)}</span>,
    },
    {
      key: 'actions',
      title: t('plugin_kb.fields.actions'),
      render: (d) => (
        <div className="flex items-center gap-1">
          <Can perm="kb:edit">
            <Button variant="ghost" size="icon" title={t('plugin_kb.rename')} onClick={() => setRenameDoc(d)}>
              <Pencil aria-hidden="true" />
            </Button>
            {d.status === 'failed' ? (
              <Button variant="ghost" size="icon" title={t('plugin_kb.retry')} disabled={retry.isPending} onClick={() => void onRetry(d)}>
                <RotateCcw aria-hidden="true" />
              </Button>
            ) : null}
            {needReembed(d) ? (
              <Button variant="ghost" size="icon" title={t('plugin_kb.reembed')} disabled={reembed.isPending} onClick={() => void onReembed(d)}>
                <RefreshCw aria-hidden="true" />
              </Button>
            ) : null}
          </Can>
          <Can perm="kb:delete">
            <Button variant="ghost" size="icon" title={t('common.delete')} onClick={() => void onDelete(d)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        </div>
      ),
    },
  ]

  return (
    <PageContainer>
      {/* 动作按钮放表格工具栏左侧（与产品管理等列表页同一口径），不悬浮在标题右侧 */}
      <PageHeader title={t('plugin_kb.title')} description={t('plugin_kb.description')} />

      {status.data && (!status.data.embedReady || !status.data.chatReady) ? (
        <div className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm text-warning">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>{status.data.embedReady ? t('plugin_kb.missingChat') : t('plugin_kb.missingEmbed')}</span>
        </div>
      ) : null}

      {stats.data ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <FileText className="size-3.5" aria-hidden="true" />
            {t('plugin_kb.statsDocs', { count: stats.data.docs })}
          </span>
          <span>{t('plugin_kb.statsChunks', { count: stats.data.chunks })}</span>
          <span>{t('plugin_kb.statsSize', { size: formatSize(stats.data.size) })}</span>
          {status.data?.embedModel ? <span>{t('plugin_kb.statsModel', { model: status.data.embedModel })}</span> : null}
          {stats.data.queued > 0 ? (
            <span className="inline-flex items-center gap-1.5 text-warning">
              <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" aria-hidden="true" />
              {t('plugin_kb.statsQueued', { count: stats.data.queued })}
            </span>
          ) : null}
          {stats.data.failed > 0 ? (
            <span className="text-warning">{t('plugin_kb.statsFailed', { count: stats.data.failed })}</span>
          ) : null}
          {stats.data.pendingMiss > 0 ? (
            <span className="text-warning">{t('plugin_kb.statsPendingMiss', { count: stats.data.pendingMiss })}</span>
          ) : null}
        </div>
      ) : null}

      <DataTable
        columns={columns}
        data={rows}
        rowKey="id"
        loading={docs.isLoading}
        isError={docs.isError}
        onRetry={() => void docs.refetch()}
        retrying={docs.isFetching}
        hasFilters={statusFilter !== 'all'}
        sort={sortProp}
        pagination={{
          page,
          pageSize,
          total,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
            setSelected([])
          },
        }}
        columnSettingsKey="plugin-kb-documents"
        emptyText={t('plugin_kb.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => {
          const reembedTargets = rows.filter((d) => ids.includes(d.id) && needReembed(d))
          return (
            <>
              {reembedTargets.length > 0 ? (
                <Can perm="kb:edit">
                  <Button size="sm" variant="outline" disabled={reembed.isPending} onClick={() => void onBatchReembed(ids)}>
                    <RefreshCw aria-hidden="true" />
                    {t('plugin_kb.batchReembed', { count: reembedTargets.length })}
                  </Button>
                </Can>
              ) : null}
              <Can perm="kb:delete">
                <Button size="sm" variant="destructive" disabled={remove.isPending} onClick={() => void onBatchDelete(ids)}>
                  <Trash2 aria-hidden="true" />
                  {t('plugin_kb.batchDelete')}
                </Button>
              </Can>
            </>
          )
        }}
        toolbar={
          <>
            <Can perm="kb:edit">
              <Button variant="outline" onClick={() => setSettingsOpen(true)}>
                <BotMessageSquare aria-hidden="true" />
                {t('plugin_kb.settingsTitle')}
              </Button>
              <Button onClick={() => setUploadOpen(true)}>
                <Upload aria-hidden="true" />
                {t('plugin_kb.upload')}
              </Button>
            </Can>
            <SimpleSelect
              className="w-44"
              aria-label={t('plugin_kb.filterStatus')}
              value={statusFilter}
              onChange={(v) => {
                setStatusFilter(v as 'all' | 'ready' | 'failed' | 'stale' | 'queued')
                setPage(1)
                setSelected([])
              }}
              options={[
                { value: 'all', label: t('plugin_kb.filterAll') },
                { value: 'ready', label: t('plugin_kb.status.ready') },
                { value: 'failed', label: t('plugin_kb.status.failed') },
                { value: 'queued', label: t('plugin_kb.filterQueued') },
                { value: 'stale', label: t('plugin_kb.filterReembed') },
              ]}
            />
          </>
        }
      />

      {/* 未命中问题池：线上答不上来的问题攒在这里，补一条答案即转成 QA 文档。 */}
      <Can perm="kb:list">
        <MissPoolPanel />
      </Can>

      {/* 测试提问：不走 IM，直接验证检索命中与模型回答（调阈值用）。 */}
      <Can perm="kb:list">
        <div className="flex flex-col gap-2 rounded-lg border bg-card p-4">
          <Label htmlFor="kb-ask">{t('plugin_kb.askTitle')}</Label>
          <div className="flex gap-2">
            <Input
              id="kb-ask"
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              placeholder={t('plugin_kb.askPlaceholder')}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !ask.isPending) void onAsk()
              }}
            />
            <Button onClick={() => void onAsk()} disabled={ask.isPending || !question.trim()}>
              {t('plugin_kb.ask')}
            </Button>
          </div>
          {ask.data ? (
            ask.data.handoff ? (
              // 检索未命中：候选列表帮定位「差多少」——若候选分数普遍接近阈值，
              // 说明知识库里有沾边内容，调低阈值或补文档；普遍很低则是问的东西库里没有。
              <div className="flex flex-col gap-2 text-sm">
                <div className="rounded-md bg-warning/10 px-3 py-2 text-warning">{t('plugin_kb.askHandoff')}</div>
                {ask.data.candidates?.length ? (
                  <div className="flex flex-col gap-1">
                    <p className="text-xs text-muted-foreground">{t('plugin_kb.candidatesTitle')}</p>
                    {ask.data.candidates.map((h) => (
                      <div key={h.documentId + h.excerpt.slice(0, 8)} className="flex items-baseline gap-2 text-xs text-muted-foreground">
                        <Badge variant="outline" className="shrink-0">
                          {t('plugin_kb.hitScore', { score: h.score })}
                        </Badge>
                        <span className="font-medium text-foreground">{h.title}</span>
                        <span className="min-w-0 truncate">{h.excerpt}</span>
                      </div>
                    ))}
                  </div>
                ) : null}
              </div>
            ) : (
              <div className="flex flex-col gap-2 text-sm">
                <div className="rounded-md bg-muted px-3 py-2 whitespace-pre-wrap">{ask.data.answer}</div>
                <div className="flex flex-col gap-1">
                  {ask.data.hits.map((h) => (
                    <div key={h.documentId + h.excerpt.slice(0, 8)} className="flex items-baseline gap-2 text-xs text-muted-foreground">
                      <Badge variant="outline" className="shrink-0">
                        {t('plugin_kb.hitScore', { score: h.score })}
                      </Badge>
                      {h.match === 'keyword' ? (
                        <Badge variant="warning" className="shrink-0">
                          {t('plugin_kb.matchKeyword')}
                        </Badge>
                      ) : null}
                      <span className="font-medium text-foreground">{h.title}</span>
                      <span className="min-w-0 truncate">{h.excerpt}</span>
                    </div>
                  ))}
                </div>
              </div>
            )
          ) : null}
        </div>
      </Can>

      <UploadDialog open={uploadOpen} onOpenChange={setUploadOpen} />
      {chunksDoc ? <ChunksManageDialog doc={chunksDoc} onOpenChange={(v) => !v && setChunksDoc(null)} /> : null}
      {renameDoc ? <RenameDialog doc={renameDoc} onOpenChange={(v) => !v && setRenameDoc(null)} /> : null}
      {settings.data ? (
        <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} initial={settings.data} />
      ) : null}
    </PageContainer>
  )
}
