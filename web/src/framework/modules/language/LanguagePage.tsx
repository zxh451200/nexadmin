import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, GripVertical, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import { cn } from '@/lib/utils'
import { useAdminLanguages, useSaveLanguages, type LanguageRow } from './api'

/** 编辑期在 LanguageRow 上挂一个稳定 key：拖拽与未保存草稿都靠它认行。 */
type EditableRow = LanguageRow & { rowId: string }

const DRAFT_KEY = 'nex-language-draft'
let seq = 0
const nextId = () => `${Date.now().toString(36)}-${(seq++).toString(36)}`

const emptyRow = (): EditableRow => ({
  rowId: nextId(), code: '', name: '', dir: 'ltr', default: false, htmlLang: '', ogLocale: '', prefix: '', dateFormat: '',
})

// 保存是整表替换：用固定字段顺序做指纹（忽略 rowId），避免对象键序不同造成“永远显示未保存”。
const signature = (rows: LanguageRow[]) =>
  rows
    .map((r) => [r.code, r.name, r.dir, r.default ? '1' : '0', r.htmlLang, r.ogLocale, r.prefix, r.dateFormat].join('\u0001'))
    .join('\u0002')

const CODE_RE = /^[a-zA-Z]{2,3}(-[a-zA-Z0-9]+)*$/
const PREFIX_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/

/** 一行语言的问题；空串表示没问题。 */
function rowError(r: LanguageRow, rows: LanguageRow[], index: number, dupMsg: string, codeMsg: string, prefixMsg: string) {
  const code = r.code.trim()
  if (!CODE_RE.test(code)) return codeMsg
  if (rows.some((o, i) => i !== index && o.code.trim().toLowerCase() === code.toLowerCase())) return dupMsg
  if (!r.default && r.prefix && !PREFIX_RE.test(r.prefix.trim())) return prefixMsg
  return ''
}

/**
 * 平台语言清单：全站唯一的语言真源。
 *
 * 这里是整表替换（与后端一致），所以编辑发生在本地副本上，点保存才落库。
 * 前缀或语言集合变了需要重启才对公开站生效——后端会算，界面照实说，不让人白重启。
 * 日常只改「显示名称」，hreflang / og:locale / 日期格式是派生值，收在每张卡片的高级里。
 * 列表顺序就是语言切换器里的顺序，可拖拽调整；未保存的修改会暂存草稿，切走再回来能恢复。
 */
