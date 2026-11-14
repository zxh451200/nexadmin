import { useMemo, useState } from 'react'
import { ChevronRight, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import type { RegionNode } from '@/framework/request/types'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { findRegionPath, useRegionTree, type RegionValue } from './pickers/useRegionTree'

export interface RegionSelectProps {
  level?: 2 | 3
  value?: RegionValue | null
  onChange?: (value: RegionValue) => void
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
}

const KEYS = ['province', 'city', 'district'] as const

// 直辖市、港澳与省直辖县级市没有地级这一层，选中省级后只需再选一级，第三列不该空着出现；
// 台湾省只到省级（没有公开的下级区划），选完就该收在省级这一列。
function widthFor(path: RegionNode[], level: number): number {
  if (path.length === 0) return level
  if (!path[0].children?.length) return 1
  return path[0].children.some((c) => c.children?.length) ? level : Math.min(level, 2)
}

function toValue(path: RegionNode[]): RegionValue {
  return { province: path[0]?.code, city: path[1]?.code, district: path[2]?.code }
}

export function RegionSelect({ level = 3, value, onChange, allowClear = true, placeholder, disabled, className, id }: RegionSelectProps) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const { data: tree = [], isLoading } = useRegionTree(level)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [draft, setDraft] = useState<RegionNode[]>([])
  const [step, setStep] = useState(0)

  const current = findRegionPath(tree, value ?? undefined)

  const onOpen = (o: boolean) => {
    setOpen(o)
    if (o) {
      setDraft(current)
      setStep(Math.max(0, Math.min(current.length, widthFor(current, level) - 1)))
      setSearch('')
    }
  }

  const choose = (depth: number, node: RegionNode) => {
    const next = [...draft.slice(0, depth), node]
    setDraft(next)
    if (depth + 1 >= level || !node.children?.length) {
      onChange?.(toValue(next))
      setOpen(false)
    } else {
      setStep(depth + 1)
    }
  }

  const kw = search.trim().toLowerCase()
  const results = useMemo(() => {
    if (!kw) return []
    const out: RegionNode[][] = []
    const walk = (nodes: RegionNode[], path: RegionNode[]) => {
      for (const n of nodes) {
        const p = [...path, n]
        // ü 在数据里写作 v（输入法习惯），按拼音搜索时 u 的写法也要能命中
        const hay = `${n.pinyin} ${n.pinyin.replace(/v/g, 'u')}`
        // 任意层级命中即入列：只认叶子的话，北海市这类带区县的地级市永远搜不到。
        // 命中自身就不再下钻，免得同名父子（北海市 → 北海市辖区）重复占行。
        if (n.name.toLowerCase().includes(kw) || hay.includes(kw)) {
          out.push(p)
          continue
        }
        if (p.length < level && n.children) walk(n.children, p)
        if (out.length >= 50) return
      }
    }
    walk(tree, [])
    return out
  }, [kw, tree, level])

  // 列数随已选层级展开：没选省份时只有省一列，选了省才长出市列——
  // 空列渲染成「暂无数据」占满整块面板，比窄列更难看
  const columns = [tree, draft[0]?.children ?? [], draft[1]?.children ?? []].slice(
    0,
    Math.min(widthFor(draft, level), draft.length + 1),
  )

  const list = (depth: number) => (
    <ul role="listbox" className="max-h-72 overflow-y-auto p-1">
      {columns[depth].map((n) => {
        const sel = draft[depth]?.code === n.code
        const hasKids = depth + 1 < level && !!n.children?.length
        return (
          <li key={n.code} role="option" aria-selected={sel}>
            <button
              type="button"
              onClick={() => choose(depth, n)}
              className={cn(
                'flex min-h-11 w-full items-center justify-between gap-2 rounded-sm px-2 text-start text-base hover:bg-muted focus-visible:bg-muted focus-visible:outline-hidden md:min-h-8 md:text-sm',
                sel && 'bg-muted font-medium text-primary',
              )}
            >
              <span className="truncate">{n.name}</span>
              {hasKids && <ChevronRight className="size-4 shrink-0 text-muted-foreground rtl:rotate-180" aria-hidden="true" />}
            </button>
          </li>
        )
      })}
      {!columns[depth].length && <li className="px-2 py-6 text-center text-sm text-muted-foreground">{isLoading ? t('common.loading') : t('common.empty')}</li>}
    </ul>
  )

  return (
    <PickerShell
      open={open}
      onOpenChange={onOpen}
      disabled={disabled}
      title={placeholder ?? t('picker.selectRegion')}
      contentClassName={cn('w-auto', columns.length === 1 ? 'w-64' : columns.length === 2 ? 'w-[28rem]' : 'min-w-xl')}
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder ?? t('picker.selectRegion')}
          hasValue={current.length > 0}
          allowClear={allowClear}
          onClear={() => onChange?.({})}
          display={current.map((n) => n.name).join(' / ')}
        />
      }
    >
      <div className="relative border-b border-border p-2">
        <Search className="pointer-events-none absolute inset-s-5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('picker.regionSearch')}
          aria-label={t('common.search')}
          className="h-11 w-full rounded-md border border-input bg-background ps-9 pe-3 text-base focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring dark:border-white/14 dark:bg-white/[0.045] md:h-8 md:text-sm"
        />
      </div>
      {kw ? (
        <ul className="max-h-72 overflow-y-auto p-1">
          {results.map((p) => (
            <li key={p[p.length - 1].code}>
              <button
                type="button"
                className="flex min-h-11 w-full items-center rounded-sm px-2 text-start text-base hover:bg-muted md:min-h-8 md:text-sm"
                onClick={() => {
                  onChange?.(toValue(p))
                  setOpen(false)
                }}
              >
                {p.map((n) => n.name).join(' / ')}
              </button>
            </li>
          ))}
          {!results.length && <li className="px-2 py-6 text-center text-sm text-muted-foreground">{t('common.empty')}</li>}
        </ul>
      ) : isMobile ? (
        <div>
          <div className="flex gap-1 border-b border-border px-2 py-1" role="tablist">
            {columns.map((_, i) => (
              <button
                key={KEYS[i]}
                type="button"
                role="tab"
                aria-selected={step === i}
                disabled={i > draft.length}
                onClick={() => setStep(i)}
                className={cn('min-h-11 flex-1 truncate rounded-md px-2 text-sm disabled:opacity-40', step === i ? 'bg-muted font-medium text-primary' : 'text-muted-foreground')}
              >
                {draft[i]?.name ?? t(`picker.${KEYS[i]}`)}
              </button>
            ))}
          </div>
          {list(step)}
        </div>
      ) : (
        <div className="grid divide-x divide-border rtl:divide-x-reverse" style={{ gridTemplateColumns: `repeat(${columns.length}, minmax(0, 1fr))` }}>
          {columns.map((_, i) => (
            <div key={KEYS[i]}>{list(i)}</div>
          ))}
        </div>
      )}
    </PickerShell>
  )
}
