import { useEffect, useRef, useState } from 'react'
import { EditorContent, useEditor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import Underline from '@tiptap/extension-underline'
import Link from '@tiptap/extension-link'
import Image from '@tiptap/extension-image'
import { Table } from '@tiptap/extension-table'
import TableRow from '@tiptap/extension-table-row'
import TableHeader from '@tiptap/extension-table-header'
import TableCell from '@tiptap/extension-table-cell'
import TextAlign from '@tiptap/extension-text-align'
import { Color } from '@tiptap/extension-color'
import { TextStyle } from '@tiptap/extension-text-style'
import Highlight from '@tiptap/extension-highlight'
import Placeholder from '@tiptap/extension-placeholder'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Video } from './editor/video'
import { sanitizeHtml } from './editor/sanitize'
import { EditorToolbar } from './editor/EditorToolbar'
import { MediaDialog } from './editor/MediaDialog'
import './editor/richtext.css'

export interface RichTextEditorProps {
  /** HTML */
  value?: string | null
  /** 输出前经 DOMPurify 过滤；内容为空时输出空字符串 */
  onChange?: (html: string) => void
  placeholder?: string
  className?: string
  disabled?: boolean
}

export function RichTextEditor({ value, onChange, placeholder, className, disabled }: RichTextEditorProps) {
  const { t } = useTranslation()
  const [media, setMedia] = useState<'image' | 'video' | null>(null)
  const lastEmitted = useRef<string>(value ?? '')
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2, 3] } }),
      Underline,
      Link.configure({ openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer', target: '_blank' } }),
      Image,
      Video,
      Table.configure({ resizable: false }),
      TableRow,
      TableHeader,
      TableCell,
      TextAlign.configure({ types: ['heading', 'paragraph'] }),
      TextStyle,
      Color,
      Highlight.configure({ multicolor: true }),
      Placeholder.configure({ placeholder: placeholder ?? t('editor.placeholder') }),
    ],
    content: sanitizeHtml(value ?? ''),
    editable: !disabled,
    editorProps: { attributes: { class: 'nex-richtext', 'aria-label': t('editor.label') } },
    onUpdate: ({ editor: e }) => {
      const html = e.isEmpty ? '' : sanitizeHtml(e.getHTML())
      lastEmitted.current = html
      onChangeRef.current?.(html)
    },
  })

  // 外部 value 变化（如表单 reset / 异步回填）时同步到编辑器
  useEffect(() => {
    if (!editor) return
    const next = value ?? ''
    if (next !== lastEmitted.current) {
      lastEmitted.current = next
      editor.commands.setContent(sanitizeHtml(next), false)
    }
  }, [value, editor])

  useEffect(() => {
    editor?.setEditable(!disabled)
  }, [disabled, editor])

  if (!editor) return null

  return (
    <div className={cn('nex-richtext-editor overflow-hidden rounded-md border border-input bg-background focus-within:ring-2 focus-within:ring-ring', className)}>
      {!disabled && <EditorToolbar editor={editor} onMedia={setMedia} />}
      <EditorContent editor={editor} />
      <MediaDialog
        kind={media}
        onClose={() => setMedia(null)}
        onInsert={(kind, file) => {
          if (kind === 'image') editor.chain().focus().setImage({ src: file.url, alt: file.name }).run()
          else editor.chain().focus().setVideo({ src: file.url, poster: file.posterUrl }).run()
          setMedia(null)
        }}
      />
    </div>
  )
}
