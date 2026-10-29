import { useState } from 'react'
import { Languages, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAiStatus } from '@/framework/ai/use-ai-status'
import { useLanguages } from '@/framework/i18n/useLanguages'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { cn } from '@/lib/utils'

export interface AiTranslateButtonProps {
  getText: () => string
  format: 'text' | 'html'
  onResult: (translated: string, toLang: string) => void
  className?: string
}

/**
 * AI 翻译助手：选目标语言（排除当前语言）后调用 /ai/translate。未启用 AI 时不渲染。
 *
 * onResult 拿到的是「译文本身」：把它拼进原文字段（`原 / 译文`）是给单列字段硬塞双语的旧写法，
 * 已经不再使用。多语言内容请走框架的语言表单（framework/components/i18n），
 * 译文落到 i18n_json 文档里，每个语言各自一列，公开侧才能按语言取用与发布。
 */
export function AiTranslateButton({ getText, format, onResult, className }: AiTranslateButtonProps) {
  const { t, i18n } = useTranslation()
  const { enabled } = useAiStatus()
  const { languages } = useLanguages()
  const [loading, setLoading] = useState(false)

  if (!enabled) return null

  const from = i18n.language
  const targets = languages.filter((l) => l.code !== from)

  const run = async (to: string) => {
    const text = getText()
    if (!text.trim()) {
      toast.warning(t('ai.translate.empty'))
      return
    }
    setLoading(true)
    try {
      const res = await api.post<{ text: string }>('/ai/translate', { text, from, to, format })
      onResult(res.text, to)
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t('ai.translate.failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="outline" size="sm" className={cn('shrink-0', className)} disabled={loading}>
          {loading ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Languages className="size-4 text-primary" aria-hidden="true" />}
          {t('ai.translate.button')}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t('ai.translate.menuLabel')}</DropdownMenuLabel>
        {targets.length === 0 ? (
          <DropdownMenuItem disabled>{t('ai.translate.noLanguage')}</DropdownMenuItem>
        ) : (
          targets.map((l) => (
            <DropdownMenuItem key={l.code} onSelect={() => run(l.code)}>
              {l.name}
            </DropdownMenuItem>
          ))
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
