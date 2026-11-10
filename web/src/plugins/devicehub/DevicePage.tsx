import { useEffect, useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { CircleCheck, CircleX, Loader2, MapPin, Pencil, Plus, Trash2 } from 'lucide-react'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { JsonInput } from '@/framework/components/JsonInput'
import { FormGrid, FormGridFull, FormSection } from '@/framework/components/FormLayout'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SimpleSelect } from '@/framework/components/SimpleSelect'
import { SwitchRow } from '@/framework/components/SwitchField'
import { LocationField, type GeoPoint } from '@/framework/map'
import { TrendChart } from '@/framework/components/TrendChart'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import { cn } from '@/lib/utils'
import { formatAgo, formatTime, formatTimeSec } from './format'
import {
  exportHistory,
  useAlarmEvents,
  useAlarmRules,
  useCommands,
  useCreateCommand,
  useDeleteAlarmRule,
  useDeleteDevice,
  useDeletePoint,
  useDeviceFeed,
  useDeviceStream,
  useDevices,
  useHistory,
  usePoints,
  useSaveAlarmRule,
  useSaveDevice,
  useSavePoint,
  type AlarmLevel,
  type AlarmOp,
  type CommandRow,
  type Device,
  type DeviceProtocol,
  type Point,
  type PointDataType,
  type PointInput,
} from './api'

const PROTOCOLS: DeviceProtocol[] = ['modbus-tcp', 'modbus-rtu', 'mqtt', 's7', 'opcua']
const DATA_TYPES: PointDataType[] = ['bool', 'int16', 'uint16', 'int32', 'float32']
const OPS: AlarmOp[] = ['gt', 'lt', 'ge', 'le', 'eq', 'ne']
const LEVELS: AlarmLevel[] = ['info', 'warn', 'critical']

function statusVariant(s: string): 'success' | 'destructive' | 'outline' {
  if (s === 'online') return 'success'
  if (s === 'offline') return 'destructive'
  return 'outline'
}

function levelVariant(l: string): 'warning' | 'destructive' | 'info' {
  if (l === 'critical') return 'destructive'
  if (l === 'warn') return 'warning'
  return 'info'
}

