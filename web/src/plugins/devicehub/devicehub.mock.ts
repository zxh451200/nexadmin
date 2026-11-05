// 设备监控的 mock：开发模式（VITE_MOCK）下给页面喂数据，形状与真后端一致。
import { defineMock } from '@/framework/request/mock'

interface MockPoint {
  id: string
  deviceId: string
  name: string
  address: string
  unit: string
  rw: 'r' | 'rw'
  dataType: string
  scale: number
  pollIntervalMs: number
  deadband: number
  enabled: boolean
  sort: number
  lastValueNum?: number | null
  lastValueStr?: string
  lastTs?: string | null
}

const devices = [
  {
    id: '901', deviceNo: 'DEV901', name: '1# 变频器', protocol: 'modbus-tcp',
    endpoint: '192.168.1.20:502', config: '{"unitId":1,"pollIntervalMs":5000}',
    status: 'online', enabled: true, lastSeenAt: new Date().toISOString(),
  },
  {
    id: '902', deviceNo: 'DEV902', name: '边缘网关 A', protocol: 'mqtt',
    endpoint: 'tcp://192.168.1.10:1883', config: '{"upstreamPrefix":"devicehub/up/DEV902"}',
    status: 'offline', enabled: true, lastSeenAt: null,
  },
]

const points: MockPoint[] = [
  { id: '1', deviceId: '901', name: '输出频率', address: 'hr:40001', unit: 'Hz', rw: 'rw', dataType: 'float32', scale: 10, pollIntervalMs: 0, deadband: 0.1, enabled: true, sort: 0, lastValueNum: 42.7, lastTs: new Date().toISOString() },
  { id: '2', deviceId: '901', name: '母线电压', address: 'hr:40003', unit: 'V', rw: 'r', dataType: 'uint16', scale: 1, pollIntervalMs: 0, deadband: 0, enabled: true, sort: 1, lastValueNum: 537, lastTs: new Date().toISOString() },
  { id: '3', deviceId: '901', name: '运行状态', address: 'co:1', unit: '', rw: 'rw', dataType: 'bool', scale: 1, pollIntervalMs: 0, deadband: 0, enabled: true, sort: 2, lastValueNum: 1, lastTs: new Date().toISOString() },
]

const deviceOf = (no: string) => devices.find((d) => d.deviceNo === no)

