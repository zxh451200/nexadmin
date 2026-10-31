// BpmnDesigner：基于 bpmn-js 的标准 BPMN 2.0 模型器。
// 只开放本引擎支持的建模元素（开始/结束/用户任务/排他网关/连线），
// 用户任务的 flowlite 扩展属性（审批人、或签会签、升级目标、超时）在右侧面板编辑。
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Modeler from 'bpmn-js/lib/Modeler'
import BpmnPaletteProvider from 'bpmn-js/lib/features/palette/PaletteProvider'
import { layoutProcess } from 'bpmn-auto-layout'

import 'bpmn-js/dist/assets/diagram-js.css'
import 'bpmn-js/dist/assets/bpmn-js.css'
import 'bpmn-js/dist/assets/bpmn-font/css/bpmn.css'
import './bpmn-modeler.css'

import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { Input } from '@/framework/ui/input'
import { ApproverPicker } from './ApproverPicker'
import { BLANK_BPMN_XML } from './blank-bpmn'
import descriptor from './flowlite-moddle.json'

/* eslint-disable @typescript-eslint/no-explicit-any */

const ALLOWED_PALETTE = new Set([
  'hand-tool',
  'lasso-tool',
  'space-tool',
  'global-connect-tool',
  'create.start-event',
  'create.end-event',
  'create.task',
  'create.exclusive-gateway',
])

// 只保留本引擎能执行的建模元素；并行网关/子流程/服务任务等一律隐藏，
// 避免用户在编辑器里画出后端 ImportXML 会明确拒绝的图。
// 必须继承原 provider 再过滤：用 factory 直接注入 palette 会形成循环依赖（palette ⇄ paletteProvider）。
class RestrictedPaletteProvider extends BpmnPaletteProvider {
  getPaletteEntries() {
    const entries = super.getPaletteEntries() as Record<string, unknown>
    const filtered: Record<string, unknown> = {}
    for (const key of Object.keys(entries)) {
      if (ALLOWED_PALETTE.has(key)) filtered[key] = entries[key]
    }
    return filtered as ReturnType<BpmnPaletteProvider['getPaletteEntries']>
  }
}

const paletteModule = { paletteProvider: ['type', RestrictedPaletteProvider] }


interface SelectedTask {
  id: string
  name: string
  approvers: string
  mode: string
  escalateTo: string
  timeoutMinutes: number
}

const DEFAULT_XML = BLANK_BPMN_XML

function hasDiagram(xml: string): boolean {
  return xml.includes('BPMNDiagram') || xml.includes('bpmndi:BPMNDiagram')
}