export default function DevicePage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const queryClient = useQueryClient()
  const [params, setParams] = useSearchParams()
  const devices = useDevices()
  const list = useMemo(() => devices.data ?? [], [devices.data])

  // URL ?device=DEV... 直达某台设备（站内信/书签）；默认选第一台
  const [selectedNo, setSelectedNo] = useState<string | undefined>(params.get('device') ?? undefined)
  useEffect(() => {
    if (!selectedNo && list.length > 0) setSelectedNo(list[0].deviceNo)
  }, [list, selectedNo])
  const select = (no: string) => {
    setSelectedNo(no)
    const next = new URLSearchParams(params)
    next.set('device', no)
    setParams(next, { replace: true })
  }

  // 左栏设备列表的本地搜索：按名称或编号过滤（设备多了以后没有搜索就没法用）。
  // 在线在前、离线其次、停用沉底——监控页默认关心活着的设备；同档内按名称排。
  const [kw, setKw] = useState('')
  const filtered = useMemo(() => {
    const k = kw.trim().toLowerCase()
    const rows = k ? list.filter((d) => d.name.toLowerCase().includes(k) || d.deviceNo.toLowerCase().includes(k)) : [...list]
    const rank = (s: string) => (s === 'online' ? 0 : s === 'offline' ? 1 : 2)
    return rows.sort((a, b) => rank(a.status) - rank(b.status) || a.name.localeCompare(b.name))
  }, [list, kw])

  // WS：命中帧即刷对应查询（8s 轮询兜底）
  useDeviceFeed(() => {
    void queryClient.invalidateQueries({ queryKey: ['plugin_devicehub', 'devices'] })
  })
  useDeviceStream(selectedNo, (f) => {
    if (f.type === 'status') {
      void queryClient.invalidateQueries({ queryKey: ['plugin_devicehub', 'devices'] })
      return
    }
    void queryClient.invalidateQueries({ queryKey: ['plugin_devicehub', 'points', selectedNo] })
    void queryClient.invalidateQueries({ queryKey: ['plugin_devicehub', 'alarms', selectedNo] })
  })

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Device | null>(null)

  const openCreate = () => {
    setEditing(null)
    setFormOpen(true)
  }
  const openEdit = (d: Device) => {
    setEditing(d)
    setFormOpen(true)
  }
  const del = useDeleteDevice()
  const onDelete = async (d: Device) => {
    if (!(await confirm({ destructive: true, description: t('plugin_devicehub.device.deleteConfirm', { name: d.name }) }))) return
    try {
      await del.mutateAsync(d.deviceNo)
      toast.success(t('common.operateSuccess'))
      setSelectedNo(undefined)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_devicehub.title')}
        description={t('plugin_devicehub.description')}
        actions={
          <Can perm="devicehub:edit">
            <Button onClick={openCreate}>
              <Plus aria-hidden="true" />
              {t('plugin_devicehub.device.add')}
            </Button>
          </Can>
        }
      />
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[20rem_1fr]">
        {/* 设备列表：标题 + 搜索 + 滚动列表。self-start：卡随内容收口，不被右侧详情的行高拉长成一块空板；
            高度仍封顶，设备多了在卡内滚动。 */}
        <Card className="flex max-h-[70vh] min-h-0 flex-col gap-2 self-start p-2 xl:max-h-[calc(100dvh-11rem)]">
          <div className="flex items-center justify-between gap-2 px-1 pt-1">
            <span className="text-sm font-medium text-foreground">{t('plugin_devicehub.device.list')}</span>
            <span className="num text-xs text-muted-foreground">{filtered.length}</span>
          </div>
          <Input
            className="h-8"
            value={kw}
            onChange={(e) => setKw(e.target.value)}
            placeholder={t('plugin_devicehub.device.search')}
            aria-label={t('plugin_devicehub.device.search')}
          />
          {devices.isLoading ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t('common.loading')}</p>
          ) : list.length === 0 ? (
            <EmptyState title={t('plugin_devicehub.device.empty')} className="py-8" />
          ) : filtered.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t('plugin_devicehub.device.noMatch')}</p>
          ) : (
            <div className="min-h-0 flex-1 overflow-y-auto">
              {filtered.map((d) => (
                <button
                  key={d.id}
                  type="button"
                  onClick={() => select(d.deviceNo)}
                  className={cn(
                    'flex w-full flex-col gap-1 rounded-md px-2 py-2 text-start transition-colors hover:bg-accent',
                    d.deviceNo === selectedNo && 'bg-accent',
                  )}
                >
                  <span className="flex w-full items-center gap-2">
                    <span className="min-w-0 flex-1 truncate font-medium text-foreground">{d.name}</span>
                    <Badge variant={statusVariant(d.status)}>{t(`plugin_devicehub.status.${d.status}`)}</Badge>
                  </span>
                  <span className="flex w-full items-center gap-2 text-xs text-muted-foreground">
                    <span className="min-w-0 flex-1 truncate font-mono">{d.deviceNo}</span>
                    <span className="shrink-0">{t(`plugin_devicehub.protocol.${d.protocol}`)}</span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </Card>

        {/* 详情 */}
        {selectedNo ? (
          <DeviceDetail deviceNo={selectedNo} onEdit={openEdit} onDelete={onDelete} />
        ) : (
          <Card className="flex items-center justify-center text-sm text-muted-foreground">
            {t('plugin_devicehub.pickDevice')}
          </Card>
        )}
      </div>

      {formOpen ? <DeviceFormDialog device={editing} onOpenChange={setFormOpen} /> : null}
    </PageContainer>
  )
}

function DeviceDetail({
  deviceNo,
  onEdit,
  onDelete,
}: {
  deviceNo: string
  onEdit: (d: Device) => void
  onDelete: (d: Device) => void
}) {
  const { t } = useTranslation()
  const devices = useDevices()
  const d = (devices.data ?? []).find((x) => x.deviceNo === deviceNo)
  const points = usePoints(deviceNo)
  const alarms = useAlarmEvents(deviceNo)
  // 告警/指令用 Tab 切换，实时值常驻上方
  const [tab, setTab] = useState('points')

  return (
    <Card className="flex min-h-0 flex-col gap-3 p-4">
      {/* 头部两行：第一行设备名 + 状态 + 操作；第二行编号/协议/地址，视觉上不与操作按钮抢一行 */}
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-base font-semibold text-foreground">{d?.name}</span>
            {d ? <Badge variant={statusVariant(d.status)}>{t(`plugin_devicehub.status.${d.status}`)}</Badge> : null}
          </div>
          <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">
            {deviceNo}
            {d ? ` · ${t(`plugin_devicehub.protocol.${d.protocol}`)} · ${d.endpoint}` : ''}
          </p>
          {d?.location ? (
            <p className="mt-0.5 truncate text-xs text-muted-foreground" title={d.location}>
              <MapPin aria-hidden="true" className="me-1 inline size-3" />
              {d.location}
              {d.lng != null && d.lat != null ? `（${d.lng.toFixed(6)}, ${d.lat.toFixed(6)}）` : ''}
            </p>
          ) : null}
          {/* 离线设备最想知道「多久没上来了」：状态徽标只说离线，说不清是刚掉还是停了三天 */}
          {d && d.status !== 'online' ? (
            <p className="mt-0.5 text-xs text-muted-foreground">
              {d.lastSeenAt ? t('plugin_devicehub.device.lastSeenAgo', { ago: formatAgo(d.lastSeenAt) }) : t('plugin_devicehub.device.neverSeen')}
            </p>
          ) : null}
        </div>
        {d ? (
          <span className="flex shrink-0 items-center gap-1">
            <Can perm="devicehub:edit">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => onEdit(d)}>
                <Pencil aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="devicehub:edit">
              {/* 二次确认只由 onDelete 负责：这里再 confirm 一次会让用户连着点两个「确认」 */}
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="text-destructive"
                aria-label={t('common.delete')}
                onClick={() => onDelete(d)}
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </span>
        ) : null}
      </div>

      {/* 实时值卡片：只放启用的测点（停用测点常年显示「从未采集」，是纯噪音），高度封顶可滚。
          右上角给「多久没更新」：值自身上没有时间戳变化，采集停了页面看起来和正常一模一样。 */}
      {(() => {
        const live = (points.data ?? []).filter((p) => p.enabled)
        const latest = live.reduce<string | null>((acc, p) => (p.lastTs && (!acc || p.lastTs > acc) ? p.lastTs : acc), null)
        const stale = latest != null && Date.now() - new Date(latest).getTime() > 90_000
        return (
          <>
            {live.length > 0 ? (
              <p className={cn('text-[11px]', stale ? 'text-warning' : 'text-muted-foreground')}>
                {latest
                  ? stale
                    ? t('plugin_devicehub.live.stale', { ago: formatAgo(latest) })
                    : t('plugin_devicehub.live.updated', { ago: formatAgo(latest) })
                  : t('plugin_devicehub.live.none')}
              </p>
            ) : null}
            <div className="max-h-56 overflow-y-auto">
              <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                {live.map((p) => (
                  <div key={p.id} className="rounded-lg border border-border p-2.5">
                    <p className="truncate text-xs text-muted-foreground" title={p.name}>
                      {p.name}
                    </p>
                    <p className="mt-1 flex items-baseline gap-1">
                      <span className="truncate text-lg font-semibold text-foreground tabular-nums">
                        {p.lastTs ? (p.lastValueStr !== '' && p.lastValueStr !== undefined ? p.lastValueStr : fmtNum(p.lastValueNum)) : '—'}
                      </span>
                      {p.unit ? <span className="text-xs text-muted-foreground">{p.unit}</span> : null}
                    </p>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      {p.lastTs ? formatTime(p.lastTs) : t('plugin_devicehub.point.never')}
                    </p>
                  </div>
                ))}
                {(points.data ?? []).length === 0 ? (
                  <p className="col-span-full rounded-lg border border-dashed border-border py-4 text-center text-sm text-muted-foreground">
                    {t('plugin_devicehub.point.empty')}
                  </p>
                ) : null}
              </div>
            </div>
          </>
        )
      })()}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="w-fit">
          <TabsTrigger value="points">{t('plugin_devicehub.tab.points')}</TabsTrigger>
          <TabsTrigger value="history">{t('plugin_devicehub.tab.history')}</TabsTrigger>
          <TabsTrigger value="commands">{t('plugin_devicehub.tab.commands')}</TabsTrigger>
          <TabsTrigger value="alarms">{t('plugin_devicehub.tab.alarms')}</TabsTrigger>
        </TabsList>
        <TabsContent value="points">
          <PointsPanel key={deviceNo} deviceNo={deviceNo} points={points.data ?? []} />
        </TabsContent>
        <TabsContent value="history">
          {/* key：换设备必须重挂面板。否则本地 state 还指着上一台的测点，历史的
              keepPreviousData 还会把上一台的数据当占位图先画在新设备的名字下面。 */}
          <HistoryPanel key={deviceNo} deviceNo={deviceNo} points={points.data ?? []} />
        </TabsContent>
        <TabsContent value="commands">
          <CommandsPanel key={deviceNo} deviceNo={deviceNo} points={points.data ?? []} />
        </TabsContent>
        <TabsContent value="alarms">
          <AlarmsPanel key={deviceNo} points={points.data ?? []} events={alarms.data ?? []} />
        </TabsContent>
      </Tabs>
    </Card>
  )
}

function fmtNum(v?: number | null): string {
  if (v === null || v === undefined) return '—'
  return Math.abs(v - Math.trunc(v)) < 1e-9 ? String(Math.trunc(v)) : String(Math.round(v * 1000) / 1000)
}

// ---- 测点管理 ----

function PointsPanel({ deviceNo, points }: { deviceNo: string; points: Point[] }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const del = useDeletePoint(deviceNo)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Point | null>(null)

  const openForm = (p: Point | null) => {
    setEditing(p)
    setOpen(true)
  }
  const remove = async (p: Point) => {
    if (!(await confirm({ destructive: true, description: t('plugin_devicehub.point.deleteConfirm', { name: p.name }) }))) return
    try {
      await del.mutateAsync(p.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <div className="space-y-2">
      <Can perm="devicehub:edit">
        <Button size="sm" variant="outline" onClick={() => openForm(null)}>
          <Plus aria-hidden="true" />
          {t('plugin_devicehub.point.add')}
        </Button>
      </Can>
      {points.length === 0 ? (
        <EmptyState title={t('plugin_devicehub.point.empty')} className="py-6" />
      ) : (
        <div className="divide-y divide-border rounded-lg border border-border">
          {/* 列标签：地址是等宽串、单位更像跟在值后面，没有表头时一行读起来像「母线电压 hr:40003 V」 */}
          <div className="hidden grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_4rem_5rem] gap-2 bg-muted/40 px-3 py-1.5 text-[11px] font-medium text-muted-foreground sm:grid">
            <span>{t('plugin_devicehub.point.name')}</span>
            <span>{t('plugin_devicehub.point.address')}</span>
            <span>{t('plugin_devicehub.point.unit')}</span>
            <span className="text-end">{t('common.actions')}</span>
          </div>
          {points.map((p) => (
            <div
              key={p.id}
              className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 px-3 py-2 text-sm sm:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_4rem_5rem]"
            >
              <span className="min-w-0">
                <span className="flex items-center gap-2">
                  <span className="truncate font-medium text-foreground">{p.name}</span>
                  {p.rw === 'rw' ? <Badge variant="secondary" className="shrink-0">{t('plugin_devicehub.point.rw')}</Badge> : null}
                  {!p.enabled ? <Badge variant="outline" className="shrink-0">{t('plugin_devicehub.point.disabled')}</Badge> : null}
                </span>
                {/* 窄屏没有地址列，地址折到名称下面；宽屏走独立列 */}
                <span className="mt-0.5 block truncate font-mono text-xs text-muted-foreground sm:hidden">
                  {p.address}
                  {p.unit ? ` · ${p.unit}` : ''}
                </span>
              </span>
              <span className="hidden truncate font-mono text-xs text-muted-foreground sm:block">{p.address}</span>
              <span className="hidden truncate text-xs text-muted-foreground sm:block">{p.unit || '—'}</span>
              <span className="flex justify-end">
                <Can perm="devicehub:edit">
                  <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(p)}>
                    <Pencil aria-hidden="true" />
                  </Button>
                  <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void remove(p)}>
                    <Trash2 aria-hidden="true" />
                  </Button>
                </Can>
              </span>
            </div>
          ))}
        </div>
      )}
      {open ? <PointFormDialog deviceNo={deviceNo} point={editing} onOpenChange={setOpen} /> : null}
    </div>
  )
}

