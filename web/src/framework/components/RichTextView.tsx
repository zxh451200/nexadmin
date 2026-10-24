import { useMemo } from 'react'
import { cn } from '@/lib/utils'
import { sanitizeHtml } from './editor/sanitize'
import './editor/richtext.css'

export interface RichTextViewProps {
  html?: string | null
  className?: string
}

/** 只渲染 DOMPurify 过滤后的 HTML */
export function RichTextView({ html, className }: RichTextViewProps) {
  const safe = useMemo(() => sanitizeHtml(html ?? ''), [html])
  return <div className={cn('nex-richtext', className)} dangerouslySetInnerHTML={{ __html: safe }} />
}
