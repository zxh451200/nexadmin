import { buildHistory, historyHours } from '@/framework/request/history-mock'
import { defineMock } from '@/framework/request/mock'
import type { ApiSnapshot, ApiStat } from './api'

const r2 = (v: number) => Math.round(v * 100) / 100

const walk = (v: number, step: number, min: number, max: number) => Math.min(max, Math.max(min, v + (Math.random() - 0.5) * step))

type Seed = [ApiStat['method'], string, number, number, number]

// [方法, 路径, 基准 QPS, 基准耗时 ms, 基准错误率 %]
const seeds: Seed[] = [
  ['POST', '/auth/login', 42, 180, 3.2],
  ['GET', '/auth/me', 120, 12, 0.1],
  ['GET', '/menus/mine', 96, 18, 0.1],
  ['GET', '/users', 64, 46, 0.4],
  ['GET', '/notices/mine', 58, 31, 0.2],
  ['GET', '/online-users', 22, 55, 0.3],
  ['POST', '/files/upload', 15, 620, 2.4],
  ['GET', '/oper-logs', 18, 88, 0.6],
  ['PUT', '/settings/:section', 3, 95, 1.1],
  ['GET', '/dicts/:code', 140, 9, 0.05],
  ['GET', '/monitor/server', 12, 40, 0.2],
  ['DELETE', '/users/:id', 4, 72, 1.6],
]

const state = seeds.map(([method, path, qps, avgMs, errorRate]) => ({ method, path, qps, avgMs, errorRate }))

defineMock([
  {
    method: 'GET',
    url: '/monitor/api',
    handler: (): ApiSnapshot => {
      state.forEach((s, i) => {
        const [, , q, ms, er] = seeds[i]
        s.qps = Math.round(walk(s.qps, q * 0.3, q * 0.4, q * 1.8))
        s.avgMs = Math.round(walk(s.avgMs, ms * 0.2, ms * 0.6, ms * 1.6))
        s.errorRate = Number(walk(s.errorRate, er * 0.4, er * 0.3, er * 2.2).toFixed(2))
      })
      const qps = state.reduce((a, s) => a + s.qps, 0)
      const avgMs = Math.round(state.reduce((a, s) => a + s.avgMs * s.qps, 0) / qps)
      const errorRate = Number((state.reduce((a, s) => a + s.errorRate * s.qps, 0) / qps).toFixed(2))
      return { ts: Date.now(), qps, avgMs, errorRate, apis: state.map((s) => ({ ...s })) }
    },
  },
  {
    method: 'GET',
    url: '/monitor/api/history',
    handler: (ctx) =>
      buildHistory(historyHours(ctx.query), (i) => {
        const wave = Math.sin(i / 6)
        return {
          qps: Math.round(520 + wave * 140),
          avgMs: r2(46 + wave * 12),
          errorRate: r2(Math.max(0.05, 1.4 + wave * 0.8)),
        }
      }),
  },
])
