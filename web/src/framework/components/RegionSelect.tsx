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
      setStep(Math.min(current.length, level - 1))
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
        const isLeaf = p.length >= level || !n.children?.length
        if (isLeaf && (n.name.toLowerCase().includes(kw) || n.pinyin.toLowerCase().includes(kw))) out.push(p)
        if (!isLeaf && n.children) walk(n.children, p)
        if (out.length >= 50) return
      }
    }
    walk(tree, [])
    return out
  }, [kw, tree, level])

  const columns = [tree, draft[0]?.children ?? [], draft[1]?.children ?? []].slice(0, level)

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
                'flex min-h-11 w-full items-center justify-between gap-2 rounded-sm px-2 text-start text-base hover:bg-muted focus-visible:bg-muted focus-visible:outline-none md:min-h-8 md:text-sm',
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
      contentClassName="w-auto min-w-[36rem]"
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
        <Search className="pointer-events-none absolute start-5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('picker.regionSearch')}
          aria-label={t('common.search')}
          className="h-11 w-full rounded-md border border-input bg-background ps-9 pe-3 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-9 md:text-sm"
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
        <div className="grid divide-x divide-border rtl:divide-x-reverse" style={{ gridTemplateColumns: `repeat(${level}, minmax(0, 1fr))` }}>
          {columns.map((_, i) => (
            <div key={KEYS[i]}>{list(i)}</div>
          ))}
        </div>
      )}
    </PickerShell>
  )
}
