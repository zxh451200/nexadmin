import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/framework/ui/button'

/** 复制文本到剪贴板的图标按钮 */
export function CopyButton({ text, label }: { text: string; label?: string }) {
  const { t } = useTranslation()
  const [done, setDone] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setDone(true)
      toast.success(t('sso.copied'))
      window.setTimeout(() => setDone(false), 1500)
    } catch {
      toast.error(t('sso.copyFailed'))
    }
  }

  return (
    <Button type="button" variant="ghost" size="icon" aria-label={label ?? t('sso.copy')} onClick={copy}>
      {done ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
    </Button>
  )
}
