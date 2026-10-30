import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Check, ChevronRight, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { api } from '@/framework/request/http'
import { PickerShell } from './pickers/PickerShell'
import { PickerTrigger } from './pickers/PickerTrigger'
import { toArray, type PickerChange } from './pickers/OptionList'

type Node = object
const get = (n: Node, k: string) => (n as Record<string, unknown>)[k]

export interface TreeSelectProps {
  data?: Node[]
  url?: string
  value?: string | string[] | null
  onChange?: PickerChange
  multiple?: boolean
  labelKey?: string
  valueKey?: string
  childrenKey?: string
  allowClear?: boolean
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
  /** 返回 true 的节点不可选 */
  isDisabled?: (node: Record<string, unknown>) => boolean
}

export function TreeSelect({
  data,
  url,
  value,
  onChange,
  multiple,
  labelKey = 'name',
  valueKey = 'id',
  childrenKey = 'children',
  allowClear = true,
  placeholder,
  disabled,
  className,
  id,
  isDisabled,
}: TreeSelectProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const remote = useQuery({
    queryKey: ['tree-select', url],
    queryFn: () => api.get<Node[]>(url as string),
    enabled: !data && !!url,
    staleTime: 60 * 1000,
  })
  const tree = useMemo(() => data ?? remote.data ?? [], [data, remote.data])
  const selected = toArray(value)
  const kw = search.trim().toLowerCase()

  const labelMap = useMemo(() => {
    const m = new Map<string, string>()
    const walk = (nodes: Node[]) =>
      nodes.forEach((n) => {
        m.set(String(get(n, valueKey)), String(get(n, labelKey)))
        const kids = get(n, childrenKey) as Node[] | undefined
        if (kids?.length) walk(kids)
      })
    walk(tree)
    return m
  }, [tree, valueKey, labelKey, childrenKey])

  const visible = useMemo(() => {
    if (!kw) return tree
    const filter = (nodes: Node[]): Node[] =>
      nodes.flatMap((n) => {
        if (String(get(n, labelKey)).toLowerCase().includes(kw)) return [n]
        const kids = filter((get(n, childrenKey) as Node[] | undefined) ?? [])
        return kids.length ? [{ ...n, [childrenKey]: kids }] : []
      })
    return filter(tree)
  }, [tree, kw, labelKey, childrenKey])

  const pick = (v: string) => {
    if (multiple) {
      onChange?.(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v])
    } else {
      onChange?.(v)
      setOpen(false)
    }
  }
  const toggle = (v: string) =>
    setExpanded((s) => {
      const n = new Set(s)
      if (n.has(v)) n.delete(v)
      else n.add(v)
      return n
    })

  const renderNodes = (nodes: Node[], depth: number): React.ReactNode =>
    nodes.map((n) => {
      const v = String(get(n, valueKey))
      const kids = (get(n, childrenKey) as Node[] | undefined) ?? []
      const isOpen = !!kw || expanded.has(v)
      const isSel = selected.includes(v)
      return (
        <li key={v} role="treeitem" aria-expanded={kids.length ? isOpen : undefined} aria-selected={isSel}>
          <div className="flex items-center" style={{ paddingInlineStart: depth * 16 }}>
            {kids.length ? (
              <button
                type="button"
                aria-label={isOpen ? t('picker.collapse') : t('picker.expand')}
                className="inline-flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:bg-muted focus-visible:outline-none md:size-6"
                onClick={() => toggle(v)}
              >
                <ChevronRight className={cn('size-3.5 transition-transform motion-reduce:transition-none rtl:rotate-180', isOpen && 'rotate-90 rtl:rotate-90')} aria-hidden="true" />
              </button>
            ) : (
              <span className="size-7 shrink-0 md:size-6" aria-hidden="true" />
            )}
            <button
              type="button"
              disabled={isDisabled?.(n as Record<string, unknown>)}
              onClick={() => pick(v)}
              className={cn(
                'flex min-h-8 flex-1 items-center gap-2 rounded-sm px-2 text-start text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 md:min-h-8',
                isSel && 'bg-muted/60',
              )}
            >
              <span className="min-w-0 flex-1 truncate">{String(get(n, labelKey))}</span>
              {isSel && <Check className="size-4 shrink-0 text-primary" aria-hidden="true" />}
            </button>
          </div>
          {kids.length > 0 && isOpen && <ul role="group">{renderNodes(kids, depth + 1)}</ul>}
        </li>
      )
    })

  return (
    <PickerShell
      open={open}
      onOpenChange={setOpen}
      disabled={disabled}
      title={placeholder ?? t('common.pleaseSelect')}
      trigger={
        <PickerTrigger
          id={id}
          className={className}
          disabled={disabled}
          placeholder={placeholder}
          hasValue={selected.length > 0}
          allowClear={allowClear}
          onClear={() => onChange?.(multiple ? [] : '')}
          display={selected.map((v) => labelMap.get(v) ?? v).join(', ')}
        />
      }
    >
      <div className="relative border-b border-border p-2">
        <Search className="pointer-events-none absolute start-5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('picker.searchPlaceholder')}
          aria-label={t('common.search')}
          className="h-11 w-full rounded-md border border-input bg-background ps-9 pe-3 text-base focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring md:h-9 md:text-sm"
        />
      </div>
      <ul role="tree" aria-multiselectable={multiple} className="max-h-72 overflow-y-auto p-1">
        {visible.length ? (
          renderNodes(visible, 0)
        ) : (
          <li className="px-2 py-6 text-center text-sm text-muted-foreground">
            {remote.isLoading ? t('common.loading') : t('common.empty')}
          </li>
        )}
      </ul>
    </PickerShell>
  )
}
