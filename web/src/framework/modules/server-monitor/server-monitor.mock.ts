import { defineMock } from '@/framework/request/mock'
import type { ClientErrorRow, HealthCheck, HealthSnapshot, ServerSnapshot } from './api'

const MIN = 60000
const errId = (n: number) => (1850000000000000000n + BigInt(n)).toString()
const UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36'
const STACK = (fn: string) =>
  `TypeError: Cannot read properties of undefined (reading 'map')\n    at ${fn} (http://localhost/assets/index-AbCd1234.js:10:5)\n    at renderWithHooks (http://localhost/assets/vendor-Zz99Yy88.js:1:2)\n    at updateFunctionComponent (http://localhost/assets/vendor-Zz99Yy88.js:3:4)`

// [消息, 路由, 用户, 次数, 多少分钟前最后一次出现, 堆栈函数名]
const errorRows: [string, string, string, number, number, string][] = [
  ["Cannot read properties of undefined (reading 'map')", '/system/user', 'zhang.wei', 37, 4, 'UserTable'],
  ['ChunkLoadError: Loading chunk 128 failed', '/monitor/server', '', 12, 18, 'loadChunk'],
  ['Network request failed: timeout of 20000ms exceeded', '/dashboard', 'li.na', 9, 55, 'fetchStats'],
  ["Unexpected token '<', \"<!doctype \"... is not valid JSON", '/system/config', 'admin', 3, 240, 'parseResponse'],
  ['ResizeObserver is not defined', '/oper-log', 'wang.fang', 1, 1300, 'useSize'],
]

const clientErrors: ClientErrorRow[] = errorRows.map(([message, route, username, hitCount, ago, fn], i) => ({
  id: errId(i + 1),
  userId: username ? errId(100 + i) : '0',
  username,
  route,
  message,
  stack: STACK(fn),
  ua: UA,
  version: '0.1.0',
  fingerprint: `mockfp${i}`,
  hitCount,
  lastSeenAt: new Date(Date.now() - ago * MIN).toISOString(),
  createdAt: new Date(Date.now() - (ago + 600) * MIN).toISOString(),
}))

const jitter = (base: number, spread: number) => Math.max(1, Math.round(base + (Math.random() - 0.5) * spread))

// 模拟服务器指标：在合理范围内随机游走，看起来像真实曲线
const walk = (v: number, step: number, min: number, max: number) => Math.min(max, Math.max(min, v + (Math.random() - 0.5) * step))
const startedAt = Date.now() - (12 * 24 * 3600 + 4 * 3600) * 1000

let cpu = 34
let memUsed = 11.6
let netIn = 820
let netOut = 460
let goroutines = 184
let heap = 96
let gcCount = 1240
let load = 1.4

defineMock([
  {
    method: 'GET',
    url: '/monitor/server',
    handler: (): ServerSnapshot => {
      cpu = walk(cpu, 16, 8, 92)
      memUsed = walk(memUsed, 0.5, 9, 24)
      netIn = walk(netIn, 380, 120, 2600)
      netOut = walk(netOut, 260, 80, 1800)
      goroutines = Math.round(walk(goroutines, 30, 120, 420))
      heap = walk(heap, 14, 60, 220)
      if (Math.random() > 0.55) gcCount += 1
      load = walk(load, 0.5, 0.3, 5.5)
      return {
        ts: Date.now(),
        cpu: { usage: cpu, cores: 8, load1: load, load5: load * 0.9, load15: load * 0.8 },
        memory: { totalGb: 32, usedGb: memUsed },
        disks: [
          { mount: '/', totalGb: 200, usedGb: 86.4 },
          { mount: '/data', totalGb: 1000, usedGb: 412.7 },
          { mount: '/var/log', totalGb: 100, usedGb: 57.9 },
        ],
        network: { inKbps: netIn, outKbps: netOut },
        runtime: { goroutines, heapMb: heap, gcCount, gcPauseMs: walk(1.2, 1.6, 0.2, 6) },
        info: {
          os: 'Linux 5.10 (Kylin V10)',
          arch: 'amd64',
          goVersion: 'go1.24',
          hostname: 'nexadmin-01',
          ip: '10.24.23.166',
          startedAt: new Date(startedAt).toISOString(),
          uptimeSec: Math.floor((Date.now() - startedAt) / 1000),
        },
      }
    },
  },
  {
    // 健康检查：大部分正常，插件授权即将到期给出一个 warn，方便看语义色
    method: 'GET',
    url: '/monitor/health',
    handler: (): HealthSnapshot => {
      const ok = (ms: number, message: string): HealthCheck => ({ status: 'ok', latencyMs: jitter(ms, ms), message })
      return {
        status: 'warn',
        ts: Date.now(),
        checks: {
          db: ok(4, 'ok'),
          cache: ok(2, 'ok'),
          storage: ok(9, 'ok'),
          migrations: { status: 'ok', latencyMs: 0, message: 'up to date' },
          plugins: { status: 'warn', latencyMs: 0, message: 'license expires soon: crm' },
          disk: { status: 'ok', latencyMs: 1, message: '/data 41% used' },
        },
      }
    },
  },
  {
    method: 'GET',
    url: '/monitor/client-errors',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let list = clientErrors.slice()
      if (kw) list = list.filter((r) => [r.message, r.route, r.username].some((v) => v.toLowerCase().includes(kw)))
      list.sort((a, b) => b.lastSeenAt.localeCompare(a.lastSeenAt))
      return { list: list.slice((page - 1) * pageSize, page * pageSize), total: list.length, page, pageSize }
    },
  },
])