export function BpmnDesigner({
  xml,
  onChange,
  readOnly,
  className,
}: {
  xml: string
  onChange: (xml: string) => void
  readOnly?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const containerRef = useRef<HTMLDivElement | null>(null)
  const modelerRef = useRef<Modeler | null>(null)
  const lastExported = useRef<string>('')
  const onChangeRef = useRef(onChange)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<SelectedTask | null>(null)

  onChangeRef.current = onChange

  // 初始化模型器（只做一次）。
  useEffect(() => {
    const container = containerRef.current
    if (!container) return
    const modeler = new Modeler({
      container,
      moddleExtensions: { flowlite: descriptor as unknown as Record<string, unknown> },
      additionalModules: [paletteModule],
    })
    modelerRef.current = modeler
    return () => {
      modeler.destroy()
      modelerRef.current = null
    }
  }, [])

  // 导入 XML：没有 DI 的旧定义先自动布局，否则 bpmn-js 画不出图。
  useEffect(() => {
    const modeler = modelerRef.current
    if (!modeler) return
    if (xml && xml === lastExported.current) return
    let cancelled = false
    ;(async () => {
      let source = xml?.trim() ? xml : DEFAULT_XML
      if (!hasDiagram(source)) {
        try {
          source = await layoutProcess(source)
        } catch {
          // 布局失败就按原样导入，至少让用户看到错误提示而不是白屏。
        }
      }
      if (cancelled) return
      try {
        await modeler.importXML(source)
        if (cancelled) return
        const canvas = modeler.get('canvas') as any
        canvas.zoom('fit-viewport')
        setError('')
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [xml])

  // 编辑变更 → 防抖导出 XML。
  useEffect(() => {
    const modeler = modelerRef.current
    if (!modeler || readOnly) return
    const eventBus = modeler.get('eventBus') as any
    let timer: ReturnType<typeof setTimeout> | undefined
    const onChanged = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(async () => {
        try {
          const { xml: out } = await (modeler as any).saveXML({ format: true })
          lastExported.current = out
          onChangeRef.current(out)
        } catch {
          /* 导出失败保留上一次合法 XML */
        }
      }, 300)
    }
    eventBus.on('commandStack.changed', onChanged)
    return () => {
      eventBus.off('commandStack.changed', onChanged)
      if (timer) clearTimeout(timer)
    }
  }, [readOnly])

  // 选中用户任务 → 右侧属性面板。
  useEffect(() => {
    const modeler = modelerRef.current
    if (!modeler) return
    const eventBus = modeler.get('eventBus') as any
    const onSelection = (e: any) => {
      const el = e.newSelection?.[0]
      if (!el || el.businessObject?.$type !== 'bpmn:UserTask') {
        setSelected(null)
        return
      }
      const bo = el.businessObject
      setSelected({
        id: el.id,
        name: bo.name ?? '',
        approvers: String(bo.get('flowlite:approvers') ?? ''),
        mode: String(bo.get('flowlite:mode') ?? 'any'),
        escalateTo: String(bo.get('flowlite:escalateTo') ?? ''),
        timeoutMinutes: Number(bo.get('flowlite:timeoutMinutes') ?? 0) || 0,
      })
    }
    eventBus.on('selection.changed', onSelection)
    return () => eventBus.off('selection.changed', onSelection)
  }, [])

  const update = (patch: Partial<SelectedTask>) => {
    const modeler = modelerRef.current
    if (!modeler || !selected) return
    const next = { ...selected, ...patch }
    setSelected(next)
    const registry = modeler.get('elementRegistry') as any
    const modeling = modeler.get('modeling') as any
    const el = registry.get(selected.id)
    if (!el) return
    const props: Record<string, unknown> = {
      'flowlite:approvers': next.approvers.trim() || undefined,
      'flowlite:mode': next.mode === 'all' ? 'all' : undefined,
      'flowlite:escalateTo': next.escalateTo || undefined,
      'flowlite:timeoutMinutes': next.timeoutMinutes > 0 ? next.timeoutMinutes : undefined,
    }
    if (patch.name !== undefined) props.name = patch.name
    modeling.updateProperties(el, props)
  }

  const escalationTargets = (() => {
    const modeler = modelerRef.current
    if (!modeler) return [] as { value: string; label: string }[]
    const registry = modeler.get('elementRegistry') as any
    const out: { value: string; label: string }[] = []
    registry.filter((el: any) => el.businessObject?.$type === 'bpmn:UserTask' && el.id !== selected?.id).forEach((el: any) => {
      out.push({ value: el.id, label: el.businessObject?.name || el.id })
    })
    return out
  })()

  return (
    <div className={className}>
      {error ? (
        <div role="alert" className="mb-2 rounded border border-destructive/40 bg-destructive/5 px-3 py-2 text-xs text-destructive">
          {t('plugin_workflow.designer.importFailed')}: {error}
        </div>
      ) : null}
      <div className="flex min-w-0 flex-col gap-3 xl:flex-row">
        <div
          ref={containerRef}
          data-readonly={readOnly ? 'true' : undefined}
          className="wf-bpmn h-[360px] w-full min-w-0 overflow-hidden rounded-lg border bg-card md:h-[480px]"
          aria-label={t('plugin_workflow.designer.canvasLabel')}
        />
        {selected ? (
          <div className="w-full shrink-0 space-y-2 rounded-lg border bg-card p-3 text-sm xl:w-64">
            <div className="truncate font-medium">{selected.name || selected.id}</div>
            <label className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.nodeName')}</span>
              <Input value={selected.name} onChange={(e) => update({ name: e.target.value })} />
            </label>
            <div className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.approvers')}</span>
              <ApproverPicker
                value={selected.approvers
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean)}
                onChange={(ids) => update({ approvers: ids.join(',') })}
              />
            </div>
            <label className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.mode')}</span>
              <SimpleSelect
                className="w-full"
                aria-label={t('plugin_workflow.designer.mode')}
                value={selected.mode === 'all' ? 'all' : 'any'}
                onChange={(v) => update({ mode: v })}
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
                value={selected.escalateTo || '__none'}
                onChange={(v) => update({ escalateTo: v === '__none' ? '' : v })}
                options={[{ value: '__none', label: t('plugin_workflow.designer.noEscalate') }, ...escalationTargets]}
              />
            </label>
            <label className="flex flex-col gap-0.5">
              <span className="text-xs text-muted-foreground">{t('plugin_workflow.designer.timeoutMinutes')}</span>
              <Input
                type="number"
                min={0}
                value={selected.timeoutMinutes}
                onChange={(e) => update({ timeoutMinutes: Math.max(0, Number(e.target.value) || 0) })}
              />
            </label>
            <p className="pt-1 text-xs leading-5 text-muted-foreground">{t('plugin_workflow.designer.propsHint')}</p>
          </div>
        ) : (
          <div className="w-full shrink-0 space-y-1 rounded-lg border border-dashed bg-card p-3 text-sm xl:w-64">
            <div className="font-medium">{t('plugin_workflow.designer.propsTitle')}</div>
            <p className="text-xs leading-5 text-muted-foreground">{t('plugin_workflow.designer.propsEmpty')}</p>
          </div>
        )}
      </div>
    </div>
  )
}
