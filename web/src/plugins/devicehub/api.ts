// 设备接入与监控的 API 层：类型 + react-query hooks + WS 实时订阅。
import { useEffect, useRef } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { Page } from '@/framework/request/types'
import { useWs } from '@/framework/hooks/useWs'
import { useHiddenPause } from '@/framework/hooks/use-hidden-pause'

const BASE = '/plugins/devicehub'
const KEY = ['plugin_devicehub'] as const

export type DeviceProtocol = 'modbus-tcp' | 'modbus-rtu' | 'mqtt' | 's7' | 'opcua'
export type DeviceStatus = 'online' | 'offline' | 'disabled'
export type PointRW = 'r' | 'rw'
export type PointDataType = 'bool' | 'int16' | 'uint16' | 'int32' | 'float32'
export type AlarmLevel = 'info' | 'warn' | 'critical'
export type AlarmOp = 'gt' | 'lt' | 'ge' | 'le' | 'eq' | 'ne'

export interface Device {
  id: string
  deviceNo: string
  name: string
  protocol: DeviceProtocol
  endpoint: string
  /** 协议配置 JSON 文本（前端按协议渲染表单；服务端校验合法性） */
  config: string
  status: DeviceStatus
  enabled: boolean
  lastSeenAt?: string | null
  /** 安装位置：坐标统一存 WGS-84 原值 */
  lng?: number | null
  lat?: number | null
  location?: string
  /** 最后一次采集错误：采集恢复时由服务端清空，这里只表示「当前仍未恢复」 */
  lastError?: string
  lastErrorAt?: string | null
}

export interface Point {
  id: string
  deviceId: string
  name: string
  address: string
  unit: string
  rw: PointRW
  dataType: PointDataType
  scale: number
  pollIntervalMs: number
  deadband: number
  enabled: boolean
  sort: number
  lastValueNum?: number | null
  lastValueStr?: string
  lastTs?: string | null
}

export interface DeviceInput {
  name: string
  protocol: DeviceProtocol
  endpoint: string
  config: string
  enabled: boolean
  /** 安装位置（LocationField 选点或手填）；不传 = 清空 */
  location?: { lng: number; lat: number; address?: string } | null
}

export interface PointInput {
  name: string
  address: string
  unit?: string
  rw: PointRW
  dataType: PointDataType
  scale?: number
  pollIntervalMs?: number
  deadband?: number
  enabled: boolean
  sort?: number
}

export interface CommandRow {
  id: string
  deviceId: string
  pointId: string
  rawValue: string
  status: 'queued' | 'done' | 'failed' | 'timeout'
  result: string
  operatorName: string
  createdAt: string
  doneAt?: string | null
}

export interface AlarmRule {
  id: string
  pointId: string
  name: string
  op: AlarmOp
  threshold: number
  level: AlarmLevel
  enabled: boolean
  cooldownSeconds: number
  lastFiredAt?: string | null
}

export type AlarmStatus = 'active' | 'ack' | 'cleared'

export interface AlarmEvent {
  id: string
  ruleId: string
  deviceId: string
  pointId: string
  value: number
  level: AlarmLevel
  message: string
  /** 生命周期：active 未处理 / ack 已认领 / cleared 已恢复 */
  status: AlarmStatus
  ackBy?: string
  ackByName?: string
  ackAt?: string | null
  clearedAt?: string | null
  /** 静默截止时间；非空且未过期时，同一规则后续告警不再发站内信 */
  silencedUntil?: string | null
  createdAt: string
}

export interface HistoryRow {
  ts: string
  min: number
  max: number
  avg: number
}

// ---- WS 实时帧（后端 service.go 的 wsFrame）----
export interface PointTick {
  pointId: string
  address: string
  name: string
  unit: string
  num?: number
  str?: string
  ts: string
}

export interface DeviceWsFrame {
  type: 'tick' | 'status' | 'alarm' | 'command'
  deviceNo: string
  deviceId: string
  status?: string
  points?: PointTick[]
}

export const DEVICE_WS_FEED = 'device:feed'
export const deviceWsTopic = (deviceNo: string) => `device:${deviceNo}`

/** 可以按帧局部失效的查询域（queryKey 的第二段）。 */
export type DeviceQueryPart = 'devices' | 'device' | 'stats' | 'points' | 'commands' | 'alarms' | 'rules' | 'history'

/**
 * 实时帧只刷受影响的那几块，不要把 `plugin_devicehub` 整域打红：
 * 之前每一帧都整域失效，历史趋势这种聚合查询（30 天降采样）被每来一个测点就重跑一遍，
 * 而它的窗口右端是固定的，重跑也拿不到新数据——纯白打库。
 * 现在：status → 列表/统计/详情/告警；tick → 测点实时值；alarm → 告警流水与规则。
 * 历史趋势由 HistoryPanel 自己按分钟滚动窗口，不跟帧。
 */
