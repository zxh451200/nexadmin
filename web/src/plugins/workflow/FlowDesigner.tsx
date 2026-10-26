// FlowDesigner 是自研 SVG 可视化流程编辑器：拖放节点、点击连线、右侧属性面板。
// 产出/读入 bpmn.Definition JSON，与 workflow 插件的 API 直接对接。
// SVG 内部使用 CSS 变量（语义 token），在浅色/深色主题下自动适配。

import { useCallback, useEffect, useRef, useState } from 'react'

import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'

import type { WfDefinition, WfNode } from './api'

// ---------- 常量 ----------

const NODE_W = 140
const NODE_H = 44
const CANVAS_W = 800
const CANVAS_H = 480
const GRID = 20

const NODE_KINDS = [
  { kind: 'start', label: '开始', color: 'var(--color-green-500)', icon: '▶' },
  { kind: 'approval', label: '审批', color: 'var(--color-blue-500)', icon: '✓' },
  { kind: 'exclusive', label: '网关', color: 'var(--color-amber-500)', icon: '◇' },
  { kind: 'end', label: '结束', color: 'var(--color-red-500)', icon: '■' },
] as const

interface Positioned {
  node: WfNode
  x: number
  y: number
}

interface Props {
  definition: WfDefinition
  onChange: (def: WfDefinition) => void
  readOnly?: boolean
}

// ---------- 辅助 ----------

function defaultY(kind: string, existing: Positioned[]) {
  const base = kind === 'start' || kind === 'end' ? 60 : kind === 'exclusive' ? 200 : 130
  let y = base
  while (existing.some((p) => Math.abs(p.y - y) < 20 && Math.abs(p.x - CANVAS_W / 2) < NODE_W)) {
    y += NODE_H + 16
  }
  return y
}

function centerOf(p: Positioned) {
  return { cx: p.x + NODE_W / 2, cy: p.y + NODE_H / 2 }
}

function edgePath(a: Positioned, b: Positioned) {
  const ac = centerOf(a)
  const bc = centerOf(b)
  if (Math.abs(ac.cy - bc.cy) < 10) {
    return `M ${ac.cx} ${ac.cy} L ${bc.cx} ${bc.cy}`
  }
  const mx = (ac.cx + bc.cx) / 2
  return `M ${ac.cx} ${ac.cy} C ${mx} ${ac.cy}, ${mx} ${bc.cy}, ${bc.cx} ${bc.cy}`
}

// ---------- 组件 ----------

