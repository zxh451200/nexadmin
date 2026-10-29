import { useState } from 'react'
import { Check, Copy, Loader2, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAiStatus } from '@/framework/ai/use-ai-status'
import { Button } from '@/framework/ui/button'
import { cn } from '@/lib/utils'
import { useLocaleForm } from './LocaleFormProvider'

/** 译文态下的批量动作。填空白只写仍为空的字段，绝不覆盖已翻译内容。 */
export function LocaleFormActions({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { enabled: aiEnabled } = useAiStatus()
  const {
    isSource, active, schema, field, source, translate, fillBlankFromSource, applyBatch,
    reviewOf, markMachine, confirmReview,
  } = useLocaleForm()
  const [busy, setBusy] = useState(false)

  if (isSource) return null
  const machine = reviewOf(active)
  const blanks = schema.translatable.filter((f) => !(field(f.key) ?? '').trim() && (source(f.key) ?? '').trim())
  if (!blanks.length && !machine) return null

  const runAi = async () => {
    if (!translate) return
    setBusy(true)
    const map: Record<string, string> = {}
    try {
      // Format 是请求级的，所以纯文本一批、富文本一批，各调一次。
      for (const format of ['text', 'html'] as const) {
        const group = blanks.filter((f) => (f.widget === 'rich') === (format === 'html'))
        if (!group.length) continue
        const out = await translate(group.map((f) => source(f.key)), active, format)
        group.forEach((f, i) => {
          const v = out[i]
          if (v && v.trim()) map[f.key] = v
        })
      }
      applyBatch(map)
      const count = Object.keys(map).length
      if (count) {
        // 机器翻完不等于可以上线：记下来，等人工确认后再清掉。
        markMachine(active)
        toast.success(t('localeForm.aiDone', { count }))
      } else {
        toast.warning(t('localeForm.aiEmpty'))
      }
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t('localeForm.aiFailed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={cn('flex flex-wrap items-center gap-2', className)}>
      {blanks.length ? (
        <>
          <Button type="button" variant="outline" size="sm" onClick={fillBlankFromSource}>
            <Copy aria-hidden="true" />
            {t('localeForm.fillFromSource')}
          </Button>
          {translate && aiEnabled ? (
            <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void runAi()}>
              {busy ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Sparkles aria-hidden="true" />}
              {busy ? t('localeForm.aiWorking') : t('localeForm.aiFill')}
            </Button>
          ) : null}
        </>
      ) : null}
      {machine ? (
        <>
          <span className="text-xs text-muted-foreground">{t('localeForm.machineHint')}</span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              confirmReview(active)
              toast.success(t('localeForm.machineConfirmed'))
            }}
          >
            <Check aria-hidden="true" />
            {t('localeForm.machineConfirm')}
          </Button>
        </>
      ) : null}
    </div>
  )
}