export function invalidateDeviceQueries(qc: QueryClient, parts: DeviceQueryPart[]) {
  for (const p of parts) void qc.invalidateQueries({ queryKey: [...KEY, p] })
}

function useDeviceTopic(topic: string | undefined, onFrame: (f: DeviceWsFrame) => void) {
  const { subscribe } = useWs()
  const handler = useRef(onFrame)
  handler.current = onFrame
  useEffect(() => {
    if (!topic) return
    return subscribe(topic, (data) => {
      const f = data as DeviceWsFrame
      if (f?.type) handler.current(f)
    })
  }, [topic, subscribe])
}

/** 选中设备的实时帧：监控页订阅，命中即刷快照（轮询保留作兜底）。 */
export function useDeviceStream(deviceNo: string | undefined, onFrame: (f: DeviceWsFrame) => void) {
  useDeviceTopic(deviceNo ? deviceWsTopic(deviceNo) : undefined, onFrame)
}

/** 全列表帧：设备在线状态变化（service.go 只在 status 真的变了时发）。 */
export function useDeviceFeed(onFrame: (f: DeviceWsFrame) => void) {
  useDeviceTopic(DEVICE_WS_FEED, onFrame)
}

// ---- hooks ----

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useDevices(query: { kw?: string; page: number; pageSize: number }) {
  return useQuery({
    queryKey: [...KEY, 'devices', query],
    queryFn: () => api.get<Page<Device>>(`${BASE}/devices`, query),
    placeholderData: keepPreviousData,
    refetchInterval: useHiddenPause(8000), // WS 兜底轮询
  })
}

/** 单台设备：列表分页后详情面板不能再从当前页里捞，得有自己的入口。 */
export function useDevice(deviceNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'device', deviceNo],
    queryFn: () => api.get<Device>(`${BASE}/devices/${deviceNo}`),
    enabled: !!deviceNo,
  })
}

/** 监控页顶部的总览计数。 */
export function useDeviceStats() {
  return useQuery({
    queryKey: [...KEY, 'stats'],
    queryFn: () => api.get<DeviceStats>(`${BASE}/stats`),
    refetchInterval: useHiddenPause(8000),
  })
}

export interface DeviceStats {
  total: number
  online: number
  offline: number
  disabled: number
}

export function useSaveDevice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ deviceNo, ...input }: DeviceInput & { deviceNo?: string }) =>
      deviceNo ? api.put<Device>(`${BASE}/devices/${deviceNo}`, input) : api.post<Device>(`${BASE}/devices`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteDevice() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (deviceNo: string) => api.del<null>(`${BASE}/devices/${deviceNo}`),
    onSuccess: invalidate,
  })
}

export function usePoints(deviceNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'points', deviceNo],
    queryFn: () => api.get<Point[]>(`${BASE}/devices/${deviceNo}/points`),
    enabled: !!deviceNo,
    refetchInterval: useHiddenPause(8000),
  })
}

export function useSavePoint(deviceNo: string) {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ pointId, ...input }: PointInput & { pointId?: string }) =>
      pointId
        ? api.put<Point>(`${BASE}/devices/${deviceNo}/points/${pointId}`, input)
        : api.post<Point>(`${BASE}/devices/${deviceNo}/points`, input),
    onSuccess: invalidate,
  })
}

export function useDeletePoint(deviceNo: string) {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (pointId: string) => api.del<null>(`${BASE}/devices/${deviceNo}/points/${pointId}`),
    onSuccess: invalidate,
  })
}

export function useHistory(deviceNo: string | undefined, query: { pointId: string; from?: string; to?: string; bucket?: string }) {
  return useQuery({
    queryKey: [...KEY, 'history', deviceNo, query],
    queryFn: () => api.get<{ rows: HistoryRow[]; bucket: string }>(`${BASE}/devices/${deviceNo}/history`, query),
    enabled: !!deviceNo && !!query.pointId,
    placeholderData: keepPreviousData,
  })
}

/** 历史数据导出：真后端回 xlsx Blob，mock 阶段回 CSV 文本——扩展名按来货定，
 * 把 CSV 文本存成 .xlsx 只会得到一个打不开的假文件。 */
