import { useState, type FormEvent } from 'react'
import { Loader2, Sparkles, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAiStatus } from '@/framework/ai/use-ai-status'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { cn } from '@/lib/utils'

export interface AiFilterBoxProps {
  scope: 'user' | 'notice' | 'operLog'
  onApply: (filters: Record<string, string>) => void
  className?: string
}

interface FilterResult {
  filters: Record<string, string>
  explanation: string
}

/** 一句话 AI 筛选：AI 只产出结构化条件，由页面映射到自己的筛选字段后走正常查询。未启用 AI 时不渲染 */
export function AiFilterBox({ scope, onApply, className }: AiFilterBoxProps) {
  const { t } = useTranslation()
  const { enabled } = useAiStatus()
  const [text, setText] = useState('')
  const [loading, setLoading] = useState(false)
  const [chip, setChip] = useState<{ text: string; explanation: string } | null>(null)

  if (!enabled) return null

  const submit = async (e?: FormEvent) => {
    e?.preventDefault()
    const q = text.trim()
    if (!q || loading) return
    setLoading(true)
    try {
      const res = await api.post<FilterResult>('/ai/filter', { scope, text: q })
      if (Object.keys(res.filters ?? {}).length === 0) {
        setChip(null)
        toast.info(res.explanation)
        return
      }
      onApply(res.filters)
      setChip({ text: q, explanation: res.explanation })
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t('ai.filter.failed'))
    } finally {
      setLoading(false)
    }
  }

  const clear = () => {
    setChip(null)
    setText('')
    onApply({})
  }

  return (
    <div className={cn('flex w-full flex-col gap-2', className)}>
      <form
        onSubmit={submit}
        className="flex min-h-11 w-full items-center gap-2 rounded-full border border-input bg-background ps-4 pe-1.5 focus-within:ring-2 focus-within:ring-ring"
      >
        <Sparkles className="size-4 shrink-0 text-primary" aria-hidden="true" />
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={t('ai.filter.placeholder')}
          aria-label={t('ai.filter.label')}
          disabled={loading}
          className="min-w-0 flex-1 bg-transparent py-2 text-base outline-none placeholder:text-muted-foreground md:text-sm"
        />
        <Button type="submit" size="sm" className="shrink-0 rounded-full" disabled={loading || !text.trim()} aria-label={t('ai.filter.submit')}>
          {loading ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Sparkles className="size-4" aria-hidden="true" />}
          <span className="hidden sm:inline">{t('ai.filter.submit')}</span>
        </Button>
      </form>
      {chip ? (
        <div className="flex">
          <span className="inline-flex max-w-full items-center gap-1 rounded-full bg-secondary py-1 ps-3 pe-1 text-sm text-secondary-foreground">
            <span className="min-w-0 break-words">{t('ai.filter.applied', { text: chip.text, explanation: chip.explanation })}</span>
            <button
              type="button"
              onClick={clear}
              aria-label={t('ai.filter.clear')}
              className="inline-flex size-6 shrink-0 items-center justify-center rounded-full hover:bg-muted"
            >
              <X className="size-3.5" aria-hidden="true" />
            </button>
          </span>
        </div>
      ) : null}
    </div>
  )
}
