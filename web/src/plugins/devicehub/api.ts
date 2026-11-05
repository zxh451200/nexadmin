// 设备接入与监控的 API 层：类型 + react-query hooks + WS 实时订阅。
import { useEffect, useRef } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import { useWs } from '@/framework/hooks/useWs'

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

export interface AlarmEvent {
  id: string
  ruleId: string
  deviceId: string
  pointId: string
  value: number
  level: AlarmLevel
  message: string
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

/** 全列表帧：任何设备状态/测点变化都发一帧。 */
export function useDeviceFeed(onFrame: (f: DeviceWsFrame) => void) {
  useDeviceTopic(DEVICE_WS_FEED, onFrame)
}

// ---- hooks ----

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: KEY })
}

export function useDevices() {
  return useQuery({
    queryKey: [...KEY, 'devices'],
    queryFn: () => api.get<Device[]>(`${BASE}/devices`),
    refetchInterval: 8000, // WS 兜底轮询
  })
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
    refetchInterval: 8000,
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
  const stamp = new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
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
    refetchInterval: 5000,
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

export function useAlarmEvents(deviceNo?: string) {
  return useQuery({
    queryKey: [...KEY, 'alarms', deviceNo],
    queryFn: () => api.get<AlarmEvent[]>(`${BASE}/alarms`, deviceNo ? { deviceNo, limit: 50 } : { limit: 50 }),
    refetchInterval: 8000,
  })
}

export function useAlarmRules(pointId?: string) {
  return useQuery({
    queryKey: [...KEY, 'rules', pointId],
    queryFn: () => api.get<AlarmRule[]>(`${BASE}/points/${pointId}/rules`),
    enabled: !!pointId,
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
