import { useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import type { Cond, CondOp, FormField, Scalar } from './api'
import { OPS_BY_TYPE, usesOptions } from './schema'

interface Props {
  value?: Cond
  /** 只能引用排在当前字段之前、产生数据的字段（因此不会有循环） */
  refs: FormField[]
  onChange: (c: Cond | undefined) => void
}

type Mode = 'all' | 'any'
type Leaf = Required<Pick<Cond, 'field' | 'op'>> & { value?: Cond['value'] }

const isLeaf = (c: Cond) => !c.all && !c.any

/** 把条件拆成"模式 + 若干叶子规则"；嵌套更深的条件（可由导入的文件带来）无法在这里编辑，返回 null。 */
function flatten(c?: Cond): { mode: Mode; rules: Leaf[] } | null {
  if (!c) return { mode: 'all', rules: [] }
  if (isLeaf(c)) return { mode: 'all', rules: [c as Leaf] }
  const mode: Mode = c.all ? 'all' : 'any'
  const list = (c.all ?? c.any ?? []) as Cond[]
  return list.every(isLeaf) ? { mode, rules: list as Leaf[] } : null
}

function build(mode: Mode, rules: Leaf[]): Cond | undefined {
  if (rules.length === 0) return undefined
  if (rules.length === 1) return rules[0]
  return { [mode]: rules } as Cond
}

const NO_VALUE: CondOp[] = ['empty', 'notEmpty']

export function CondEditor({ value, refs, onChange }: Props) {
  const { t } = useTranslation()
  const flat = flatten(value)
  if (!flat) {
    return (
      <div className="flex flex-col gap-2 rounded-md border border-border p-3 text-sm">
        <p className="text-muted-foreground">{t('plugin_formbuilder.cond.advanced')}</p>
        <Button type="button" variant="outline" size="sm" className="self-start" onClick={() => onChange(undefined)}>
          {t('plugin_formbuilder.cond.clear')}
        </Button>
      </div>
    )
  }
  const { mode, rules } = flat
  const set = (m: Mode, r: Leaf[]) => onChange(build(m, r))
  const patch = (i: number, p: Partial<Leaf>) => set(mode, rules.map((r, j) => (j === i ? { ...r, ...p } : r)))
  const add = () => {
    const first = refs[0]
    if (!first) return
    set(mode, [...rules, { field: first.key, op: OPS_BY_TYPE[first.type][0] ?? 'notEmpty' }])
  }

  return (
    <div className="flex flex-col gap-3">
      {refs.length === 0 ? <p className="text-xs text-muted-foreground">{t('plugin_formbuilder.cond.noRefs')}</p> : null}
      {rules.length > 1 ? (
        <Select value={mode} onValueChange={(m) => set(m as Mode, rules)}>
          <SelectTrigger className="md:w-64" aria-label={t('plugin_formbuilder.cond.mode')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t('plugin_formbuilder.cond.all')}</SelectItem>
            <SelectItem value="any">{t('plugin_formbuilder.cond.any')}</SelectItem>
          </SelectContent>
        </Select>
      ) : null}
      {rules.map((r, i) => (
        <RuleRow key={i} rule={r} refs={refs} onChange={(p) => patch(i, p)} onRemove={() => set(mode, rules.filter((_, j) => j !== i))} />
      ))}
      <Button type="button" variant="outline" size="sm" className="self-start" onClick={add} disabled={refs.length === 0}>
        <Plus className="me-1 size-4" aria-hidden="true" />
        {t('plugin_formbuilder.cond.add')}
      </Button>
    </div>
  )
}

function RuleRow({ rule, refs, onChange, onRemove }: { rule: Leaf; refs: FormField[]; onChange: (p: Partial<Leaf>) => void; onRemove: () => void }) {
  const { t } = useTranslation()
  const target = refs.find((f) => f.key === rule.field)
  const ops = target ? OPS_BY_TYPE[target.type] : []
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-2 md:flex-row md:items-start">
      <Select
        value={rule.field}
        onValueChange={(k) => {
          const f = refs.find((x) => x.key === k)
          onChange({ field: k, op: OPS_BY_TYPE[f?.type ?? 'text'][0] ?? 'notEmpty', value: undefined })
        }}
      >
        <SelectTrigger className="md:w-44" aria-label={t('plugin_formbuilder.cond.field')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {refs.map((f) => (
            <SelectItem key={f.key} value={f.key}>
              {f.title || f.key}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select value={rule.op} onValueChange={(op) => onChange({ op: op as CondOp, value: undefined })}>
        <SelectTrigger className="md:w-40" aria-label={t('plugin_formbuilder.cond.op')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {ops.map((op) => (
            <SelectItem key={op} value={op as string}>
              {t(`plugin_formbuilder.ops.${op}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {!NO_VALUE.includes(rule.op) && target ? <ValueInput target={target} rule={rule} onChange={(value) => onChange({ value })} /> : null}
      <Button type="button" variant="ghost" size="icon" className="size-10 shrink-0 md:size-9" aria-label={t('common.delete')} title={t('common.delete')} onClick={onRemove}>
        <Trash2 className="size-4" aria-hidden="true" />
      </Button>
    </div>
  )
}

function ValueInput({ target, rule, onChange }: { target: FormField; rule: Leaf; onChange: (v: Cond['value']) => void }) {
  const { t } = useTranslation()
  const opts = target.options ?? []
  if (rule.op === 'in') {
    const cur = Array.isArray(rule.value) ? (rule.value as Scalar[]) : []
    if (usesOptions(target)) {
      return (
        <div className="flex flex-1 flex-wrap gap-x-3 gap-y-1" role="group" aria-label={t('plugin_formbuilder.cond.value')}>
          {opts.map((o) => (
            <label key={o.value} className="flex min-h-10 items-center gap-1.5 text-sm md:min-h-8">
              <Checkbox checked={cur.includes(o.value)} onCheckedChange={(c) => onChange(c === true ? [...cur, o.value] : cur.filter((x) => x !== o.value))} />
              {o.label}
            </label>
          ))}
        </div>
      )
    }
    return <ListInput target={target} cur={cur} onChange={onChange} />
  }
  if (usesOptions(target)) {
    return (
      <Select value={typeof rule.value === 'string' ? rule.value : ''} onValueChange={(v) => onChange(v)}>
        <SelectTrigger className="flex-1" aria-label={t('plugin_formbuilder.cond.value')}>
          <SelectValue placeholder={t('common.pleaseSelect')} />
        </SelectTrigger>
        <SelectContent>
          {opts.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  }
  if (target.type === 'switch') {
    return (
      <Select value={rule.value === undefined ? '' : String(rule.value)} onValueChange={(v) => onChange(v === 'true')}>
        <SelectTrigger className="flex-1" aria-label={t('plugin_formbuilder.cond.value')}>
          <SelectValue placeholder={t('common.pleaseSelect')} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="true">{t('common.yes')}</SelectItem>
          <SelectItem value="false">{t('common.no')}</SelectItem>
        </SelectContent>
      </Select>
    )
  }
  const numeric = target.type === 'number' || target.type === 'rating'
  return (
    <Input
      className="flex-1"
      type={numeric ? 'number' : target.type === 'date' ? 'date' : 'text'}
      aria-label={t('plugin_formbuilder.cond.value')}
      value={rule.value === undefined ? '' : String(rule.value)}
      onChange={(e) => onChange(numeric ? (e.target.value === '' ? undefined : Number(e.target.value)) : e.target.value)}
    />
  )
}

/** 逗号分隔的取值列表。用本地文本保存输入，避免每次按键都被解析结果覆盖（否则输不进逗号）。 */
function ListInput({ target, cur, onChange }: { target: FormField; cur: Scalar[]; onChange: (v: Cond['value']) => void }) {
  const { t } = useTranslation()
  const [text, setText] = useState(cur.join(','))
  // 数字/评分字段的元素必须是数字，服务端 scalarEq 不做类型转换
  const numeric = target.type === 'number' || target.type === 'rating'
  const parse = (s: string): Scalar[] => {
    const parts = s.split(',').map((x) => x.trim()).filter(Boolean)
    return numeric ? parts.map(Number).filter((n) => Number.isFinite(n)) : parts
  }
  return (
    <Input
      className="flex-1"
      value={text}
      aria-label={t('plugin_formbuilder.cond.value')}
      placeholder={t('plugin_formbuilder.cond.inHint')}
      onChange={(e) => {
        setText(e.target.value)
        onChange(parse(e.target.value))
      }}
    />
  )
}
