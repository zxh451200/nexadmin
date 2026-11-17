import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { cn } from '@/lib/utils'

/** 默认多词拆分口径：空格/中英文逗号/顿号/分号都算分隔符，忽略大小写去重——与各实体后端的 normalize 同源。 */
const WORD_SEPARATORS = /[ \t\n\r,，、;；]+/

function splitWords(s: string, separators: RegExp): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of s.split(separators)) {
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
 * 标签形态的多词输入：芯片嵌在输入框里，回车/失焦/输入分隔符把已输入的词收进芯片，
 * 空值 Backspace 删最后一个，芯片尾部 × 删除。底层值仍是分隔符拼接的字符串——
 * 与存量数据、AI 输出互通，只是交互以词为单位。
 * 词内含空格的字段（如认证资质「UL 1973」）用 separators 收窄拆分口径、joinWith 指定回拼分隔符，
 * 空格就不会把一个词拆成两半。
 */
export function TagsInput({
  id, value, onChange, max, placeholder, ariaLabel, disabled, list, separators = WORD_SEPARATORS, joinWith = ' ',
}: {
  id?: string
  value: string
  onChange: (v: string) => void
  max?: number
  placeholder?: string
  ariaLabel?: string
  disabled?: boolean
  /** datalist 的 DOM id（外部渲染 <datalist id={list}> 提供候选）：透传给内部输入框做自动补全 */
  list?: string
  /** 词的分隔符（正则，不要带 g 标志）；缺省与搜索关键词同口径 */
  separators?: RegExp
  /** 收词后底层字符串的拼接符；缺省空格（关键词的存量存储格式） */
  joinWith?: string
}) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const words = useMemo(() => splitWords(value, separators), [value, separators])
  const [draft, setDraft] = useState('')
  // draft 的 ref 镜像：blur 回调可能拿到过期闭包（onChange 触发的重渲染还没落场），收词以 ref 为准
  const draftRef = useRef('')
  const updateDraft = (v: string) => {
    draftRef.current = v
    setDraft(v)
  }
  const atLimit = max !== undefined && words.length >= max
  const over = max !== undefined && words.length > max
  // 已输入内容里出现分隔符即收词（覆盖逗号/顿号/分号与粘贴多词）；与已有词忽略大小写去重。
  // 词与词跨段去重（value 里的 + 刚输入的），原实现靠整串重拆天然做到，这里显式合并。
  const commit = (raw: string) => {
    updateDraft('')
    const added = splitWords(raw, separators)
    if (!added.length) return
    const seen = new Set<string>()
    const merged: string[] = []
    for (const w of [...words, ...added]) {
      const k = w.toLowerCase()
      if (seen.has(k)) continue
      seen.add(k)
      merged.push(w)
    }
    onChange(merged.join(joinWith))
  }
  const remove = (w: string) => onChange(words.filter((x) => x !== w).join(joinWith))
  return (
    <div className="flex flex-col gap-1">
      <div
        className={cn(
          // 高度对齐普通 Input（h-10 / md:h-8）：py-1 只留一行余量，芯片不把框撑高
          'flex min-h-10 w-full flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-3 py-1 text-base shadow-xs',
          'transition-[color,box-shadow,border-color] duration-(--motion-fast) ease-(--ease-standard)',
          'focus-within:border-ring focus-within:outline-hidden focus-within:ring-2 focus-within:ring-ring/40',
          'dark:border-white/14 dark:bg-white/[0.045] md:min-h-8 md:text-sm',
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
          list={list}
          value={draft}
          maxLength={64}
          disabled={disabled || atLimit}
          onChange={(e) => {
            const next = e.target.value
            if (separators.test(next)) commit(next)
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
