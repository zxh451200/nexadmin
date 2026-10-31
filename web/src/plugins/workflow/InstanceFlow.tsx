// InstanceFlow 是实例详情里的只读流程图：按节点分层布局，高亮当前节点与已办节点。
// 不依赖图形库——实例详情只需要"走到哪、还剩谁"，不是可编辑建模器。
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/framework/components/EmptyState'
import { cn } from '@/lib/utils'

export interface FlowNode {
  key: string
  name: string
  kind: 'start' | 'approval' | 'exclusive' | 'end'
}

export interface FlowEdge {
  from: string
  to: string
  when?: string
}

const NODE_W = 150
const NODE_H = 46
const GAP_X = 72
const GAP_Y = 18
const PAD = 16

interface Placed extends FlowNode {
  x: number
  y: number
  level: number
}

function layout(def: { nodes: FlowNode[]; edges: FlowEdge[]; start?: string }): {
  nodes: Placed[]
  width: number
  height: number
} {
  const byKey = new Map(def.nodes.map((n) => [n.key, n]))
  const incoming = new Map<string, string[]>()
  for (const n of def.nodes) incoming.set(n.key, [])
  for (const e of def.edges) {
    if (byKey.has(e.to) && byKey.has(e.from)) incoming.get(e.to)!.push(e.from)
  }
  const start = def.start || def.nodes.find((n) => n.kind === 'start')?.key
  const level = new Map<string, number>()
  if (start && byKey.has(start)) {
    const queue: string[] = [start]
    level.set(start, 0)
    while (queue.length > 0) {
      const key = queue.shift()!
      const next = level.get(key)! + 1
      for (const e of def.edges) {
        if (e.from !== key || !byKey.has(e.to)) continue
        if (!level.has(e.to) || level.get(e.to)! < next) {
          level.set(e.to, next)
          queue.push(e.to)
        }
      }
    }
  }
  // 不可达节点（定义被改坏的历史数据）排在最后一列，而不是消失。
  let maxLevel = 0
  for (const v of level.values()) maxLevel = Math.max(maxLevel, v)
  for (const n of def.nodes) {
    if (!level.has(n.key)) {
      maxLevel += 1
      level.set(n.key, maxLevel)
    }
  }
  const columns = new Map<number, FlowNode[]>()
  for (const n of def.nodes) {
    const lv = level.get(n.key) ?? 0
    if (!columns.has(lv)) columns.set(lv, [])
    columns.get(lv)!.push(n)
  }
  const placed: Placed[] = []
  let maxRows = 1
  for (const [lv, list] of [...columns.entries()].sort((a, b) => a[0] - b[0])) {
    maxRows = Math.max(maxRows, list.length)
    list.forEach((n, row) => {
      placed.push({
        ...n,
        level: lv,
        x: PAD + lv * (NODE_W + GAP_X),
        y: PAD + row * (NODE_H + GAP_Y),
      })
    })
  }
  return {
    nodes: placed,
    width: PAD * 2 + (maxLevel + 1) * NODE_W + maxLevel * GAP_X,
    height: PAD * 2 + maxRows * NODE_H + (maxRows - 1) * GAP_Y,
  }
}

export function InstanceFlow({
  definition,
  current,
  done,
  className,
}: {
  definition?: { nodes: FlowNode[]; edges: FlowEdge[]; start?: string } | null
  current: string[]
  done: string[]
  className?: string
}) {
  const { t } = useTranslation()
  const view = useMemo(() => (definition && definition.nodes.length > 0 ? layout(definition) : null), [definition])

  if (!definition || !view) {
    return <EmptyState title={t('plugin_workflow.flow.empty')} className="py-6" />
  }

  const currentSet = new Set(current)
  const doneSet = new Set(done)
  const pos = new Map(view.nodes.map((n) => [n.key, n]))
  const title =
    current.length > 0
      ? t('plugin_workflow.flow.summary', { nodes: current.map((k) => pos.get(k)?.name || k).join('、') })
      : t('plugin_workflow.flow.summaryEnded')

  return (
    <div className={cn('overflow-x-auto rounded-lg border bg-muted/20', className)}>
      <svg
        role="img"
        aria-label={title}
        viewBox={`0 0 ${view.width} ${view.height}`}
        style={{ minWidth: Math.min(view.width, 640) }}
        className="block h-auto w-full"
      >
        <defs>
          <marker id="wf-arrow-live" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto">
            <path d="M0,0 L8,3 L0,6" className="fill-muted-foreground" />
          </marker>
        </defs>
        {definition.edges.map((e, i) => {
          const from = pos.get(e.from)
          const to = pos.get(e.to)
          if (!from || !to) return null
          const x1 = from.x + NODE_W
          const y1 = from.y + NODE_H / 2
          const x2 = to.x
          const y2 = to.y + NODE_H / 2
          const mx = (x1 + x2) / 2
          return (
            <g key={`${e.from}-${e.to}-${i}`}>
              <path
                d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
                fill="none"
                strokeWidth={1.5}
                markerEnd="url(#wf-arrow-live)"
                className={cn('stroke-muted-foreground/60', doneSet.has(e.from) && doneSet.has(e.to) && 'stroke-success/60')}
              />
              {e.when ? (
                <text x={mx} y={(y1 + y2) / 2 - 6} textAnchor="middle" className="fill-muted-foreground text-[10px]">
                  {e.when.length > 18 ? `${e.when.slice(0, 18)}…` : e.when}
                </text>
              ) : null}
            </g>
          )
        })}
        {view.nodes.map((n) => {
          const isCurrent = currentSet.has(n.key)
          const isDone = !isCurrent && doneSet.has(n.key)
          return (
            <g
              key={n.key}
              transform={`translate(${n.x},${n.y})`}
              className={cn(isCurrent && 'animate-pulse motion-reduce:animate-none')}
            >
              <rect
                width={NODE_W}
                height={NODE_H}
                rx={n.kind === 'exclusive' ? NODE_H / 2 : 8}
                strokeWidth={isCurrent ? 2 : 1.5}
                className={cn(
                  'fill-background',
                  isCurrent ? 'stroke-primary' : isDone ? 'stroke-success' : 'stroke-border',
                )}
              />
              <text x={12} y={NODE_H / 2 + 5} fontSize={13} className={cn(isCurrent ? 'fill-primary' : isDone ? 'fill-success' : 'fill-muted-foreground')}>
                {n.kind === 'start' ? '▶' : n.kind === 'end' ? '■' : n.kind === 'exclusive' ? '◇' : '✓'}
              </text>
              <text x={30} y={NODE_H / 2 - 2} fontSize={12} className="fill-foreground">
                {n.name || n.key}
              </text>
              <text x={30} y={NODE_H / 2 + 12} fontSize={10} className={cn(isCurrent ? 'fill-primary' : 'fill-muted-foreground')}>
                {isCurrent
                  ? t('plugin_workflow.flow.current')
                  : isDone
                    ? t('plugin_workflow.flow.done')
                    : t('plugin_workflow.flow.pending')}
              </text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}