function PointFormDialog({ deviceNo, point, onOpenChange }: { deviceNo: string; point: Point | null; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const save = useSavePoint(deviceNo)
  // 地址格式按当前设备的协议给：四种协议并列成一段会把首行撑成半屏，也逼用户自己挑
  const devices = useDevices()
  const protocol = (devices.data ?? []).find((d) => d.deviceNo === deviceNo)?.protocol ?? 'modbus-tcp'
  const [form, setForm] = useState<PointInput>(
    point
      ? {
          name: point.name, address: point.address, unit: point.unit, rw: point.rw,
          dataType: point.dataType, scale: point.scale, pollIntervalMs: point.pollIntervalMs,
          deadband: point.deadband, enabled: point.enabled, sort: point.sort,
        }
      : { name: '', address: '', unit: '', rw: 'r', dataType: 'float32', scale: 1, pollIntervalMs: 0, deadband: 0, enabled: true, sort: 0 },
  )
  const submit = async () => {
    if (!form.name.trim() || !form.address.trim()) {
      toast.error(t('plugin_devicehub.point.required'))
      return
    }
    try {
      await save.mutateAsync({ ...form, pointId: point?.id })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }
  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={point ? t('plugin_devicehub.point.edit') : t('plugin_devicehub.point.add')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button disabled={save.isPending} onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <FormSection title={t('plugin_devicehub.point.section.basic')}>
          <FormGrid cols={2}>
            <Field label={t('plugin_devicehub.point.name')} required>
              <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} maxLength={100} />
            </Field>
            <Field label={t('plugin_devicehub.point.unit')}>
              <Input value={form.unit} onChange={(e) => setForm((f) => ({ ...f, unit: e.target.value }))} maxLength={16} placeholder={t('plugin_devicehub.point.unitPlaceholder')} />
            </Field>
            {/* 地址是等宽长串、又是必填的关键字段，独占整行 */}
            <FormGridFull>
              <Field
                label={t('plugin_devicehub.point.address')}
                required
                hint={<code className="font-mono">{t(`plugin_devicehub.point.addressFormat.${protocol}`)}</code>}
              >
                <Input value={form.address} onChange={(e) => setForm((f) => ({ ...f, address: e.target.value }))} className="font-mono" maxLength={128} />
              </Field>
            </FormGridFull>
            <Field label={t('plugin_devicehub.point.dataType')}>
              <SimpleSelect
                value={form.dataType}
                onChange={(v) => setForm((f) => ({ ...f, dataType: v as PointDataType }))}
                options={DATA_TYPES.map((dt) => ({ value: dt, label: dt }))}
                aria-label={t('plugin_devicehub.point.dataType')}
              />
            </Field>
            <Field label={t('plugin_devicehub.point.rw')}>
              <SimpleSelect
                value={form.rw}
                onChange={(v) => setForm((f) => ({ ...f, rw: v as PointInput['rw'] }))}
                options={[
                  { value: 'r', label: t('plugin_devicehub.point.readonly') },
                  { value: 'rw', label: t('plugin_devicehub.point.readwrite') },
                ]}
                aria-label={t('plugin_devicehub.point.rw')}
              />
            </Field>
          </FormGrid>
        </FormSection>
        {/* 换算与落库规则说明集中成一段，避免每个数字框下面各挂两行小字 */}
        <FormSection title={t('plugin_devicehub.point.section.collect')} description={t('plugin_devicehub.point.collectHint')}>
          <FormGrid cols={3}>
            <Field label={t('plugin_devicehub.point.scale')}>
              <Input type="number" step="0.001" min="0" className="num" value={form.scale} onChange={(e) => setForm((f) => ({ ...f, scale: Number(e.target.value) || 1 }))} />
            </Field>
            <Field label={t('plugin_devicehub.point.deadband')}>
              <Input type="number" step="0.001" min="0" className="num" value={form.deadband} onChange={(e) => setForm((f) => ({ ...f, deadband: Number(e.target.value) || 0 }))} />
            </Field>
            <Field label={t('plugin_devicehub.point.sort')}>
              <Input type="number" className="num" value={form.sort} onChange={(e) => setForm((f) => ({ ...f, sort: Math.trunc(Number(e.target.value)) || 0 }))} />
            </Field>
          </FormGrid>
        </FormSection>
        <SwitchRow
          label={t('plugin_devicehub.point.enabled')}
          description={t('plugin_devicehub.point.enabledHint')}
          checked={form.enabled}
          onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
        />
      </div>
    </ResponsiveDialog>
  )
}

function Field({ label, required, hint, children }: { label: string; required?: boolean; hint?: React.ReactNode; children: React.ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-foreground">
        {label}
        {required ? <span className="text-destructive"> *</span> : null}
      </span>
      {children}
      {hint ? <span className="block text-[11px] leading-relaxed text-muted-foreground">{hint}</span> : null}
    </label>
  )
}

// ---- 历史趋势 ----

function HistoryPanel({ deviceNo, points }: { deviceNo: string; points: Point[] }) {
  const { t } = useTranslation()
  const numeric = points.filter((p) => p.dataType !== 'bool')
  const [pickedId, setPickedId] = useState(numeric[0]?.id ?? '')
  // 换设备后面板不会重挂，选中项必须按当前设备的测点校验：否则还拿着上一台的 pointId 去查，
  // 图表要么空、要么是别人的数据。测点被删掉时同理回落到第一个。
  const pointId = numeric.some((p) => p.id === pickedId) ? pickedId : (numeric[0]?.id ?? '')
  const [hours, setHours] = useState(24)
  const query = useMemo(() => {
    const to = new Date()
    const from = new Date(to.getTime() - hours * 3600_000)
    return { pointId, from: from.toISOString(), to: to.toISOString(), bucket: hours > 48 ? 'hour' : 'raw' }
  }, [pointId, hours])
  const history = useHistory(deviceNo, query)
  const rows = history.data?.rows ?? []
  // 后端按查询窗口降采样，bucket 回传实际采样间隔（raw/hour/5s/2m…），展示出来避免误读
  const bucket = history.data?.bucket ?? ''
  const resolution =
    bucket === 'raw'
      ? t('plugin_devicehub.history.raw')
      : bucket === 'hour'
        ? t('plugin_devicehub.history.hour')
        : bucket
          ? t('plugin_devicehub.history.resolution', { bucket })
          : ''
  // 秒级间隔精确到秒，其余沿用分钟粒度
  const fmtTs = bucket.endsWith('s') ? formatTimeSec : formatTime

  // 没有数值型测点时连选择器与图表一起收掉：只留一副空图框架，等于假装「有数据但画不出来」
  if (numeric.length === 0) return <EmptyState title={t('plugin_devicehub.history.noNumeric')} className="py-8" />

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SimpleSelect
          className="w-56"
          aria-label={t('plugin_devicehub.point.name')}
          value={pointId}
          onChange={setPickedId}
          options={numeric.map((p) => ({ value: p.id, label: p.name }))}
        />
        <SimpleSelect
          className="w-36"
          aria-label={t('plugin_devicehub.history.range')}
          value={String(hours)}
          onChange={(v) => setHours(Number(v))}
          options={[
            { value: '1', label: t('plugin_devicehub.history.h1') },
            { value: '24', label: t('plugin_devicehub.history.h24') },
            { value: '168', label: t('plugin_devicehub.history.d7') },
            { value: '720', label: t('plugin_devicehub.history.d30') },
          ]}
        />
      </div>
      {rows.length === 0 ? (
        <EmptyState title={t('plugin_devicehub.history.empty')} className="py-8" />
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2">
            {resolution ? <span className="text-xs text-muted-foreground">{resolution}</span> : null}
            <Button size="sm" variant="outline" className="ms-auto" onClick={() => void exportHistory(deviceNo, query)}>
              {t('plugin_devicehub.history.export')}
            </Button>
          </div>
          <TrendChart
            data={rows.map((r) => ({ ts: fmtTs(r.ts), avg: r.avg, min: r.min, max: r.max }))}
            series={[
              { key: 'avg', label: t('plugin_devicehub.history.avg') },
              { key: 'min', label: t('plugin_devicehub.history.min') },
              { key: 'max', label: t('plugin_devicehub.history.max') },
            ]}
            xKey="ts"
          />
        </>
      )}
    </div>
  )
}

