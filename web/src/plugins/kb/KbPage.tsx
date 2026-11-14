import { useState } from 'react'
import { AlertTriangle, BotMessageSquare, RefreshCw, Trash2, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { FileDropField } from '@/framework/components/FileDropField'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Textarea } from '@/framework/ui/textarea'
import {
  uploadDocument,
  useAsk,
  useDeleteDocument,
  useDocuments,
  useKbSettings,
  useKbStatus,
  useReembedDocument,
  useSaveKbSettings,
  type KbDocument,
} from './api'

/** 上传弹窗：一期只收 txt/md（≤2MB），同步切分并向量化，成功即入列。 */
function UploadDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<File[]>([])
  const [busy, setBusy] = useState(false)
  const ready = files.length > 0 && !busy

  const submit = async () => {
    if (!ready) return
    setBusy(true)
    let ok = 0
    let fail = 0
    for (const file of files) {
      try {
        const doc = await uploadDocument(file)
        ok++
        toast.success(t('plugin_kb.uploadDone', { name: doc.title, count: doc.chunkCount }))
      } catch (e) {
        fail++
        toast.error(`${file.name}: ${(e as Error).message}`)
      }
    }
    setBusy(false)
    if (ok > 0) {
      setFiles([])
      onOpenChange(false)
    } else {
      toast.error(t('plugin_kb.uploadAllFailed', { fail }))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setFiles([])
        onOpenChange(o)
      }}
      title={t('plugin_kb.uploadTitle')}
      description={t('plugin_kb.uploadDesc')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={!ready}>
            <Upload className="me-2 size-4" aria-hidden="true" />
            {t('plugin_kb.upload')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <FileDropField
          multiple
          accept=".txt,.md,.markdown,text/plain,text/markdown"
          files={files}
          onChange={setFiles}
        />
        <p className="text-xs text-muted-foreground">{t('plugin_kb.uploadHint')}</p>
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
    } catch (e) {
      toast.error((e as Error).message)
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
  const [question, setQuestion] = useState('')

  const docs = useDocuments()
  const status = useKbStatus()
  const settings = useKbSettings()
  const remove = useDeleteDocument()
  const reembed = useReembedDocument()
  const ask = useAsk()

  const embedModel = status.data?.embedModel ?? ''
  const needReembed = (d: KbDocument) => d.status === 'failed' || (!!embedModel && !!d.model && d.model !== embedModel)

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
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const onReembed = async (d: KbDocument) => {
    try {
      await reembed.mutateAsync(d.id)
      toast.success(t('plugin_kb.reembedDone', { name: d.title }))
    } catch (e) {
      toast.error((e as Error).message)
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
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const columns: Column<KbDocument>[] = [
    {
      key: 'title',
      title: t('plugin_kb.fields.title'),
      mobile: 'title',
      render: (d) => (
        <div className="min-w-0">
          <span className="block max-w-[20rem] truncate font-medium text-foreground" title={d.title}>
            {d.title}
          </span>
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
      render: (d) => <span className="num text-xs text-muted-foreground">{formatTime(d.createdAt, i18n.language)}</span>,
    },
    {
      key: 'actions',
      title: t('plugin_kb.fields.actions'),
      render: (d) => (
        <div className="flex items-center gap-1">
          <Can perm="kb:edit">
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
      <PageHeader
        title={t('plugin_kb.title')}
        description={t('plugin_kb.description')}
        actions={
          <>
            <Can perm="kb:edit">
              <Button variant="outline" onClick={() => setSettingsOpen(true)}>
                <BotMessageSquare aria-hidden="true" />
                {t('plugin_kb.settingsTitle')}
              </Button>
              <Button onClick={() => setUploadOpen(true)}>
                <Upload className="me-2 size-4" aria-hidden="true" />
                {t('plugin_kb.upload')}
              </Button>
            </Can>
          </>
        }
      />

      {status.data && (!status.data.embedReady || !status.data.chatReady) ? (
        <div className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm text-warning-foreground">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>{status.data.embedReady ? t('plugin_kb.missingChat') : t('plugin_kb.missingEmbed')}</span>
        </div>
      ) : null}

      <DataTable
        columns={columns}
        data={docs.data ?? []}
        rowKey="id"
        loading={docs.isLoading}
        columnSettingsKey="plugin-kb-documents"
        emptyText={t('plugin_kb.empty')}
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
          ) : null}
        </div>
      </Can>

      <UploadDialog open={uploadOpen} onOpenChange={setUploadOpen} />
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
