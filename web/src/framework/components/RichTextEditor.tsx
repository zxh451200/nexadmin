import { useEffect, useRef, useState } from 'react'
import { EditorContent, useEditor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import Image from '@tiptap/extension-image'
import { Table, TableRow, TableHeader, TableCell } from '@tiptap/extension-table'
import TextAlign from '@tiptap/extension-text-align'
import { TextStyle, Color } from '@tiptap/extension-text-style'
import Highlight from '@tiptap/extension-highlight'
import { Placeholder } from '@tiptap/extensions'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Video } from './editor/video'
import { Embed } from './editor/embed'
import { normalizeEmbedUrl } from './editor/sanitize'
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
      // 放开到 h6：工具栏只给 h1–h3 按钮，但粘贴/导入进来的 h4–h6 不能被降级成段落。
      // v3 的 StarterKit 自带 Link / Underline，链接配置挪进 configure。
      StarterKit.configure({
        heading: { levels: [1, 2, 3, 4, 5, 6] },
        link: { openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer', target: '_blank' } },
      }),
      Image,
      Video,
      Embed,
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
    // 工具栏的 isActive/can 依赖每次事务后的重渲染；v3 默认关闭
    shouldRerenderOnTransaction: true,
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
      editor.commands.setContent(sanitizeHtml(next), { emitUpdate: false })
    }
  }, [value, editor])

  useEffect(() => {
    editor?.setEditable(!disabled)
  }, [disabled, editor])

  if (!editor) return null

  return (
    <div className={cn('nex-richtext-editor min-w-0 overflow-hidden rounded-md border border-input bg-background focus-within:ring-2 focus-within:ring-ring', className)}>
      {!disabled && <EditorToolbar editor={editor} onMedia={setMedia} />}
      <EditorContent editor={editor} />
      <MediaDialog
        kind={media}
        onClose={() => setMedia(null)}
        onInsert={(kind, file, alt) => {
          if (kind === 'image') {
            editor.chain().focus().setImage({ src: file.url, alt: alt || file.name }).run()
          } else {
            // B 站/YouTube 链接规范成嵌入地址；其它地址仍按原生 <video> 插入。
            const embed = normalizeEmbedUrl(file.url)
            if (embed) editor.chain().focus().setEmbed({ src: embed }).run()
            else editor.chain().focus().setVideo({ src: file.url, poster: file.posterUrl }).run()
          }
          setMedia(null)
        }}
      />
    </div>
  )
}
