import { useMemo, useState } from 'react'
import { AlertTriangle, BotMessageSquare, CircleCheck, CircleX, Clock, FileText, Loader2, Pencil, RefreshCw, Trash2, Upload } from 'lucide-react'
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
import {
  uploadDocument,
  uploadDocumentFromLibrary,
  useAsk,
  useDeleteDocument,
  useDocumentChunks,
  useDocuments,
  useKbSettings,
  useKbStatus,
  useRenameDocument,
  useReembedDocument,
  useSaveKbSettings,
  type KbDocument,
} from './api'

/** 单个文件在批量上传过程中的行状态：等待 → 上传中 → 成功/失败。 */
type UploadState = 'pending' | 'uploading' | 'done' | 'failed'

/** 上传弹窗：一期只收 txt/md（≤2MB），同步切分并向量化，成功即入列。
 * 多文件逐个串行上传：后端单次同步切块 + 向量化（十几秒级），并行只会互相挤占。
 * 支持两条来源：本地直传（FileDropField）与文件库选取（fileId 引用，不重复占存储），
 * 两条队列在同一个串行循环里提交，行状态共用一张表。 */
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
        toast.success(t('plugin_kb.uploadDone', { name: doc.title, count: doc.chunkCount }))
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
        />
        {/* 从文件库选取：已传过的文档直接引用（fileId），不再重复占一份存储。
            左对齐紧跟拖拽区——它是「拖拽/点选」之外的另一条入口，不是页面主操作，
            不再单独占一行飘在右侧，中间夹一大块空白。 */}
        <PickFromLibraryButton onClick={() => setPickOpen(true)} />
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

/** 知识块预览弹窗：看这篇文档切出来的全部块——库里实际参与检索的文本。
 * 排查「AI 为什么这么答 / 为什么没命中」时从这里入手，不用对着块数黑盒猜。 */
