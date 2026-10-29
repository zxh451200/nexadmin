import { useMemo, useState } from 'react'
import { ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { ExpandAllToggle } from '@/framework/components/ExpandAllToggle'
import { cn } from '@/lib/utils'

export interface PermissionNode {
  id: string
  title: string
  type?: 'dir' | 'menu' | 'button'
  perm?: string
  children?: PermissionNode[]
}

export interface PermissionTreeProps {
  data: PermissionNode[]
  /** 已勾选的节点 id（含因子节点被选中而半选的祖先） */
  value: string[]
  onChange: (ids: string[]) => void
  disabled?: boolean
  className?: string
}

const hasKids = (n: PermissionNode) => !!n.children?.length

function subtreeIds(n: PermissionNode, out: string[] = []): string[] {
  out.push(n.id)
  n.children?.forEach((c) => subtreeIds(c, out))
  return out
}

function leafIds(n: PermissionNode, out: string[] = []): string[] {
  if (!hasKids(n)) out.push(n.id)
  else n.children!.forEach((c) => leafIds(c, out))
  return out
}

/** 祖先节点：只要有后代被选中就计入（半选也保留，保证菜单可见） */
function normalize(nodes: PermissionNode[], set: Set<string>): boolean {
  let any = false
  for (const n of nodes) {
    if (hasKids(n)) {
      const c = normalize(n.children!, set)
      if (c) set.add(n.id)
      else set.delete(n.id)
      any = any || c
    } else if (set.has(n.id)) {
      any = true
    }
  }
  return any
}

/** 勾选父节点联动全部子孙；子孙全选时父节点为选中，部分选中时为半选 */
export function PermissionTree({ data, value, onChange, disabled, className }: PermissionTreeProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState<Set<string> | null>(null)
  const selected = useMemo(() => new Set(value), [value])

  const open = expanded ?? new Set(data.filter(hasKids).map((n) => n.id))
  const toggleOpen = (id: string) => {
    const next = new Set(open)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setExpanded(next)
  }
  const allParents = useMemo(() => {
    const out: string[] = []
    const walk = (ns: PermissionNode[]) =>
      ns.forEach((n) => {
        if (hasKids(n)) {
          out.push(n.id)
          walk(n.children!)
        }
      })
    walk(data)
    return out
  }, [data])
  const allIds = useMemo(() => data.flatMap((n) => subtreeIds(n)), [data])

  const stateOf = (n: PermissionNode): boolean | 'indeterminate' => {
    const leaves = leafIds(n)
    const count = leaves.filter((id) => selected.has(id)).length
    if (count === 0) return false
    return count === leaves.length ? true : 'indeterminate'
  }

  const toggle = (n: PermissionNode) => {
    if (disabled) return
    const next = new Set(selected)
    const check = stateOf(n) !== true
    subtreeIds(n).forEach((id) => (check ? next.add(id) : next.delete(id)))
    normalize(data, next)
    onChange([...next])
  }

  const render = (nodes: PermissionNode[], depth: number): React.ReactNode =>
    nodes.map((n) => {
      const kids = hasKids(n)
      const isOpen = open.has(n.id)
      return (
        <li key={n.id} role="treeitem" aria-expanded={kids ? isOpen : undefined}>
          <div className="flex items-center" style={{ paddingInlineStart: depth * 20 }}>
            {kids ? (
              <button
                type="button"
                aria-label={isOpen ? t('role.assign.collapse') : t('role.assign.expand')}
                className="inline-flex h-11 w-11 shrink-0 items-center justify-center text-muted-foreground md:h-8 md:w-8"
                onClick={() => toggleOpen(n.id)}
              >
                <ChevronRight
                  className={cn('h-4 w-4 transition-transform motion-reduce:transition-none rtl:rotate-180', isOpen && 'rotate-90 rtl:rotate-90')}
                  aria-hidden="true"
                />
              </button>
            ) : (
              <span className="h-11 w-11 shrink-0 md:h-8 md:w-8" aria-hidden="true" />
            )}
            <label className="flex min-h-11 flex-1 cursor-pointer items-center gap-2 rounded-sm px-1 hover:bg-muted md:min-h-8">
              <Checkbox checked={stateOf(n)} disabled={disabled} onCheckedChange={() => toggle(n)} />
              <span className="min-w-0 flex-1 truncate text-base md:text-sm">{t(n.title, { defaultValue: n.title })}</span>
              {n.type ? <Badge variant={n.type === 'button' ? 'outline' : 'secondary'}>{t(`role.assign.type.${n.type}`)}</Badge> : null}
            </label>
          </div>
          {kids && isOpen ? <ul role="group">{render(n.children!, depth + 1)}</ul> : null}
        </li>
      )
    })

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div className="flex flex-wrap items-center gap-2">
        <ExpandAllToggle ids={allParents} expanded={open} onChange={setExpanded} />
        <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => onChange(allIds)}>
          {t('role.assign.selectAll')}
        </Button>
        <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => onChange([])}>
          {t('role.assign.clearAll')}
        </Button>
        <span className="num ms-auto text-sm text-muted-foreground">{t('role.assign.selectedCount', { count: value.length })}</span>
      </div>
      <ul role="tree" aria-multiselectable="true" className="rounded-lg border border-border bg-card p-1">
        {data.length ? (
          render(data, 0)
        ) : (
          <li className="px-2 py-6 text-center text-sm text-muted-foreground">{t('role.assign.empty')}</li>
        )}
      </ul>
    </div>
  )
}
