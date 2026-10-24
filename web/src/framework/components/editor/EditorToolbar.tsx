import { useState } from 'react'
import type { Editor } from '@tiptap/react'
import {
  AlignCenter, AlignJustify, AlignLeft, AlignRight, Bold, Code2, Heading1, Heading2, Heading3, Highlighter, ImageIcon, Italic, Link2, List, ListOrdered,
  Palette, Quote, Redo2, Strikethrough, Table2, Underline as UnderlineIcon, Undo2, VideoIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/framework/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { PRESET_COLORS } from '@/framework/components/pickers/constants'

interface ToolbarProps {
  editor: Editor
  onMedia: (kind: 'image' | 'video') => void
}

function Btn({ label, active, disabled, onClick, children }: { label: string; active?: boolean; disabled?: boolean; onClick?: () => void; children: React.ReactNode }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      aria-pressed={active}
      disabled={disabled}
      onClick={onClick}
      className={cn('shrink-0', active && 'bg-muted text-primary')}
    >
      {children}
    </Button>
  )
}

function ColorBtn({ label, icon, onPick, onClear }: { label: string; icon: React.ReactNode; onPick: (c: string) => void; onClear: () => void }) {
  const { t } = useTranslation()
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="ghost" size="icon" aria-label={label} title={label} className="shrink-0">
          {icon}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-3">
        <div className="grid grid-cols-7 gap-2">
          {PRESET_COLORS.map((c) => (
            <button key={c} type="button" aria-label={c} style={{ backgroundColor: c }} className="size-8 rounded-md border border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => onPick(c)} />
          ))}
        </div>
        <Button type="button" variant="outline" size="sm" className="mt-3 w-full" onClick={onClear}>
          {t('common.clear')}
        </Button>
      </PopoverContent>
    </Popover>
  )
}

function LinkBtn({ editor }: { editor: Editor }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState('')
  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) setUrl((editor.getAttributes('link').href as string | undefined) ?? '')
      }}
    >
      <PopoverTrigger asChild>
        <Button type="button" variant="ghost" size="icon" aria-label={t('editor.link')} title={t('editor.link')} aria-pressed={editor.isActive('link')} className={cn('shrink-0', editor.isActive('link') && 'bg-muted text-primary')}>
          <Link2 className="size-4" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 space-y-2 p-3">
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://"
          aria-label={t('editor.link')}
          className="h-11 w-full rounded-md border border-input bg-background px-3 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-9 md:text-sm"
        />
        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              editor.chain().focus().extendMarkRange('link').unsetLink().run()
              setOpen(false)
            }}
          >
            {t('editor.unlink')}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!/^(https?:\/\/|\/|mailto:)/i.test(url)}
            onClick={() => {
              editor.chain().focus().extendMarkRange('link').setLink({ href: url }).run()
              setOpen(false)
            }}
          >
            {t('common.confirm')}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

export function EditorToolbar({ editor, onMedia }: ToolbarProps) {
  const { t } = useTranslation()
  const c = () => editor.chain().focus()
  const sep = <span className="mx-1 h-6 w-px shrink-0 bg-border" aria-hidden="true" />
  const ic = 'size-4'
  return (
    <div role="toolbar" aria-label={t('editor.toolbar')} className="flex items-center gap-0.5 overflow-x-auto border-b border-border bg-muted/40 p-1">
      <Btn label={t('editor.undo')} disabled={!editor.can().undo()} onClick={() => c().undo().run()}><Undo2 className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.redo')} disabled={!editor.can().redo()} onClick={() => c().redo().run()}><Redo2 className={ic} aria-hidden="true" /></Btn>
      {sep}
      <Btn label={t('editor.h1')} active={editor.isActive('heading', { level: 1 })} onClick={() => c().toggleHeading({ level: 1 }).run()}><Heading1 className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.h2')} active={editor.isActive('heading', { level: 2 })} onClick={() => c().toggleHeading({ level: 2 }).run()}><Heading2 className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.h3')} active={editor.isActive('heading', { level: 3 })} onClick={() => c().toggleHeading({ level: 3 }).run()}><Heading3 className={ic} aria-hidden="true" /></Btn>
      {sep}
      <Btn label={t('editor.bold')} active={editor.isActive('bold')} onClick={() => c().toggleBold().run()}><Bold className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.italic')} active={editor.isActive('italic')} onClick={() => c().toggleItalic().run()}><Italic className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.underline')} active={editor.isActive('underline')} onClick={() => c().toggleUnderline().run()}><UnderlineIcon className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.strike')} active={editor.isActive('strike')} onClick={() => c().toggleStrike().run()}><Strikethrough className={ic} aria-hidden="true" /></Btn>
      <ColorBtn label={t('editor.textColor')} icon={<Palette className={ic} aria-hidden="true" />} onPick={(col) => c().setColor(col).run()} onClear={() => c().unsetColor().run()} />
      <ColorBtn label={t('editor.highlight')} icon={<Highlighter className={ic} aria-hidden="true" />} onPick={(col) => c().toggleHighlight({ color: col }).run()} onClear={() => c().unsetHighlight().run()} />
      {sep}
      <Btn label={t('editor.bulletList')} active={editor.isActive('bulletList')} onClick={() => c().toggleBulletList().run()}><List className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.orderedList')} active={editor.isActive('orderedList')} onClick={() => c().toggleOrderedList().run()}><ListOrdered className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.quote')} active={editor.isActive('blockquote')} onClick={() => c().toggleBlockquote().run()}><Quote className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.codeBlock')} active={editor.isActive('codeBlock')} onClick={() => c().toggleCodeBlock().run()}><Code2 className={ic} aria-hidden="true" /></Btn>
      {sep}
      <Btn label={t('editor.alignLeft')} active={editor.isActive({ textAlign: 'left' })} onClick={() => c().setTextAlign('left').run()}><AlignLeft className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.alignCenter')} active={editor.isActive({ textAlign: 'center' })} onClick={() => c().setTextAlign('center').run()}><AlignCenter className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.alignRight')} active={editor.isActive({ textAlign: 'right' })} onClick={() => c().setTextAlign('right').run()}><AlignRight className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.alignJustify')} active={editor.isActive({ textAlign: 'justify' })} onClick={() => c().setTextAlign('justify').run()}><AlignJustify className={ic} aria-hidden="true" /></Btn>
      {sep}
      <LinkBtn editor={editor} />
      <Btn label={t('editor.insertImage')} onClick={() => onMedia('image')}><ImageIcon className={ic} aria-hidden="true" /></Btn>
      <Btn label={t('editor.insertVideo')} onClick={() => onMedia('video')}><VideoIcon className={ic} aria-hidden="true" /></Btn>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" variant="ghost" size="icon" aria-label={t('editor.table')} title={t('editor.table')} className={cn('shrink-0', editor.isActive('table') && 'bg-muted text-primary')}>
            <Table2 className={ic} aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => c().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()}>{t('editor.insertTable')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!editor.can().addColumnAfter()} onSelect={() => c().addColumnAfter().run()}>{t('editor.addColumn')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!editor.can().addRowAfter()} onSelect={() => c().addRowAfter().run()}>{t('editor.addRow')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!editor.can().deleteColumn()} onSelect={() => c().deleteColumn().run()}>{t('editor.deleteColumn')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!editor.can().deleteRow()} onSelect={() => c().deleteRow().run()}>{t('editor.deleteRow')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!editor.can().mergeOrSplit()} onSelect={() => c().mergeOrSplit().run()}>{t('editor.mergeSplit')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!editor.can().deleteTable()} onSelect={() => c().deleteTable().run()}>{t('editor.deleteTable')}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
