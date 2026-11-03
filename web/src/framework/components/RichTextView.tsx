import { useMemo, useRef, useState } from 'react'
import { cn } from '@/lib/utils'
import { Lightbox, type LightboxSlide } from '@/framework/upload/Lightbox'
import { sanitizeHtml } from './editor/sanitize'
import './editor/richtext.css'

export interface RichTextViewProps {
  html?: string | null
  className?: string
}

/** 渲染 DOMPurify 过滤后的 HTML；正文里的图片点开即大图，多张可左右翻页 */
export function RichTextView({ html, className }: RichTextViewProps) {
  const safe = useMemo(() => sanitizeHtml(html ?? ''), [html])
  const containerRef = useRef<HTMLDivElement>(null)
  const [viewer, setViewer] = useState<{ slides: LightboxSlide[]; index: number } | null>(null)

  return (
    <>
      <div
        ref={containerRef}
        className={cn('nex-richtext nex-richtext-view', className)}
        dangerouslySetInnerHTML={{ __html: safe }}
        onClick={(e) => {
          const box = containerRef.current
          if (!box) return
          const imgs = Array.from(box.querySelectorAll('img'))
          const index = imgs.indexOf(e.target as HTMLImageElement)
          if (index < 0) return
          setViewer({
            slides: imgs.map((img) => ({ src: img.src, alt: img.alt })),
            index,
          })
        }}
      />
      {viewer ? (
        <Lightbox
          open
          onOpenChange={(open) => {
            if (!open) setViewer(null)
          }}
          slides={viewer.slides}
          index={viewer.index}
        />
      ) : null}
    </>
  )
}