defineMock([
  { method: 'GET', url: '/plugins/devicehub/devices', handler: () => devices },
  {
    method: 'POST', url: '/plugins/devicehub/devices',
    handler: ({ body }) => {
      const d = {
        id: String(900 + devices.length + 1), deviceNo: `DEV${900 + devices.length + 1}`,
        name: String(body.name ?? ''), protocol: body.protocol ?? 'modbus-tcp',
        endpoint: String(body.endpoint ?? ''), config: String(body.config ?? ''),
        status: 'offline', enabled: body.enabled !== false, lastSeenAt: null,
      }
      devices.push(d)
      return d
    },
  },
  {
    method: 'PUT', url: '/plugins/devicehub/devices/:no',
    handler: ({ params, body }) => {
      const d = deviceOf(params.no)
      if (!d) throw new Error('device not found')
      Object.assign(d, body)
      return d
    },
  },
  {
    method: 'DELETE', url: '/plugins/devicehub/devices/:no',
    handler: ({ params }) => {
      const i = devices.findIndex((d) => d.deviceNo === params.no)
      if (i >= 0) devices.splice(i, 1)
      return null
    },
  },
  {
    method: 'GET', url: '/plugins/devicehub/devices/:no/points',
    handler: ({ params }) => points.filter((p) => p.deviceId === deviceOf(params.no)?.id),
  },
  {
    method: 'POST', url: '/plugins/devicehub/devices/:no/points',
    handler: ({ params, body }) => {
      const d = deviceOf(params.no)
      const row: MockPoint = {
        id: String(points.length + 1), deviceId: d?.id ?? '0', name: body.name, address: body.address,
        unit: body.unit ?? '', rw: body.rw ?? 'r', dataType: body.dataType ?? 'float32',
        scale: Number(body.scale ?? 1), pollIntervalMs: Number(body.pollIntervalMs ?? 0),
        deadband: Number(body.deadband ?? 0), enabled: body.enabled !== false, sort: Number(body.sort ?? 0),
        lastValueNum: null, lastTs: null,
      }
      points.push(row)
      return row
    },
  },
  {
    method: 'PUT', url: '/plugins/devicehub/devices/:no/points/:pid',
    handler: ({ params, body }) => {
      const row = points.find((p) => p.id === params.pid)
      if (!row) throw new Error('point not found')
      Object.assign(row, body)
      return row
    },
  },
  {
    method: 'DELETE', url: '/plugins/devicehub/devices/:no/points/:pid',
    handler: ({ params }) => {
      const i = points.findIndex((p) => p.id === params.pid)
      if (i >= 0) points.splice(i, 1)
      return null
    },
  },
  {
    method: 'GET', url: '/plugins/devicehub/devices/:no/history',
    handler: () => {
      // 用正弦波造 24h 小时级趋势，让趋势图在 mock 下有形状
      const rows = []
      const now = Date.now()
      for (let i = 23; i >= 0; i--) {
        const ts = new Date(now - i * 3600_000).toISOString()
        const v = 42 + 8 * Math.sin(i / 3)
        rows.push({ ts, min: v - 1.2, max: v + 1.2, avg: v })
      }
      return { rows, bucket: 'hour' }
    },
  },
  {
    // 历史导出：真后端回 xlsx 二进制，mock 回同口径的 CSV 文本（前端按 Content 类型定扩展名）
    method: 'GET', url: '/plugins/devicehub/devices/:no/history-export',
    handler: ({ params }) => {
      const rows: string[] = ['device_no,point,unit,ts,min,max,avg']
      const now = Date.now()
      for (let i = 23; i >= 0; i--) {
        const v = 42 + 8 * Math.sin(i / 3)
        rows.push(`${params.no},母线电压,V,${new Date(now - i * 3600_000).toISOString()},${(v - 1.2).toFixed(2)},${(v + 1.2).toFixed(2)},${v.toFixed(2)}`)
      }
      return rows.join('\n')
    },
  },
  {
    method: 'GET', url: '/plugins/devicehub/devices/:no/commands',
    handler: () => [
      {
        id: '1', deviceId: '901', pointId: '1', rawValue: '350', status: 'done',
        result: '', operatorName: 'admin', createdAt: new Date(Date.now() - 3600_000).toISOString(),
        doneAt: new Date(Date.now() - 3600_000 + 200).toISOString(),
      },
    ],
  },
  {
    method: 'POST', url: '/plugins/devicehub/devices/:no/commands',
    handler: ({ body }) => ({
      id: String(Date.now()), deviceId: '901', pointId: String(body.pointId), rawValue: String(body.value),
      status: 'queued', result: '', operatorName: 'admin', createdAt: new Date().toISOString(), doneAt: null,
    }),
  },
  { method: 'GET', url: '/plugins/devicehub/alarms', handler: () => [
    { id: '1', ruleId: '1', deviceId: '901', pointId: '1', value: 52.3, level: 'warn', message: '1# 变频器 / 输出频率 = 52.3 Hz（> 50）', createdAt: new Date(Date.now() - 7200_000).toISOString() },
  ] },
  {
    method: 'GET', url: '/plugins/devicehub/points/:pid/rules',
    handler: () => [
      { id: '1', pointId: '1', name: '超频告警', op: 'gt', threshold: 50, level: 'warn', enabled: true, cooldownSeconds: 300, lastFiredAt: new Date(Date.now() - 7200_000).toISOString() },
    ],
  },
  {
    method: 'POST', url: '/plugins/devicehub/points/:pid/rules',
    handler: ({ params, body }) => ({
      id: String(Date.now()), pointId: params.pid, name: body.name, op: body.op,
      threshold: Number(body.threshold), level: body.level ?? 'warn', enabled: true,
      cooldownSeconds: Number(body.cooldownSeconds ?? 300), lastFiredAt: null,
    }),
  },
  { method: 'PUT', url: '/plugins/devicehub/rules/:id', handler: ({ params, body }) => ({ id: params.id, ...body }) },
  { method: 'DELETE', url: '/plugins/devicehub/rules/:id', handler: () => null },
])
