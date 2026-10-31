import { useEffect, useRef } from 'react'
import { EditorState, type Extension } from '@codemirror/state'
import { EditorView, highlightSpecialChars, keymap, lineNumbers } from '@codemirror/view'
import { HighlightStyle, foldGutter, foldKeymap, syntaxHighlighting } from '@codemirror/language'
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search'
import { go } from '@codemirror/lang-go'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { sql } from '@codemirror/lang-sql'
import { tags as t } from '@lezer/highlight'
import type { GenFile } from './api'

/** 语法配色全部取自主题令牌，浅色/深色随界面切换，不引入编辑器自带主题。 */
const codeHighlight = HighlightStyle.define([
  { tag: [t.comment, t.lineComment, t.blockComment], color: 'hsl(var(--muted-foreground))', fontStyle: 'italic' },
  {
    tag: [t.keyword, t.modifier, t.controlKeyword, t.definitionKeyword, t.moduleKeyword, t.operatorKeyword],
    color: 'hsl(var(--chart-1))',
  },
  { tag: [t.string, t.special(t.string), t.regexp], color: 'hsl(var(--success))' },
  { tag: [t.number, t.bool, t.null, t.atom], color: 'hsl(var(--warning))' },
  { tag: [t.typeName, t.className, t.namespace], color: 'hsl(var(--chart-3))' },
  { tag: [t.function(t.variableName), t.function(t.propertyName)], color: 'hsl(var(--info))' },
  { tag: [t.tagName], color: 'hsl(var(--chart-1))' },
  { tag: [t.attributeName, t.propertyName], color: 'hsl(var(--chart-2))' },
  { tag: [t.meta, t.annotation, t.documentMeta], color: 'hsl(var(--muted-foreground))' },
  { tag: t.invalid, color: 'hsl(var(--destructive))' },
])

const codeTheme = EditorView.theme({
  '&': { backgroundColor: 'transparent', color: 'hsl(var(--foreground))' },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': {
    fontFamily: "'JetBrains Mono Variable', ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: '12px',
    lineHeight: '1.6',
    maxHeight: '60dvh',
    padding: '0.75rem 0',
    direction: 'ltr',
  },
  '.cm-line': { padding: '0 0.75rem' },
  '.cm-gutters': {
    backgroundColor: 'hsl(var(--muted))',
    borderRight: '1px solid hsl(var(--border))',
    color: 'hsl(var(--muted-foreground))',
  },
  '.cm-lineNumbers .cm-gutterElement': { padding: '0 0.5rem 0 0.75rem', minWidth: '2.5ch' },
  '.cm-foldGutter .cm-gutterElement': { color: 'hsl(var(--muted-foreground))' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'hsl(var(--primary) / 0.22)' },
  '.cm-searchMatch': { backgroundColor: 'hsl(var(--warning) / 0.25)', outline: '1px solid hsl(var(--warning) / 0.5)' },
  '.cm-panels': { backgroundColor: 'hsl(var(--muted))', color: 'hsl(var(--foreground))' },
  '.cm-panels.cm-panels-bottom': { borderTop: '1px solid hsl(var(--border))' },
  '.cm-foldPlaceholder': {
    backgroundColor: 'hsl(var(--muted))',
    border: '1px solid hsl(var(--border))',
    color: 'hsl(var(--muted-foreground))',
  },
})

function languageFor(lang: GenFile['lang']): Extension {
  switch (lang) {
    case 'go':
      return go()
    case 'ts':
      return javascript({ typescript: true })
    case 'tsx':
      return javascript({ typescript: true, jsx: true })
    case 'json':
      return json()
    case 'sql':
      return sql()
  }
}

function createState(content: string, lang: GenFile['lang']): EditorState {
  return EditorState.create({
    doc: content,
    extensions: [
      lineNumbers(),
      foldGutter(),
      highlightSpecialChars(),
      search(),
      highlightSelectionMatches(),
      keymap.of([...searchKeymap, ...foldKeymap]),
      EditorState.readOnly.of(true),
      // 代码恒为从左到右，不随界面 RTL 翻转
      EditorView.contentAttributes.of({ dir: 'ltr', spellcheck: 'false' }),
      syntaxHighlighting(codeHighlight),
      codeTheme,
      languageFor(lang),
    ],
  })
}

interface Props {
  content: string
  lang: GenFile['lang']
}

/** 只读代码视图（CodeMirror 6）：行号、折叠、Ctrl+F 搜索，选中可复制。 */
export function CodeView({ content, lang }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)

  useEffect(() => {
    if (!hostRef.current) return
    if (viewRef.current) {
      viewRef.current.setState(createState(content, lang))
      return
    }
    viewRef.current = new EditorView({ state: createState(content, lang), parent: hostRef.current })
  }, [content, lang])

  useEffect(
    () => () => {
      viewRef.current?.destroy()
      viewRef.current = null
    },
    [],
  )

  return <div ref={hostRef} className="min-w-0" />
}