export function FlowDesigner({ definition, onChange, readOnly }: Props) {
  const [nodes, setNodes] = useState<Positioned[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [linkFrom, setLinkFrom] = useState<string | null>(null)
  const [dragging, setDragging] = useState<{ key: string; dx: number; dy: number } | null>(null)
  const svgRef = useRef<SVGSVGElement | null>(null)

  useEffect(() => {
    const positions = parsePositions(definition)
    const placed: Positioned[] = definition.nodes.map((node, i) => ({
      node,
      x: positions[node.key]?.x ?? 80 + (i % 3) * (NODE_W + 60),
      y: positions[node.key]?.y ?? defaultY(node.kind, placed),
    }))
    setNodes(placed)
    setSelected(null)
    setLinkFrom(null)
  }, [definition])

  const persist = useCallback(
    (updated: Positioned[]) => {
      setNodes(updated)
      if (readOnly) return
      onChange({
        ...definition,
        nodes: updated.map((p) => p.node),
        edges: definition.edges.filter(
          (e) => updated.some((p) => p.node.key === e.from) && updated.some((p) => p.node.key === e.to),
        ),
      })
    },
    [definition, onChange, readOnly],
  )

  const svgPoint = (e: React.MouseEvent) => {
    const rect = svgRef.current?.getBoundingClientRect()
    if (!rect) return { x: 0, y: 0 }
    return { x: e.clientX - rect.left, y: e.clientY - rect.top }
  }

  const onMouseDown = (key: string, e: React.MouseEvent) => {
    if (readOnly) return
    const pt = svgPoint(e)
    const p = nodes.find((n) => n.node.key === key)
    if (!p) return
    setDragging({ key, dx: pt.x - p.x, dy: pt.y - p.y })
    setSelected(key)
  }

  const onMouseMove = (e: React.MouseEvent) => {
    if (!dragging) return
    const pt = svgPoint(e)
    const x = Math.max(0, Math.min(CANVAS_W - NODE_W, Math.round((pt.x - dragging.dx) / GRID) * GRID))
    const y = Math.max(0, Math.min(CANVAS_H - NODE_H, Math.round((pt.y - dragging.dy) / GRID) * GRID))
    setNodes((cur) => cur.map((p) => (p.node.key === dragging.key ? { ...p, x, y } : p)))
  }

  const onMouseUp = () => {
    if (dragging) {
      persist(nodes)
      setDragging(null)
    }
  }

  const addNode = (kind: string) => {
    if (readOnly) return
    if (kind === 'start' && nodes.some((n) => n.node.kind === 'start')) return
    if (kind === 'end' && nodes.some((n) => n.node.kind === 'end')) return
    let key = kind
    let i = 1
    while (nodes.some((n) => n.node.key === key)) {
      key = `${kind}_${i++}`
    }
    const labels: Record<string, string> = { start: '开始', approval: '审批', exclusive: '网关', end: '结束' }
    const node: WfNode = { key, name: labels[kind] ?? kind, kind: kind as WfNode['kind'] }
    if (kind === 'approval') {
      node.approvers = []
      node.mode = 'any'
    }
    persist([...nodes, { node, x: CANVAS_W / 2 - NODE_W / 2, y: defaultY(kind, nodes) }])
  }

  const removeNode = (key: string) => {
    if (readOnly) return
    const node = nodes.find((n) => n.node.key === key)
    if (!node || node.node.kind === 'start' || node.node.kind === 'end') return
    persist(nodes.filter((n) => n.node.key !== key))
    if (selected === key) setSelected(null)
  }

  const completeLink = (toKey: string) => {
    if (!linkFrom || linkFrom === toKey || readOnly) return
    const exists = definition.edges.some((e) => e.from === linkFrom && e.to === toKey)
    if (!exists) {
      onChange({ ...definition, edges: [...definition.edges, { from: linkFrom, to: toKey }] })
    }
    setLinkFrom(null)
  }

  const removeEdge = (idx: number) => {
    if (readOnly) return
    onChange({ ...definition, edges: definition.edges.filter((_, i) => i !== idx) })
  }

  const selectedNode = nodes.find((n) => n.node.key === selected)?.node
  const updateNode = (patch: Partial<WfNode>) => {
    if (!selectedNode) return
    persist(nodes.map((p) => (p.node.key === selected ? { ...p, node: { ...p.node, ...patch } } : p)))
  }

  const updateEdgeWhen = (idx: number, when: string) => {
    const edges = [...definition.edges]
    edges[idx] = { ...edges[idx], when: when || undefined }
    onChange({ ...definition, edges })
  }

  return (
    <div className="flex gap-4">
      <div className="flex-1">
        {!readOnly ? (
          <div className="mb-2 flex gap-2">
            {NODE_KINDS.map((k) => (
              <button
                key={k.kind}
                type="button"
                className="flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-accent"
                onClick={() => addNode(k.kind)}
              >
                <span className="fill-current" style={{ color: k.color }}>{k.icon}</span>
                {k.label}
              </button>
            ))}
          </div>
        ) : null}
        <svg
          ref={svgRef}
          width={CANVAS_W}
          height={CANVAS_H}
          className="rounded border bg-muted/30"
          onMouseMove={onMouseMove}
          onMouseUp={onMouseUp}
          onMouseLeave={onMouseUp}
        >
          <defs>
            <pattern id="wf-grid" width={GRID} height={GRID} patternUnits="userSpaceOnUse">
              <circle cx={1} cy={1} r={0.5} className="fill-border" />
            </pattern>
            <marker id="wf-arrow" markerWidth={8} markerHeight={6} refX={8} refY={3} orient="auto">
              <path d="M0,0 L8,3 L0,6" className="fill-muted-foreground" />
            </marker>
          </defs>
          <rect width={CANVAS_W} height={CANVAS_H} fill="url(#wf-grid)" />

          {definition.edges.map((e, i) => {
            const from = nodes.find((n) => n.node.key === e.from)
            const to = nodes.find((n) => n.node.key === e.to)
            if (!from || !to) return null
            const midX = (centerOf(from).cx + centerOf(to).cx) / 2
            const midY = (centerOf(from).cy + centerOf(to).cy) / 2
            return (
              <g key={i}>
                <path d={edgePath(from, to)} className="stroke-muted-foreground" fill="none" strokeWidth={1.5} markerEnd="url(#wf-arrow)" />
                {e.when ? (
                  <text x={midX} y={midY - 6} textAnchor="middle" className="fill-muted-foreground text-[10px]">
                    {e.when}
                  </text>
                ) : null}
                {!readOnly ? (
                  <circle cx={midX} cy={midY} r={8} fill="transparent" className="cursor-pointer hover:fill-destructive/20" onClick={() => removeEdge(i)}>
                    <title>点击删除连线</title>
                  </circle>
                ) : null}
              </g>
            )
          })}

          {linkFrom ? (
            <text x={10} y={16} className="fill-primary text-xs">
              点击目标节点完成连线（Esc 取消）
            </text>
          ) : null}

          {nodes.map((p) => {
            const kind = NODE_KINDS.find((k) => k.kind === p.node.kind)
            const isSelected = selected === p.node.key
            const isLinking = linkFrom === p.node.key
            return (
              <g
                key={p.node.key}
                transform={`translate(${p.x},${p.y})`}
                className={readOnly ? '' : 'cursor-move'}
                onMouseDown={(e) => onMouseDown(p.node.key, e)}
                onClick={() => completeLink(p.node.key)}
              >
                <rect
                  width={NODE_W}
                  height={NODE_H}
                  rx={p.node.kind === 'exclusive' ? NODE_H / 2 : 8}
                  className="fill-background"
                  stroke={isLinking || isSelected ? 'var(--color-blue-500)' : kind?.color}
                  strokeWidth={isSelected || isLinking ? 2 : 1.5}
                />
                <text x={12} y={NODE_H / 2 + 5} fontSize={14} className="fill-current" style={{ color: kind?.color }}>
                  {kind?.icon}
                </text>
                <text x={30} y={NODE_H / 2 + 5} fontSize={12} className="fill-foreground">
                  {p.node.name || p.node.key}
                </text>
                {p.node.mode === 'all' ? (
                  <text x={NODE_W - 24} y={NODE_H / 2 + 5} fontSize={10} className="fill-muted-foreground">
                    会签
                  </text>
                ) : null}
              </g>
            )
          })}
        </svg>
      </div>

      {selectedNode && !readOnly ? (
        <div className="w-56 space-y-2 rounded border bg-card p-3 text-sm">
          <div className="font-medium">{selectedNode.name || selectedNode.key}</div>
          {selectedNode.kind === 'approval' ? (
            <>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">节点名称</span>
                <Input value={selectedNode.name} onChange={(e) => updateNode({ name: e.target.value })} />
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">审批人 ID（逗号分隔）</span>
                <Input
                  value={(selectedNode.approvers ?? []).join(',')}
                  onChange={(e) => updateNode({ approvers: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })}
                />
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">模式</span>
                <select
                  className="h-8 rounded-md border bg-background px-2"
                  value={selectedNode.mode ?? 'any'}
                  onChange={(e) => updateNode({ mode: e.target.value })}
                >
                  <option value="any">或签（任一通过）</option>
                  <option value="all">会签（全部通过）</option>
                </select>
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">超时升级到</span>
                <select
                  className="h-8 rounded-md border bg-background px-2"
                  value={selectedNode.escalateTo ?? ''}
                  onChange={(e) => updateNode({ escalateTo: e.target.value || undefined })}
                >
                  <option value="">不升级</option>
                  {nodes.filter((n) => n.node.key !== selectedNode.key && n.node.kind === 'approval').map((n) => (
                    <option key={n.node.key} value={n.node.key}>
                      {n.node.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">超时（分钟，0=不升级）</span>
                <Input
                  type="number"
                  value={selectedNode.timeoutMinutes ?? 0}
                  onChange={(e) => updateNode({ timeoutMinutes: Number(e.target.value) || 0 })}
                />
              </label>
            </>
          ) : null}
          {selectedNode.kind !== 'start' && selectedNode.kind !== 'end' ? (
            <Button variant="destructive" size="sm" onClick={() => removeNode(selectedNode.key)}>
              删除节点
            </Button>
          ) : null}

          <div className="mt-2 space-y-1 border-t pt-2">
            <div className="text-xs text-muted-foreground">出边条件</div>
            {definition.edges.map((e, i) =>
              e.from === selectedNode.key ? (
                <div key={i} className="flex items-center gap-1">
                  <span className="min-w-0 flex-1 truncate text-xs">→ {e.to}</span>
                  <Input
                    className="h-6 w-24 text-xs"
                    value={e.when ?? ''}
                    placeholder="条件(可空)"
                    onChange={(ev) => updateEdgeWhen(i, ev.target.value)}
                  />
                </div>
              ) : null,
            )}
          </div>
        </div>
      ) : null}
    </div>
  )
}

// ---- 位置持久化 ----

const POS_KEY = '_positions'

export function parsePositions(def: WfDefinition): Record<string, { x: number; y: number }> {
  const hidden = (def as unknown as Record<string, unknown>)[POS_KEY]
  if (hidden && typeof hidden === 'object') return hidden as Record<string, { x: number; y: number }>
  return {}
}

export function withPositions(def: WfDefinition, nodes: Positioned[]): WfDefinition {
  const pos: Record<string, { x: number; y: number }> = {}
  for (const p of nodes) {
    pos[p.node.key] = { x: p.x, y: p.y }
  }
  return { ...def, [POS_KEY]: pos } as WfDefinition
}
