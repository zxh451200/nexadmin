import { useState } from 'react'
import { Loader2, PenLine } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAiStatus } from '@/framework/ai/use-ai-status'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { cn } from '@/lib/utils'

type Tone = 'default' | 'formal' | 'friendly' | 'concise'
const TONES: Tone[] = ['default', 'formal', 'friendly', 'concise']

export interface AiWriteButtonProps {
  /** title：单行标题；body：正文 */
  target: 'title' | 'body'
  /** body 的 html 表示富文本（润色时保留原有标签，起草时由服务端包 <p>） */
  format: 'text' | 'html'
  /** 当前字段内容；润色用它，起草时把它当要点 */
  getText: () => string
  /** 起草时当前字段为空则用它当要点（如正文为空时用标题） */
  getBrief?: () => string
  onResult: (text: string) => void
  className?: string
}

/** 富文本转纯文本，供“按要点起草”当要点用 */
function htmlToText(html: string): string {
  const doc = new DOMParser().parseFromString(html.replace(/<\/(p|div|li|h[1-6])>|<br\s*\/?>/gi, '\n'), 'text/html')
  return (doc.body.textContent ?? '').replace(/\n{2,}/g, '\n').trim()
}

/**
 * AI 写作 / 润色助手：按要点起草或润色已有内容，结果替换当前字段，并给出“撤销”。
 * 权限沿用 AI 本身（登录即可、租户可用、未被关闭）：未启用 AI 或租户被关闭 AI 时不渲染；
 * 次数超限、被关闭等错误直接展示服务端已本地化的文案。
 */
export function AiWriteButton({ target, format, getText, getBrief, onResult, className }: AiWriteButtonProps) {
  const { t, i18n } = useTranslation()
  const { enabled } = useAiStatus()
  const [loading, setLoading] = useState(false)
  const [tone, setTone] = useState<Tone>('default')

  if (!enabled) return null

  const run = async (mode: 'write' | 'polish') => {
    const before = getText()
    const plain = format === 'html' ? htmlToText(before) : before.trim()
    let text = mode === 'polish' ? before : plain
    if (mode === 'write' && !text) text = (getBrief ? htmlToText(getBrief()) : '').trim()
    if (!text.trim()) {
      toast.warning(t(target === 'body' && mode === 'write' ? 'ai.write.emptyBody' : 'ai.write.empty'))
      return
    }
    setLoading(true)
    try {
      const res = await api.post<{ text: string }>(
        '/ai/write',
        { mode, target, text, format: target === 'title' ? 'text' : format, tone: tone === 'default' ? '' : tone, lang: mode === 'write' ? i18n.language : '' },
        { silent: true },
      )
      onResult(res.text)
      toast.success(t('ai.write.done'), before.trim() ? { action: { label: t('ai.write.undo'), onClick: () => onResult(before) } } : undefined)
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t('ai.write.failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="outline" size="sm" className={cn('shrink-0', className)} disabled={loading}>
          {loading ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <PenLine className="size-4 text-primary" aria-hidden="true" />}
          {t('ai.write.button')}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t('ai.write.menuLabel')}</DropdownMenuLabel>
        <DropdownMenuItem onSelect={() => run('write')}>{t('ai.write.draft')}</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => run('polish')}>{t('ai.write.polish')}</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>{t('ai.write.toneLabel')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={tone} onValueChange={(v) => setTone(v as Tone)}>
          {TONES.map((k) => (
            <DropdownMenuRadioItem key={k} value={k} onSelect={(e) => e.preventDefault()}>
              {t(`ai.write.tone.${k}`)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
