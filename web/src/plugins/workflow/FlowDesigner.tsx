// FlowDesigner 是自研 SVG 可视化流程编辑器：拖放节点、点击连线、右侧属性面板。
// 产出/读入 bpmn.Definition JSON，与 workflow 插件的 API 直接对接。
// SVG 内部使用 CSS 变量（语义 token），在浅色/深色主题下自动适配。

import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { SimpleSelect } from '@/framework/components/SimpleSelect'

import type { WfDefinition, WfNode } from './api'

// ---------- 常量 ----------

const NODE_W = 140
const NODE_H = 44
const CANVAS_W = 800
const CANVAS_H = 480
const GRID = 20

const NODE_KINDS = [
  { kind: 'start', labelKey: 'plugin_workflow.designer.nodeStart', color: 'hsl(var(--success))', icon: '▶' },
  { kind: 'approval', labelKey: 'plugin_workflow.designer.nodeApproval', color: 'hsl(var(--primary))', icon: '✓' },
  { kind: 'exclusive', labelKey: 'plugin_workflow.designer.nodeGateway', color: 'hsl(var(--warning))', icon: '◇' },
  { kind: 'end', labelKey: 'plugin_workflow.designer.nodeEnd', color: 'hsl(var(--destructive))', icon: '■' },
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

/**
 * 竖向找空位：只避让「同一条竖带上」的已有节点。
 * x 必须由调用方传入——早先这里拿 CANVAS_W/2 当目标 x，结果离中线远的节点永远
 * 判不出冲突，新节点直接压在旧节点上（开始/结束叠在一起）。
 */
function defaultY(kind: string, existing: Positioned[], x: number) {
  const base = kind === 'start' || kind === 'end' ? 60 : kind === 'exclusive' ? 200 : 130
  let y = base
  while (existing.some((p) => Math.abs(p.y - y) < NODE_H / 2 && Math.abs(p.x - x) < NODE_W)) {
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
  const { t } = useTranslation()
  const [nodes, setNodes] = useState<Positioned[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [linkFrom, setLinkFrom] = useState<string | null>(null)
  const [dragging, setDragging] = useState<{ key: string; dx: number; dy: number } | null>(null)
  const svgRef = useRef<SVGSVGElement | null>(null)

  useEffect(() => {
    const positions = parsePositions(definition)
    // 默认摆位：竖向单列居中，按节点数压缩间距以塞进画布；
    // 存过坐标（POS_KEY）的定义一律以存量为准。逐个落位是为了配合 defaultY 的避让。
    const count = Math.max(definition.nodes.length, 1)
    const gap = Math.min(NODE_H + 40, Math.floor((CANVAS_H - 80) / count))
    const placed: Positioned[] = []
    definition.nodes.forEach((node, i) => {
      const saved = positions[node.key]
      const x = saved?.x ?? CANVAS_W / 2 - NODE_W / 2
      placed.push({ node, x, y: saved?.y ?? 48 + i * gap })
    })
    setNodes(placed)
    // 拖动会经 persist 重建 definition（新对象），这里若无条件清空选中，点一下节点面板就消失。
    // 只在节点真的没了的时候才清。
    const alive = new Set(placed.map((p) => p.node.key))
    setSelected((cur) => (cur && alive.has(cur) ? cur : null))
    setLinkFrom((cur) => (cur && alive.has(cur) ? cur : null))
  }, [definition])

  // Esc 取消连线：画布提示文案承诺了这个行为，得真的接上。
  // 必须用捕获阶段并 stopPropagation：Radix 的 Dialog 在 document 上监听 Esc 并关闭弹窗，
  // 冒泡阶段处理会被它先吃掉——取消连线会把整个编辑器关掉。
  useEffect(() => {
    if (!linkFrom) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.preventDefault()
      e.stopPropagation()
      setLinkFrom(null)
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [linkFrom])

  const persist = useCallback(
    (updated: Positioned[]) => {
      setNodes(updated)
      if (readOnly) return
      onChange(
        // 坐标必须一起写回：否则父级 definition 里没有 _positions，重渲染时节点按默认位重排，
        // 拖动松手就弹回原位。
        withPositions(
          {
            ...definition,
            nodes: updated.map((p) => p.node),
            edges: definition.edges.filter(
              (e) => updated.some((p) => p.node.key === e.from) && updated.some((p) => p.node.key === e.to),
            ),
          },
          updated,
        ),
      )
    },
    [definition, onChange, readOnly],
  )

  // SVG 用 viewBox 跟随容器缩放，指针坐标要按实际渲染宽度换算回画布单位。
  const svgPoint = (e: React.MouseEvent) => {
    const rect = svgRef.current?.getBoundingClientRect()
    if (!rect || rect.width === 0) return { x: 0, y: 0 }
    const scale = rect.width / CANVAS_W
    return { x: (e.clientX - rect.left) / scale, y: (e.clientY - rect.top) / scale }
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
    const meta = NODE_KINDS.find((k) => k.kind === kind)
    const node: WfNode = { key, name: meta ? t(meta.labelKey) : kind, kind: kind as WfNode['kind'] }
    if (kind === 'approval') {
      node.approvers = []
      node.mode = 'any'
    }
    const addX = CANVAS_W / 2 - NODE_W / 2
    persist([...nodes, { node, x: addX, y: defaultY(kind, nodes, addX) }])
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
    <div className="flex min-w-0 flex-col gap-4 xl:flex-row">
      <div className="min-w-0 flex-1">
        {!readOnly ? (
          <div className="mb-2 flex flex-wrap gap-2">
            {NODE_KINDS.map((k) => (
              <button
                key={k.kind}
                type="button"
                className="flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-accent"
                onClick={() => addNode(k.kind)}
              >
                <span className="fill-current" style={{ color: k.color }}>{k.icon}</span>
                {t(k.labelKey)}
              </button>
            ))}
          </div>
        ) : null}
        {/* 画布按容器缩放；窄屏保底 600px 宽并在自己的框内横向平移，避免节点文字缩到看不清。 */}
        <div className="overflow-x-auto">
          <svg
            ref={svgRef}
            viewBox={`0 0 ${CANVAS_W} ${CANVAS_H}`}
            style={{ maxWidth: CANVAS_W, minWidth: 600 }}
            className="block h-auto w-full rounded border bg-muted/30"
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
            {/* 点画布空白处取消选中/取消连线：否则属性面板会一直停在最后一个点上 */}
            <rect
              width={CANVAS_W}
              height={CANVAS_H}
              fill="url(#wf-grid)"
              onClick={() => {
                setSelected(null)
                setLinkFrom(null)
              }}
            />

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
                      <title>{t('plugin_workflow.designer.removeEdge')}</title>
                    </circle>
                  ) : null}
                </g>
              )
            })}

            {linkFrom ? (
              <text x={10} y={16} className="fill-primary text-xs">
                {t('plugin_workflow.designer.linkingHint')}
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
                    stroke={isLinking || isSelected ? 'hsl(var(--primary))' : kind?.color}
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
                      {t('plugin_workflow.designer.countersignBadge')}
                    </text>
                  ) : null}
                </g>
              )
            })}
          </svg>
        </div>
      </div>

      {selectedNode && !readOnly ? (
        <div className="w-full shrink-0 space-y-2 rounded border bg-card p-3 text-sm xl:w-56">
          <div className="flex items-center justify-between gap-2">
            <span className="min-w-0 truncate font-medium">{selectedNode.name || selectedNode.key}</span>
            <Button
              variant="outline"
              size="sm"
              className="h-7 shrink-0 px-2 text-xs"
              onClick={() => setLinkFrom(selectedNode.key)}
            >
              {t('plugin_workflow.designer.connectFrom')}
            </Button>
          </div>
          {selectedNode.kind === 'approval' ? (
            <>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.nodeName')}</span>
                <Input value={selectedNode.name} onChange={(e) => updateNode({ name: e.target.value })} />
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.approvers')}</span>
                <Input
                  value={(selectedNode.approvers ?? []).join(',')}
                  onChange={(e) => updateNode({ approvers: e.target.value.split(',').map((s) => s.trim()).filter(Boolean) })}
                />
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.mode')}</span>
                <SimpleSelect
                  className="w-full"
                  aria-label={t('plugin_workflow.designer.mode')}
                  value={selectedNode.mode ?? 'any'}
                  onChange={(v) => updateNode({ mode: v as 'any' | 'all' })}
                  options={[
                    { value: 'any', label: t('plugin_workflow.designer.modeAny') },
                    { value: 'all', label: t('plugin_workflow.designer.modeAll') },
                  ]}
                />
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.escalateTo')}</span>
                <SimpleSelect
                  className="w-full"
                  aria-label={t('plugin_workflow.designer.escalateTo')}
                  value={selectedNode.escalateTo ?? '__none'}
                  onChange={(v) => updateNode({ escalateTo: v === '__none' ? undefined : v })}
                  options={[
                    { value: '__none', label: t('plugin_workflow.designer.noEscalate') },
                    ...nodes.filter((n) => n.node.key !== selectedNode.key && n.node.kind === 'approval').map((n) => ({ value: n.node.key, label: n.node.name })),
                  ]}
                />
              </label>
              <label className="flex flex-col gap-0.5">
                <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.timeoutMinutes')}</span>
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
              {t('plugin_workflow.designer.deleteNode')}
            </Button>
          ) : null}

          <div className="mt-2 space-y-1 border-t pt-2">
            <div className="text-xs text-muted-foreground">{t('plugin_workflow.designer.edgeConditions')}</div>
            {definition.edges.map((e, i) =>
              e.from === selectedNode.key ? (
                <div key={i} className="flex items-center gap-1">
                  <span className="min-w-0 flex-1 truncate text-xs">→ {e.to}</span>
                  <Input
                    className="h-6 w-24 text-xs"
                    value={e.when ?? ''}
                    placeholder={t('plugin_workflow.designer.edgeWhen')}
                    onChange={(ev) => updateEdgeWhen(i, ev.target.value)}
                  />
                </div>
              ) : null,
            )}
          </div>
        </div>
      ) : null}
      {/* 未选中节点时也保留同宽面板：否则画布会随选中/取消左右跳一下，
          而且用户看不出「这里其实可以编辑」——固定常驻，只换内容。 */}
      {!selectedNode && !readOnly ? (
        <div className="w-full shrink-0 space-y-2 rounded border border-dashed bg-card p-3 text-sm xl:w-56">
          <div className="font-medium">{t('plugin_workflow.designer.propsTitle')}</div>
          <p className="text-xs leading-5 text-muted-foreground">{t('plugin_workflow.designer.propsEmpty')}</p>
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