export function LanguageSection() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data, isLoading } = useAdminLanguages()
  const save = useSaveLanguages()
  const [rows, setRows] = useState<EditableRow[]>([])
  const [hydrated, setHydrated] = useState(false)
  const restoredDraft = useRef(false)
  const hydratedRef = useRef(false)
  // 服务端上一份数据的指纹：用来区分“确实刷新了”和“本地有改动，别覆盖”
  const serverSig = useRef('')
  // rows 的最新值：effect 里判断“是否与服务端一致”要用它，不能读闭包里的旧值
  const rowsRef = useRef<EditableRow[]>([])
  rowsRef.current = rows

  useEffect(() => {
    if (!data) return
    const next = data.map((r) => ({ ...r, rowId: nextId() }))
    // updater 必须保持纯函数：StrictMode 会拿同一份 prev 调用两次，
    // 所以“该不该采用服务端数据”要在外面算好，不能靠 updater 里改 ref 再判断。
    const firstLoad = !hydratedRef.current
    hydratedRef.current = true
    const cleanBefore = signature(rowsRef.current) === serverSig.current
    serverSig.current = signature(data)
    setHydrated(true)
    setRows((prev) => (firstLoad || cleanBefore ? next : prev))
  }, [data])

  // 未保存的修改落成草稿：切到别的分区会卸载本组件，回来（或刷新）时按草稿恢复。
  useEffect(() => {
    // 首次挂载、rows 还是空的：此时写草稿会把真草稿覆盖成 []，必须等数据落地后再记
    if (!data || !hydrated) return
    if (signature(rows) === signature(data)) {
      sessionStorage.removeItem(DRAFT_KEY)
      return
    }
    sessionStorage.setItem(DRAFT_KEY, JSON.stringify(rows))
  }, [rows, data, hydrated])

  useEffect(() => {
    if (!data || restoredDraft.current) return
    restoredDraft.current = true
    const raw = sessionStorage.getItem(DRAFT_KEY)
    if (!raw) return
    try {
      const draft = JSON.parse(raw) as EditableRow[]
      if (!Array.isArray(draft) || draft.length === 0 || signature(draft) === signature(data)) return
      setRows(draft.map((r) => ({ ...r, rowId: r.rowId || nextId() })))
      toast.info(t('language.restoredDraft'))
    } catch {
      sessionStorage.removeItem(DRAFT_KEY)
    }
  }, [data, t])

  useEffect(() => {
    if (!data || signature(rows) === signature(data)) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [rows, data])

  const baseline = useMemo(() => signature(data ?? []), [data])
  const dirty = signature(rows) !== baseline

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  const patch = (i: number, next: Partial<LanguageRow>) =>
    setRows((prev) => prev.map((r, k) => (k === i ? { ...r, ...next } : r)))
  const setDefault = (i: number) =>
    setRows((prev) => prev.map((r, k) => ({ ...r, default: k === i, prefix: k === i ? '' : r.prefix })))

  const remove = async (i: number) => {
    const row = rows[i]
    const ok = await confirm({
      destructive: true,
      description: t('language.removeConfirm', { name: row.name || row.code || '—' }),
    })
    if (ok) setRows((prev) => prev.filter((_, k) => k !== i))
  }

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    if (!over || active.id === over.id) return
    setRows((prev) => {
      const from = prev.findIndex((r) => r.rowId === active.id)
      const to = prev.findIndex((r) => r.rowId === over.id)
      return from < 0 || to < 0 ? prev : arrayMove(prev, from, to)
    })
  }

  const firstError = rows
    .map((r, i) => rowError(r, rows, i, t('language.codeDup'), t('language.codeInvalid'), t('language.prefixInvalid')))
    .find((err) => err !== '')

  const submit = async () => {
    if (firstError) {
      toast.error(firstError)
      return
    }
    try {
      const res = await save.mutateAsync(rows)
      const saved = res.languages ?? rows
      serverSig.current = signature(saved)
      setRows(saved.map((r) => ({ ...r, rowId: nextId() })))
      sessionStorage.removeItem(DRAFT_KEY)
      toast.success(res.needRestart ? t('language.savedRestart') : t('language.saved'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  if (isLoading) return <Skeleton className="h-64 w-full" />

  const canEdit = rows.length > 1

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">{t('language.desc')}</p>
        <Can perm="language:update">
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" onClick={() => setRows((prev) => [...prev, emptyRow()])}>
              <Plus aria-hidden="true" />
              {t('language.add')}
            </Button>
            <Button type="button" onClick={() => void submit()} disabled={save.isPending || !dirty}>
              {t('common.save')}
            </Button>
          </div>
        </Can>
      </div>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext items={rows.map((r) => r.rowId)} strategy={verticalListSortingStrategy}>
          <div className="flex flex-col gap-3">
            {rows.map((row, i) => {
              const err = rowError(row, rows, i, t('language.codeDup'), t('language.codeInvalid'), t('language.prefixInvalid'))
              return (
                <SortableCard key={row.rowId} id={row.rowId} label={t('language.reorder')} disabled={!canEdit}>
                  {(handle) => (
                    <Card className="flex flex-col gap-3 p-4">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex min-w-0 items-center gap-2">
                          {handle}
                          <span className="truncate font-mono text-sm font-medium">{row.code || '…'}</span>
                          {row.default ? <Badge variant="success">{t('language.defaultTag')}</Badge> : null}
                          <Badge variant={row.dir === 'rtl' ? 'secondary' : 'outline'}>{row.dir}</Badge>
                        </div>
                        <Can perm="language:update">
                          <div className="flex items-center gap-2">
                            {row.default ? null : (
                              <Button type="button" variant="ghost" size="sm" onClick={() => setDefault(i)}>
                                {t('language.setDefault')}
                              </Button>
                            )}
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              aria-label={row.default ? t('language.removeDefaultBlocked') : t('language.remove')}
                              disabled={row.default}
                              onClick={() => void remove(i)}
                            >
                              <Trash2 aria-hidden="true" className="text-destructive" />
                            </Button>
                          </div>
                        </Can>
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                        <Field label={t('language.fields.code')} error={err}>
                          <Input value={row.code} className="font-mono" placeholder="ja-JP" onChange={(e) => patch(i, { code: e.target.value })} />
                        </Field>
                        <Field label={t('language.fields.name')}>
                          <Input value={row.name} placeholder="日本語" onChange={(e) => patch(i, { name: e.target.value })} />
                        </Field>
                        <Field label={t('language.fields.prefix')} hint={row.default ? t('language.prefixRoot') : t('language.prefixHint')}>
                          <Input
                            value={row.prefix ?? ''}
                            className="font-mono"
                            placeholder={row.default ? '' : 'ja'}
                            disabled={row.default}
                            onChange={(e) => patch(i, { prefix: e.target.value })}
                          />
                        </Field>
                      </div>

                      {/* 派生字段：留空自动推导，平时不用动 */}
                      <details className="group rounded-md border border-border/60 px-3 py-2">
                        <summary className="flex cursor-pointer list-none items-center gap-1.5 text-xs text-muted-foreground [&::-webkit-details-marker]:hidden">
                          <ChevronDown aria-hidden="true" className="size-3.5 transition-transform group-open:rotate-180" />
                          {t('language.advanced')}
                        </summary>
                        <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                          <Field label={t('language.fields.htmlLang')} hint={t('language.derivedHint')}>
                            <Input value={row.htmlLang ?? ''} className="font-mono" placeholder={row.code} onChange={(e) => patch(i, { htmlLang: e.target.value })} />
                          </Field>
                          <Field label={t('language.fields.ogLocale')} hint={t('language.derivedHint')}>
                            <Input value={row.ogLocale ?? ''} className="font-mono" placeholder={row.code?.replace('-', '_')} onChange={(e) => patch(i, { ogLocale: e.target.value })} />
                          </Field>
                          <Field label={t('language.fields.dateFormat')} hint={t('language.dateFormatHint')}>
                            <Input value={row.dateFormat ?? ''} className="font-mono" placeholder="2006-01-02" onChange={(e) => patch(i, { dateFormat: e.target.value })} />
                          </Field>
                        </div>
                      </details>
                    </Card>
                  )}
                </SortableCard>
              )
            })}
          </div>
        </SortableContext>
      </DndContext>

      <p className="text-xs text-muted-foreground">{t('language.hint')}</p>
    </div>
  )
}

/** 可拖拽的卡片外壳：把手由调用方放到标题行里，键盘（Tab 到把手后空格+方向键）也能排序。 */
function SortableCard({ id, label, disabled, children }: { id: string; label: string; disabled?: boolean; children: (handle: ReactNode) => ReactNode }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id, disabled })
  const handle = (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      className={cn(
        'inline-flex size-7 shrink-0 items-center justify-center rounded text-muted-foreground',
        disabled ? 'cursor-default opacity-40' : 'cursor-grab hover:bg-muted hover:text-foreground active:cursor-grabbing',
      )}
      {...attributes}
      {...listeners}
    >
      <GripVertical aria-hidden="true" className="size-4" />
    </button>
  )
  return (
    <div ref={setNodeRef} style={{ transform: CSS.Transform.toString(transform), transition }} className={cn(isDragging && 'relative z-10 opacity-90')}>
      {children(handle)}
    </div>
  )
}

function Field({ label, hint, error, children }: { label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <label className={cn('flex min-w-0 flex-col gap-1 text-xs')}>
      <span className="text-muted-foreground">{label}</span>
      {children}
      {error ? <span className="text-destructive">{error}</span> : hint ? <span className="text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  )
}
