import { useState } from 'react'
import { Check, Eye, EyeOff, Loader2, Pencil, RefreshCw, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Textarea } from '@/framework/ui/textarea'
import { Badge } from '@/framework/ui/badge'
import {
  useDocumentChunks,
  useRechunkDocument,
  useRechunkPreview,
  useToggleChunk,
  useUpdateChunk,
  type KbDocument,
} from './api'

interface Props {
  doc: KbDocument
  onOpenChange: (v: boolean) => void
}

/** 知识块运营弹窗：预览/应用重切，单块启停与编辑。 */
export default function ChunksManageDialog({ doc, onOpenChange }: Props) {
  const { t } = useTranslation()
  const chunks = useDocumentChunks(doc.id)
  const preview = useRechunkPreview()
  const apply = useRechunkDocument()
  const update = useUpdateChunk()
  const toggle = useToggleChunk()
  const [previewBlocks, setPreviewBlocks] = useState<string[] | null>(null)
  const [editing, setEditing] = useState<number | null>(null)
  const [draft, setDraft] = useState('')
  const stored = chunks.data?.chunks ?? []
  const list = previewBlocks
    ? previewBlocks.map((content, i) => ({ key: `p${i}`, seq: i + 1, content, enabled: true }))
    : stored.map((c) => ({ key: c.id, seq: c.seq, content: c.content, enabled: c.enabled !== false }))

  const onPreview = async () => {
    try {
      const res = await preview.mutateAsync(doc.id)
      setPreviewBlocks(res.chunks)
    } catch {
      // http 拦截器统一提示
    }
  }

  const onApply = async () => {
    try {
      await apply.mutateAsync(doc.id)
      toast.success(t('plugin_kb.rechunkDone', { count: previewBlocks?.length ?? 0 }))
      onOpenChange(false)
    } catch {
      // http 拦截器统一提示
    }
  }

  const onSave = async (seq: number) => {
    if (!draft.trim()) return
    if (draft.trim().length > 4000) {
      toast.warning(t('plugin_kb.chunkTooLong'))
      return
    }
    try {
      await update.mutateAsync({ id: doc.id, seq, content: draft.trim() })
      toast.success(t('plugin_kb.chunkUpdated'))
      setEditing(null)
    } catch {
      // http 拦截器统一提示
    }
  }

  const onToggle = async (seq: number, enabled: boolean) => {
    try {
      await toggle.mutateAsync({ id: doc.id, seq, enabled })
      toast.success(t('plugin_kb.chunkToggled'))
    } catch {
      // http 拦截器统一提示
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={t('plugin_kb.chunksTitle', { name: doc.title })}
      description={
        previewBlocks
          ? t('plugin_kb.rechunkPreviewDesc', { count: previewBlocks.length, current: doc.chunkCount })
          : t('plugin_kb.chunksDesc', { count: doc.chunkCount, model: doc.model })
      }
      size="lg"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.close')}</Button>
          {previewBlocks ? (
            <>
              <Button variant="ghost" onClick={() => setPreviewBlocks(null)} disabled={apply.isPending}>
                {t('plugin_kb.rechunkBack')}
              </Button>
              <Button onClick={() => void onApply()} disabled={apply.isPending}>
                {apply.isPending ? <Loader2 className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : null}
                {t('plugin_kb.rechunkApply', { count: previewBlocks.length })}
              </Button>
            </>
          ) : (
            <Button variant="outline" onClick={() => void onPreview()} disabled={preview.isPending || stored.length === 0}>
              {preview.isPending ? <Loader2 className="animate-spin motion-reduce:animate-none" aria-hidden="true" /> : <RefreshCw aria-hidden="true" />}
              {t('plugin_kb.rechunkPreview')}
            </Button>
          )}
        </>
      }
    >
      {previewBlocks ? (
        <div className="mb-3 rounded-md bg-warning/10 px-3 py-2 text-xs text-warning">{t('plugin_kb.rechunkWarn')}</div>
      ) : null}
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
            <li key={c.key} className={`flex flex-col gap-2 rounded-md border border-border p-2.5 ${c.enabled ? '' : 'opacity-60'}`}>
              <div className="flex items-start gap-2.5">
                <span className="num shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">#{c.seq}</span>
                <p className="min-w-0 flex-1 whitespace-pre-wrap break-words text-sm leading-relaxed">{c.content}</p>
                {!c.enabled ? <Badge variant="outline" className="shrink-0">{t('plugin_kb.chunkDisabled')}</Badge> : null}
              </div>
              {!previewBlocks ? (
                <div className="flex justify-end gap-1">
                  <Button variant="ghost" size="sm" title={t('plugin_kb.chunkEdit')}
                    onClick={() => { setEditing(c.seq); setDraft(c.content) }}>
                    <Pencil aria-hidden="true" />{t('plugin_kb.chunkEdit')}
                  </Button>
                  <Button variant="ghost" size="sm" title={t(c.enabled ? 'plugin_kb.chunkDisable' : 'plugin_kb.chunkEnable')}
                    disabled={toggle.isPending}
                    onClick={() => void onToggle(c.seq, !c.enabled)}>
                    {c.enabled ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
                    {t(c.enabled ? 'plugin_kb.chunkDisable' : 'plugin_kb.chunkEnable')}
                  </Button>
                </div>
              ) : null}
              {editing === c.seq ? (
                <div className="flex flex-col gap-2 rounded-md bg-muted/40 p-2">
                  <Textarea rows={5} value={draft} onChange={(e) => setDraft(e.target.value)} />
                  <p className="text-xs text-muted-foreground">{t('plugin_kb.chunkEditHint')}</p>
                  <div className="flex justify-end gap-1">
                    <Button variant="ghost" size="sm" onClick={() => setEditing(null)}><X aria-hidden="true" />{t('common.cancel')}</Button>
                    <Button size="sm" disabled={update.isPending || !draft.trim()} onClick={() => void onSave(c.seq)}>
                      <Check aria-hidden="true" />{t('common.save')}
                    </Button>
                  </div>
                </div>
              ) : null}
            </li>
          ))}
        </ol>
      )}
    </ResponsiveDialog>
  )
}
