import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, X } from 'lucide-react'
import { AiWriteButton } from '@/framework/components/AiWriteButton'
import { RichTextEditor } from '@/framework/components/RichTextEditor'
import { RichTextView } from '@/framework/components/RichTextView'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Textarea } from '@/framework/ui/textarea'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/framework/ui/collapsible'
import { cn } from '@/lib/utils'
import { FieldHint } from '@/framework/components/FieldHint'
import { useLocaleForm } from './LocaleFormProvider'
import { SourceHint } from './SourceHint'

export interface LocalizedFieldProps {
  /** 必须是实体 schema 里声明过的可翻译字段，否则不渲染 */
  fieldKey: string
  placeholder?: string
  /** 星号只在原文语言下出现：译文留空是合法状态，渲染时回退原文 */
  required?: boolean
  /** 覆盖该字段的 AI 动作类型；缺省按控件形态推断（text→title，其余→body） */
  aiTarget?: 'title' | 'body' | 'keywords'
  /** 多词枚举字段（如搜索关键词）的标签形态：芯片嵌在输入框里，max 为词数上限 */
  tags?: { max: number }
  /** 挂在标签尾部「?」上的长说明；直接铺在字段下会和标签抢层级 */
  hint?: string
  className?: string
}

/** 多词拆分口径：空格/中英文逗号/顿号/分号都算分隔符，忽略大小写去重——与各实体后端的 normalize 同源。 */
const WORD_SEPARATORS = /[ \t\n\r,，、;；]+/

function splitWords(s: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of s.split(WORD_SEPARATORS)) {
    const f = raw.trim()
    if (!f) continue
    const k = f.toLowerCase()
    if (seen.has(k)) continue
    seen.add(k)
    out.push(f)
  }
  return out
}

/**
 * 标签形态的多词输入：芯片嵌在输入框里，空格/回车/逗号把已输入的词收进芯片，空值 Backspace 删最后一个，
 * 芯片尾部 × 删除。底层值仍是分隔符拼接的字符串——与存量数据、AI 输出互通，只是交互以词为单位。
 */
function TagsInput({
  id, value, onChange, max, placeholder, ariaLabel, disabled,
}: {
  id?: string
  value: string
  onChange: (v: string) => void
  max?: number
  placeholder?: string
  ariaLabel?: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const words = useMemo(() => splitWords(value), [value])
  const [draft, setDraft] = useState('')
  // draft 的 ref 镜像：blur 回调可能拿到过期闭包（onChange 触发的重渲染还没落场），收词以 ref 为准
  const draftRef = useRef('')
  const updateDraft = (v: string) => {
    draftRef.current = v
    setDraft(v)
  }
  const atLimit = max !== undefined && words.length >= max
  const over = max !== undefined && words.length > max
  // 已输入内容里出现分隔符即收词（覆盖空格/逗号/顿号/分号与粘贴多词）；与已有词忽略大小写去重。
  const commit = (raw: string) => {
    updateDraft('')
    if (!splitWords(raw).length) return
    onChange(splitWords(`${value} ${raw}`).join(' '))
  }
  const remove = (w: string) => onChange(words.filter((x) => x !== w).join(' '))
  return (
    <div className="flex flex-col gap-1">
      <div
        className={cn(
          // 高度对齐普通 Input（h-10 / md:h-8）：py-1 只留一行余量，芯片不把框撑高
          'flex min-h-10 w-full flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-3 py-1 text-base shadow-xs',
          'transition-[color,box-shadow,border-color] duration-(--motion-fast) ease-(--ease-standard)',
          'focus-within:border-ring focus-within:outline-hidden focus-within:ring-2 focus-within:ring-ring/40',
          'dark:border-white/26 dark:bg-white/[0.06] md:min-h-8 md:text-sm',
          disabled && 'cursor-not-allowed opacity-50',
        )}
        onClick={() => inputRef.current?.focus()}
      >
        {words.map((w) => (
          /* 芯片收薄到 22px（py-0 + 20px 删除钮 + 描边）：md:size-5 显式压掉 icon 按钮的 md:h-9，
             否则 36px 的删除钮会把整框撑到 ~50px，比普通 Input 高出一截 */
          <Badge key={w.toLowerCase()} variant="secondary" className="gap-0.5 py-0 ps-2 pe-1">
            <span className="max-w-40 truncate">{w}</span>
            <Button
              variant="ghost"
              size="icon"
              className="size-5 md:size-5 [&_svg]:size-3"
              disabled={disabled}
              aria-label={t('localeForm.tagsRemove', { word: w })}
              onClick={() => remove(w)}
            >
              <X aria-hidden="true" />
            </Button>
          </Badge>
        ))}
        <input
          ref={inputRef}
          id={id}
          value={draft}
          maxLength={64}
          disabled={disabled || atLimit}
          onChange={(e) => {
            const next = e.target.value
            if (WORD_SEPARATORS.test(next)) commit(next)
            else updateDraft(next)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              commit(draftRef.current)
            } else if (e.key === 'Backspace' && !draft && words.length) {
              remove(words[words.length - 1])
            }
          }}
          // 失焦也收词：直接点保存时来不及回车，输入到一半的词不能丢
          onBlur={() => commit(draftRef.current)}
          className="min-w-16 flex-1 bg-transparent outline-hidden placeholder:text-muted-foreground"
          placeholder={words.length ? '' : placeholder}
          aria-label={ariaLabel}
        />
      </div>
      {over ? (
        <span className="text-xs text-warning">{t('localeForm.tagsOver', { count: words.length, max })}</span>
      ) : null}
    </div>
  )
}