function ChunksDialog({ doc, onOpenChange }: { doc: KbDocument; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const chunks = useDocumentChunks(doc.id)
  const list = chunks.data?.chunks ?? []
  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={t('plugin_kb.chunksTitle', { name: doc.title })}
      description={t('plugin_kb.chunksDesc', { count: doc.chunkCount, model: doc.model })}
      size="lg"
      footer={
        <Button variant="outline" onClick={() => onOpenChange(false)}>
          {t('common.close')}
        </Button>
      }
    >
      {chunks.isLoading ? (
        <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
          <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />
          {t('common.loading')}
        </div>
      ) : list.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">{t('plugin_kb.chunksEmpty')}</p>
      ) : (
        <ol className="flex max-h-[26rem] flex-col gap-2 overflow-y-auto pe-1">
          {list.map((c) => (
            <li key={c.id} className="flex gap-2.5 rounded-md border border-border p-2.5">
              <span className="num shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">#{c.seq}</span>
              <p className="min-w-0 flex-1 whitespace-pre-wrap break-words text-sm leading-relaxed">{c.content}</p>
            </li>
          ))}
        </ol>
      )}
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
    <ResponsiveDialog
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

/** 检索设置弹窗：阈值 / TopK / 追加风格要求。 */
function SettingsDialog({
  open,
  onOpenChange,
  initial,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  initial?: { threshold: number; topK: number; systemPrompt: string }
}) {
  const { t } = useTranslation()
  const save = useSaveKbSettings()
  const [threshold, setThreshold] = useState(String(initial?.threshold ?? ''))
  const [topK, setTopK] = useState(String(initial?.topK ?? ''))
  const [systemPrompt, setSystemPrompt] = useState(initial?.systemPrompt ?? '')
  const ready = threshold !== '' && topK !== ''

  const submit = async () => {
    try {
      await save.mutateAsync({ threshold: Number(threshold), topK: Number(topK), systemPrompt })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  return (
    <ResponsiveDialog
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
        <div className="grid gap-4 md:grid-cols-2">
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
        </div>
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
  // 状态筛选：ready/failed 走后端 ?status=；「需重新向量化」是「模型已变更」的推导态，
  // 后端没有这个状态值，拉全量后在前端过滤。
  const [statusFilter, setStatusFilter] = useState<'all' | 'ready' | 'failed' | 'reembed'>('all')
  const [selected, setSelected] = useState<string[]>([])
  // 表头排序：默认新文档在前（与后端回退一致）
  const { sort, desc, sortProp } = useHeaderSort('created_at', true)

  const docs = useDocuments(statusFilter === 'ready' || statusFilter === 'failed' ? statusFilter : undefined, sort, desc)
  const status = useKbStatus()
  const settings = useKbSettings()
  const remove = useDeleteDocument()
  const reembed = useReembedDocument()
  const ask = useAsk()

  const embedModel = status.data?.embedModel ?? ''
  const needReembed = (d: KbDocument) => d.status === 'failed' || (!!embedModel && !!d.model && d.model !== embedModel)
  const rows = useMemo(() => {
    const all = docs.data ?? []
    if (statusFilter === 'reembed') {
      return all.filter((d) => d.status === 'failed' || (!!embedModel && !!d.model && d.model !== embedModel))
    }
    return all
  }, [docs.data, statusFilter, embedModel])

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
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  // 批量删除：后端只有单条 DELETE，前端逐条循环；条数大时串行耗时可接受（文档量级小）
  const onBatchDelete = async (ids: string[]) => {
    const targets = (docs.data ?? []).filter((d) => ids.includes(d.id))
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
  }

  // 批量重新向量化：同样逐条调单文档接口，只挑真正「需重新向量化」的行
  const onBatchReembed = async (ids: string[]) => {
    const targets = (docs.data ?? []).filter((d) => ids.includes(d.id) && needReembed(d))
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

  // 统计条：文档 / 知识块 / 未就绪，数据都在现有接口里，前端汇总（量级小不做后端聚合）
  const stats = useMemo(() => {
    const all = docs.data ?? []
    const chunks = all.reduce((n, d) => n + d.chunkCount, 0)
    const failed = all.filter((d) => d.status !== 'ready').length
    return { docs: all.length, chunks, failed }
  }, [docs.data])

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
          <Badge variant={d.status === 'ready' ? 'success' : 'destructive'}>
            {t(`plugin_kb.status.${d.status}`)}
          </Badge>
          {d.error ? <span className="max-w-56 text-xs text-muted-foreground">{d.error}</span> : null}
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

      {docs.data ? (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <FileText className="size-3.5" aria-hidden="true" />
            {t('plugin_kb.statsDocs', { count: stats.docs })}
          </span>
          <span>{t('plugin_kb.statsChunks', { count: stats.chunks })}</span>
          {status.data?.embedModel ? <span>{t('plugin_kb.statsModel', { model: status.data.embedModel })}</span> : null}
          {stats.failed > 0 ? (
            <span className="text-warning">{t('plugin_kb.statsFailed', { count: stats.failed })}</span>
          ) : null}
        </div>
      ) : null}

      <DataTable
        columns={columns}
        data={rows}
        rowKey="id"
        loading={docs.isLoading}
        sort={sortProp}
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
                setStatusFilter(v as 'all' | 'ready' | 'failed' | 'reembed')
                setSelected([])
              }}
              options={[
                { value: 'all', label: t('plugin_kb.filterAll') },
                { value: 'ready', label: t('plugin_kb.status.ready') },
                { value: 'failed', label: t('plugin_kb.status.failed') },
                { value: 'reembed', label: t('plugin_kb.filterReembed') },
              ]}
            />
          </>
        }
      />

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
      {chunksDoc ? <ChunksDialog doc={chunksDoc} onOpenChange={(v) => !v && setChunksDoc(null)} /> : null}
      {renameDoc ? <RenameDialog doc={renameDoc} onOpenChange={(v) => !v && setRenameDoc(null)} /> : null}
      {settings.data ? (
        <SettingsDialog
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
          initial={{
            threshold: settings.data.threshold,
            topK: settings.data.topK,
            systemPrompt: settings.data.systemPrompt,
          }}
        />
      ) : null}
    </PageContainer>
  )
}