export async function exportHistory(deviceNo: string, query: { pointId: string; from: string; to: string; bucket?: string }): Promise<void> {
  const { http } = await import('@/framework/request/http')
  // 不带 silent：导出失败（行数超限、设备不存在）由拦截器解码 Blob 里的 JSON 错误体并弹提示，
  // 静默的话点完按钮什么都没发生，现场只能理解成「导出坏了」。
  const res = await http.get<unknown>(`${BASE}/devices/${deviceNo}/history-export`, {
    params: { ...query },
    responseType: 'blob',
  })
  const data = res.data
  const blob = data instanceof Blob ? data : new Blob(['\uFEFF', String(data)], { type: 'text/csv;charset=utf-8' })
  const ext = blob.type.includes('csv') ? 'csv' : 'xlsx'
  const stamp = new Date().toISOString().slice(0, 19).replace(/-|:|T/g, '')
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `history-${deviceNo}-${stamp}.${ext}`
  a.click()
  URL.revokeObjectURL(url)
}

export function useCommands(deviceNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'commands', deviceNo],
    queryFn: () => api.get<CommandRow[]>(`${BASE}/devices/${deviceNo}/commands`, { limit: 50 }),
    enabled: !!deviceNo,
    refetchInterval: useHiddenPause(5000),
  })
}

/** 采集/指令日志：服务端是每设备 200 条的内存环形缓冲，新的在前。 */
export interface DeviceLog {
  ts: string
  level: 'info' | 'warn' | 'error'
  /** connect 建链 / poll 轮询摘要 / read 读点 / write 指令 / status 在线状态 */
  kind: 'connect' | 'poll' | 'read' | 'write' | 'status'
  target?: string
  message: string
  costMs?: number
  /** 同一条日志在合并窗口内重复的次数（1 表示没重复） */
  repeat: number
}

export function useDeviceLogs(deviceNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'logs', deviceNo],
    queryFn: () => api.get<{ list: DeviceLog[]; capacity: number }>(`${BASE}/devices/${deviceNo}/logs`, { limit: 200 }),
    enabled: !!deviceNo,
    // 日志按秒级变化，但没必要跟 WS 抢实时性：4 秒一轮已经能看着故障发生
    refetchInterval: useHiddenPause(4000),
  })
}

export function useCreateCommand(deviceNo: string) {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (input: { pointId: string; value: number; rawValue?: string }) =>
      api.post<CommandRow>(`${BASE}/devices/${deviceNo}/commands`, input),
    onSuccess: invalidate,
  })
}

export function useAlarmEvents(
  deviceNo?: string,
  query: { level?: string; status?: string; page?: number; pageSize?: number } = {},
) {
  return useQuery({
    queryKey: [...KEY, 'alarms', deviceNo, query],
    queryFn: () =>
      api.get<Page<AlarmEvent>>(`${BASE}/alarms`, deviceNo ? { deviceNo, ...query } : query),
    placeholderData: keepPreviousData,
    refetchInterval: useHiddenPause(8000),
  })
}

/** 认领告警：active → ack，列表上显示谁在处理。 */
export function useAckAlarmEvent() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<AlarmEvent>(`${BASE}/alarms/${id}/ack`),
    onSuccess: invalidate,
  })
}

/** 人工恢复告警：active/ack → cleared。 */
export function useClearAlarmEvent() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (id: string) => api.post<AlarmEvent>(`${BASE}/alarms/${id}/clear`),
    onSuccess: invalidate,
  })
}

/** 静默告警；minutes=0 表示取消静默。 */
export function useSilenceAlarmEvent() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ id, minutes }: { id: string; minutes: number }) =>
      api.post<AlarmEvent>(`${BASE}/alarms/${id}/silence`, { minutes }),
    onSuccess: invalidate,
  })
}

/** 设备的全部告警规则一次拉回（按测点分组在页面做），替代原来的每测点一请求。 */
export function useDeviceRules(deviceNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'rules', deviceNo],
    queryFn: () => api.get<AlarmRule[]>(`${BASE}/devices/${deviceNo}/rules`),
    enabled: !!deviceNo,
  })
}

export function useSaveAlarmRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: ({ ruleId, ...input }: AlarmRuleInput & { ruleId?: string }) =>
      ruleId
        ? api.put<AlarmRule>(`${BASE}/rules/${ruleId}`, input)
        : api.post<AlarmRule>(`${BASE}/points/${input.pointId}/rules`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteAlarmRule() {
  const invalidate = useInvalidate()
  return useMutation({
    mutationFn: (ruleId: string) => api.del<null>(`${BASE}/rules/${ruleId}`),
    onSuccess: invalidate,
  })
}

export interface AlarmRuleInput {
  pointId: string
  name: string
  op: AlarmOp
  threshold: number
  level: AlarmLevel
  enabled: boolean
  cooldownSeconds: number
}