// ---- 指令下发 ----

function CommandsPanel({ deviceNo, points }: { deviceNo: string; points: Point[] }) {
  const { t } = useTranslation()
  const writable = points.filter((p) => p.rw === 'rw')
  const create = useCreateCommand(deviceNo)
  const commands = useCommands(deviceNo)
  const [pickedId, setPickedId] = useState(writable[0]?.id ?? '')
  // 同 HistoryPanel：换设备后按当前设备的可写测点校验，不能沿用上一台的 pointId
  const pointId = writable.some((p) => p.id === pickedId) ? pickedId : (writable[0]?.id ?? '')
  const target = writable.find((x) => x.id === pointId)
  const [value, setValue] = useState('')

  const submit = async () => {
    if (!pointId || value === '') {
      toast.error(t('plugin_devicehub.command.required'))
      return
    }
    const p = writable.find((x) => x.id === pointId)
    // bool 测点只接受 0/1
    if (p?.dataType === 'bool' && value !== '0' && value !== '1') {
      toast.error(t('plugin_devicehub.command.boolOnly'))
      return
    }
    try {
      await create.mutateAsync({ pointId, value: Number(value) })
      toast.success(t('plugin_devicehub.command.sent'))
      setValue('')
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <div className="space-y-3">
      <Can perm="devicehub:control">
        <div className="flex flex-wrap items-end gap-2">
          <label className="block space-y-1">
            <span className="text-xs font-medium text-foreground">{t('plugin_devicehub.command.point')}</span>
            <SimpleSelect
              className="w-56"
              aria-label={t('plugin_devicehub.command.point')}
              value={pointId}
              onChange={setPickedId}
              options={writable.map((p) => ({ value: p.id, label: p.name }))}
            />
            {/* 下达前得看见「现在是多少」：否则只能凭记忆填一个数，填错就是一台设备跑飞 */}
            <span className="block text-[11px] text-muted-foreground">
              {target
                ? t('plugin_devicehub.command.current', {
                    value: target.lastTs
                      ? `${target.lastValueStr !== '' && target.lastValueStr !== undefined ? target.lastValueStr : fmtNum(target.lastValueNum)}${target.unit ? ` ${target.unit}` : ''}`
                      : t('plugin_devicehub.point.never'),
                  })
                : null}
            </span>
          </label>
          <label className="block space-y-1">
            <span className="text-xs font-medium text-foreground">
              {t('plugin_devicehub.command.value')}
              <span className="text-destructive"> *</span>
            </span>
            {target?.dataType === 'bool' ? (
              // 开关量不给自由输入：只能填 0/1，让用户手打纯属制造一次报错
              <SimpleSelect
                className="w-32"
                aria-label={t('plugin_devicehub.command.value')}
                value={value}
                onChange={setValue}
                options={[
                  { value: '1', label: '1 · ON' },
                  { value: '0', label: '0 · OFF' },
                ]}
              />
            ) : (
              <Input className="w-32" value={value} onChange={(e) => setValue(e.target.value)} placeholder={t('plugin_devicehub.command.valuePlaceholder')} />
            )}
          </label>
          <Button onClick={() => void submit()} disabled={create.isPending}>
            {t('plugin_devicehub.command.send')}
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">{t('plugin_devicehub.command.hint')}</p>
      </Can>
      {commands.data?.length ? (
        <div className="divide-y divide-border rounded-lg border border-border">
          {(commands.data as CommandRow[]).map((cmd) => (
            <div key={cmd.id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
              <span className="font-mono text-xs text-muted-foreground">#{cmd.id}</span>
              {cmd.status === 'done' ? (
                <CircleCheck aria-hidden="true" className="size-4 text-success" />
              ) : cmd.status === 'queued' ? (
                <Loader2 aria-hidden="true" className="size-4 animate-spin text-muted-foreground motion-reduce:animate-none" />
              ) : (
                <CircleX aria-hidden="true" className="size-4 text-destructive" />
              )}
              <span className="text-xs">{t(`plugin_devicehub.command.status.${cmd.status}`)}</span>
              <span className="font-mono text-xs">{cmd.rawValue}</span>
              <span className="text-xs text-muted-foreground">{cmd.result}</span>
              <span className="ms-auto text-xs text-muted-foreground">{formatTime(cmd.createdAt)}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

// ---- 告警 ----

function AlarmsPanel({
  points,
  events,
}: {
  points: Point[]
  events: { id: string; level: string; message: string; createdAt: string }[]
}) {
  const { t } = useTranslation()
  const [ruleOpen, setRuleOpen] = useState(false)

  return (
    <div className="space-y-5">
      {/* 规则与事件是两种东西（一处配置、一处流水），挤在一起时分不清哪行是规则哪行是历史告警 */}
      <section className="space-y-2">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-sm font-medium text-foreground">{t('plugin_devicehub.alarm.rulesTitle')}</h3>
          <Can perm="devicehub:edit">
            <Button size="sm" variant="outline" onClick={() => setRuleOpen(true)}>
              <Plus aria-hidden="true" />
              {t('plugin_devicehub.alarm.addRule')}
            </Button>
          </Can>
        </div>
        {/* 已配置的规则：能建就要能看、能删，否则改阈值只能靠重建（没有删除入口，规则会越堆越多）。
            没有规则的测点整块不渲染，避免出现一堆空标题。 */}
        {points.map((p) => (
          <PointRules key={p.id} point={p} />
        ))}
      </section>
      <section className="space-y-2">
        <h3 className="text-sm font-medium text-foreground">{t('plugin_devicehub.alarm.eventsTitle')}</h3>
        {events.length === 0 ? (
          <EmptyState title={t('plugin_devicehub.alarm.empty')} className="py-6" />
        ) : (
          <div className="divide-y divide-border rounded-lg border border-border">
            {events.map((e) => (
              <div key={e.id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                <Badge variant={levelVariant(e.level)}>{t(`plugin_devicehub.alarm.level.${e.level}`)}</Badge>
                <span className="min-w-0 flex-1 truncate">{e.message}</span>
                <span className="text-xs text-muted-foreground">{formatTime(e.createdAt)}</span>
              </div>
            ))}
          </div>
        )}
      </section>
      {ruleOpen ? <AlarmRuleDialog points={points} onOpenChange={setRuleOpen} /> : null}
    </div>
  )
}

// PointRules 是某个测点的告警规则行：没有规则就整块不渲染（避免出现空标题）。
function PointRules({ point }: { point: Point }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const del = useDeleteAlarmRule()
  const rules = useAlarmRules(point.id)
  const rows = useMemo(() => rules.data ?? [], [rules.data])
  if (!rows.length) return null
  return (
    <div className="divide-y divide-border rounded-lg border border-border">
      {rows.map((r) => (
        <div key={r.id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
          <Badge variant={levelVariant(r.level)}>{t(`plugin_devicehub.alarm.level.${r.level}`)}</Badge>
          <span className="min-w-0 truncate font-medium">{point.name}</span>
          <span className="num text-muted-foreground">
            {opSymbol(r.op)} {r.threshold}
          </span>
          <span className="min-w-0 flex-1 truncate text-muted-foreground">{r.name}</span>
          {!r.enabled ? <span className="text-xs text-muted-foreground">{t('plugin_devicehub.alarm.ruleDisabled')}</span> : null}
          {r.lastFiredAt ? (
            <span className="text-xs text-muted-foreground">
              {t('plugin_devicehub.alarm.lastFired')} {formatTime(r.lastFiredAt)}
            </span>
          ) : null}
          <Can perm="devicehub:edit">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('plugin_devicehub.alarm.deleteRule')}
              disabled={del.isPending}
              onClick={async () => {
                if (!(await confirm({ destructive: true, description: t('plugin_devicehub.alarm.deleteRuleConfirm', { name: r.name }) }))) return
                try {
                  await del.mutateAsync(r.id)
                  toast.success(t('common.operateSuccess'))
                } catch (e) {
                  toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
                }
              }}
            >
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        </div>
      ))}
    </div>
  )
}

function AlarmRuleDialog({ points, onOpenChange }: { points: Point[]; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const save = useSaveAlarmRule()
  const [pointId, setPointId] = useState(points[0]?.id ?? '')
  const [name, setName] = useState('')
  const [op, setOp] = useState<AlarmOp>('gt')
  const [threshold, setThreshold] = useState('')
  const [level, setLevel] = useState<AlarmLevel>('warn')
  const [cooldown, setCooldown] = useState(300)

  const submit = async () => {
    if (!pointId || !name.trim() || threshold === '') {
      toast.error(t('plugin_devicehub.alarm.required'))
      return
    }
    try {
      await save.mutateAsync({
        pointId, name: name.trim(), op, threshold: Number(threshold), level, enabled: true, cooldownSeconds: cooldown,
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={t('plugin_devicehub.alarm.addRule')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button disabled={save.isPending} onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('plugin_devicehub.point.name')} required>
          <SimpleSelect
            className="w-full"
            aria-label={t('plugin_devicehub.point.name')}
            value={pointId}
            onChange={setPointId}
            options={points.map((p) => ({ value: p.id, label: p.name }))}
          />
        </Field>
        <Field label={t('plugin_devicehub.alarm.name')} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
        </Field>
        <Field label={t('plugin_devicehub.alarm.op')}>
          <SimpleSelect
            value={op}
            onChange={(v) => setOp(v as AlarmOp)}
            options={OPS.map((o) => ({ value: o, label: opSymbol(o) }))}
            aria-label={t('plugin_devicehub.alarm.op')}
          />
        </Field>
        <Field label={t('plugin_devicehub.alarm.threshold')} required>
          <Input type="number" step="0.001" value={threshold} onChange={(e) => setThreshold(e.target.value)} />
        </Field>
        <Field label={t('plugin_devicehub.alarm.levelText')}>
          <SimpleSelect
            value={level}
            onChange={(v) => setLevel(v as AlarmLevel)}
            options={LEVELS.map((l) => ({ value: l, label: t(`plugin_devicehub.alarm.level.${l}`) }))}
            aria-label={t('plugin_devicehub.alarm.levelText')}
          />
        </Field>
        <Field label={t('plugin_devicehub.alarm.cooldown')} hint={t('plugin_devicehub.alarm.cooldownHint')}>
          <Input type="number" min="0" value={cooldown} onChange={(e) => setCooldown(Math.trunc(Number(e.target.value)) || 0)} />
        </Field>
      </div>
    </ResponsiveDialog>
  )
}

function opSymbol(op: AlarmOp): string {
  return { gt: '>', lt: '<', ge: '>=', le: '<=', eq: '=', ne: '≠' }[op]
}

// 设备表单：协议相关字段切换
function DeviceFormDialog({ device, onOpenChange }: { device: Device | null; onOpenChange: (v: boolean) => void }) {
  const { t } = useTranslation()
  const save = useSaveDevice()
  const [form, setForm] = useState({
    name: device?.name ?? '',
    protocol: (device?.protocol ?? 'modbus-tcp') as DeviceProtocol,
    endpoint: device?.endpoint ?? '',
    config: device?.config ?? '',
    enabled: device?.enabled ?? true,
  })
  // 安装位置：LocationField 选点或手填；坐标存 WGS-84 原值
  const [loc, setLoc] = useState<GeoPoint | null>(
    device?.lng != null && device?.lat != null
      ? { lng: device.lng, lat: device.lat, address: device.location || undefined }
      : null,
  )

  const submit = async () => {
    if (!form.name.trim() || !form.endpoint.trim()) {
      toast.error(t('plugin_devicehub.device.required'))
      return
    }
    try {
      await save.mutateAsync({
        ...form,
        deviceNo: device?.deviceNo,
        location: loc ? { lng: loc.lng, lat: loc.lat, address: loc.address } : null,
      })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open
      onOpenChange={onOpenChange}
      title={device ? t('plugin_devicehub.device.edit') : t('plugin_devicehub.device.add')}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t('common.cancel')}</Button>
          <Button disabled={save.isPending} onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <FormSection title={t('plugin_devicehub.device.section.basic')}>
          <FormGrid cols={2}>
            <Field label={t('plugin_devicehub.device.name')} required>
              <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} maxLength={100} />
            </Field>
            <Field label={t('plugin_devicehub.device.protocol')} required>
              <SimpleSelect
                value={form.protocol}
                onChange={(v) => setForm((f) => ({ ...f, protocol: v as DeviceProtocol }))}
                options={PROTOCOLS.map((p) => ({ value: p, label: t(`plugin_devicehub.protocol.${p}`) }))}
                aria-label={t('plugin_devicehub.device.protocol')}
              />
            </Field>
          </FormGrid>
        </FormSection>
        <FormSection title={t('plugin_devicehub.device.section.connection')}>
          <FormGrid>
            <Field
              label={t('plugin_devicehub.device.endpoint')}
              required
              hint={
                form.protocol === 'mqtt'
                  ? t('plugin_devicehub.device.endpointMqttHint')
                  : form.protocol === 's7'
                    ? t('plugin_devicehub.device.endpointS7Hint')
                    : form.protocol === 'opcua'
                      ? t('plugin_devicehub.device.endpointOpcuaHint')
                      : t('plugin_devicehub.device.endpointHint')
              }
            >
              <Input
                value={form.endpoint}
                onChange={(e) => setForm((f) => ({ ...f, endpoint: e.target.value }))}
                className="font-mono"
                maxLength={255}
                placeholder={form.protocol === 'mqtt' ? 'tcp://192.168.1.10:1883' : '192.168.1.20:502'}
              />
            </Field>
            <Field
              label={t('plugin_devicehub.device.config')}
              hint={
                <>
                  <span className="block">{t(`plugin_devicehub.device.configExample.${form.protocol}`)}</span>
                  {/* 密码字段回显为 ***：原样提交即沿用已保存的密码，改密才需重填 */}
                  <span className="block">{t('plugin_devicehub.device.configSecretHint')}</span>
                </>
              }
            >
              <JsonInput
                rows={3}
                value={form.config}
                onChange={(v) => setForm((f) => ({ ...f, config: v }))}
                placeholder={'{"unitId":1,"pollIntervalMs":5000}'}
              />
            </Field>
          </FormGrid>
        </FormSection>
        {/* 安装位置：地图选点（地图插件）或手填坐标；坐标原值存 WGS-84 */}
        <FormSection title={t('plugin_devicehub.device.location')}>
          <LocationField addressLabel={t('plugin_devicehub.device.locationAddress')} value={loc} onChange={setLoc} />
        </FormSection>
        <SwitchRow
          label={t('plugin_devicehub.device.enabled')}
          description={t('plugin_devicehub.device.enabledHint')}
          checked={form.enabled}
          onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))}
        />
      </div>
    </ResponsiveDialog>
  )
}