/** 可翻译字段的唯一入口：标签 + 控件 + 原文对照。切语言换的是数据源，不是表单结构。 */
export function LocalizedField({ fieldKey, placeholder, required, aiTarget, tags, hint, className }: LocalizedFieldProps) {
  const { t } = useTranslation()
  const { spec, field, setField, source, isSource, fieldLabel, active, markMachine } = useLocaleForm()

  const f = spec(fieldKey)
  if (!f) return null

  const value = field(fieldKey)
  const src = source(fieldKey)
  const rich = f.widget === 'rich'
  const inputId = `lf-${active}-${fieldKey}`
  const tagsMode = tags && !rich
  const caption = (
    <span className="inline-flex items-center gap-1.5">
      <span className="inline-flex items-baseline">
        {fieldLabel(fieldKey)}
        {required && isSource ? (
          <span aria-hidden="true" className="ms-0.5 text-destructive">*</span>
        ) : null}
      </span>
      <FieldHint text={hint} />
    </span>
  )

  // 逐字段 AI 起草/润色：与「按语言批量翻译填充」（LocaleFormActions）互补——
  // 批量那条要有原文才有活可干，起草空字段只能靠这里。未启用 AI 时组件自己返回 null。
  const target = aiTarget ?? (f.widget === 'text' ? 'title' : 'body')
  const ai = (
    <AiWriteButton
      className="h-7"
      target={target}
      format={rich ? 'html' : 'text'}
      getText={() => field(fieldKey)}
      // 关键词类字段（多词枚举）按标题当要点起草；普通 text 字段自己就是要点。
      getBrief={target === 'keywords' ? () => field('title') : f.widget === 'text' || fieldKey === 'title' ? undefined : () => field('title')}
      onResult={(text) => {
        setField(fieldKey, rich ? text : text.slice(0, f.max ?? text.length))
        // 写到译文页签下的内容一律把该语言降回「机翻未校对」：已经人工校对过的语言被 AI 改写后
        // 必须重新看一眼，否则「已校对」就成了假状态。原文页签不受影响。
        if (!isSource) markMachine()
      }}
    />
  )

  if (rich) {
    // 原文是 HTML，只能按富文本渲染；折叠面板默认收起，不翻正文的人完全感知不到它。
    if (isSource || !src.trim()) {
      return (
        <div className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}>
          <div className="flex min-h-7 items-center justify-between gap-2">
            {caption}
            {ai}
          </div>
          <RichTextEditor value={value} onChange={(next) => setField(fieldKey, next)} />
        </div>
      )
    }
    return (
      <div className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}>
        <Collapsible className="flex flex-col gap-1">
          <div className="flex min-h-7 items-center justify-between gap-2">
            {caption}
            <div className="flex items-center gap-2">
              {ai}
              <CollapsibleTrigger className="inline-flex items-center gap-1 rounded text-xs text-muted-foreground underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">
                {t('localeForm.source')}
                <ChevronDown className="size-3.5 transition-transform" aria-hidden="true" />
              </CollapsibleTrigger>
            </div>
          </div>
          <CollapsibleContent>
            <div className="rounded-md border border-dashed bg-muted/40">
              <RichTextView html={src} className="px-3 py-2 text-sm" />
            </div>
          </CollapsibleContent>
        </Collapsible>
        <RichTextEditor value={value} onChange={(next) => setField(fieldKey, next)} />
      </div>
    )
  }

  return (
    <div className={cn('flex min-w-0 flex-col gap-1 text-sm', className)}>
      <div className="flex min-h-7 items-center justify-between gap-2">
        <label htmlFor={inputId}>{caption}</label>
        {ai}
      </div>
      {tagsMode ? (
        <TagsInput
          id={inputId}
          value={value}
          onChange={(next) => setField(fieldKey, next)}
          max={tags.max}
          placeholder={placeholder}
          ariaLabel={fieldLabel(fieldKey)}
        />
      ) : f.widget === 'textarea' ? (
        <Textarea
          id={inputId}
          rows={f.rows ?? 2}
          maxLength={f.max}
          placeholder={placeholder}
          value={value}
          onChange={(e) => setField(fieldKey, e.target.value)}
        />
      ) : (
        <Input
          id={inputId}
          maxLength={f.max}
          placeholder={placeholder}
          value={value}
          onChange={(e) => setField(fieldKey, e.target.value)}
        />
      )}
      {!isSource ? <SourceHint value={src} /> : null}
    </div>
  )
}
