import { buildHistory, historyHours } from '@/framework/request/history-mock'
import { defineMock } from '@/framework/request/mock'
import type { CacheSnapshot } from './api'

const r2 = (v: number) => Math.round(v * 100) / 100

const walk = (v: number, step: number, min: number, max: number) => Math.min(max, Math.max(min, v + (Math.random() - 0.5) * step))
const startedAt = Date.now() - (6 * 24 * 3600 + 9 * 3600) * 1000

let usedMb = 182
let keys = 12840
let hit = 96.4
let qps = 860
let conns = 24
const cmd = { get: 482_300, set: 96_400, del: 21_700, expire: 88_100, incr: 15_900, hget: 33_200 }

defineMock([
  {
    method: 'GET',
    url: '/monitor/cache',
    handler: (): CacheSnapshot => {
      usedMb = walk(usedMb, 6, 120, 420)
      keys = Math.round(walk(keys, 180, 9000, 20000))
      hit = walk(hit, 1.4, 88, 99.5)
      qps = Math.round(walk(qps, 260, 120, 2400))
      conns = Math.round(walk(conns, 4, 8, 60))
      cmd.get += Math.round(qps * 0.62 * 3)
      cmd.set += Math.round(qps * 0.12 * 3)
      cmd.del += Math.round(qps * 0.03 * 3)
      cmd.expire += Math.round(qps * 0.1 * 3)
      cmd.incr += Math.round(qps * 0.02 * 3)
      cmd.hget += Math.round(qps * 0.04 * 3)
      return {
        ts: Date.now(),
        driver: 'valkey',
        version: 'Valkey 8.0.1',
        uptimeSec: Math.floor((Date.now() - startedAt) / 1000),
        usedMemoryMb: usedMb,
        maxMemoryMb: 1024,
        keys,
        hitRate: hit,
        qps,
        connections: conns,
        commands: Object.entries(cmd).map(([name, count]) => ({ name: name.toUpperCase(), count })),
        prefixes: [
          { prefix: 'nex:session:*', keys: Math.round(keys * 0.31), ttl: '2h', desc: 'session' },
          { prefix: 'nex:captcha:*', keys: Math.round(keys * 0.06), ttl: '5m', desc: 'captcha' },
          { prefix: 'nex:online:*', keys: Math.round(keys * 0.12), ttl: '30m', desc: 'online' },
          { prefix: 'nex:dict:*', keys: Math.round(keys * 0.02), ttl: '1h', desc: 'dict' },
          { prefix: 'nex:tenant:*', keys: Math.round(keys * 0.04), ttl: '10m', desc: 'tenant' },
          { prefix: 'nex:ratelimit:*', keys: Math.round(keys * 0.45), ttl: '1m', desc: 'ratelimit' },
        ],
      }
    },
  },
  {
    method: 'GET',
    url: '/monitor/cache/history',
    handler: (ctx) =>
      buildHistory(historyHours(ctx.query), (i) => {
        const wave = Math.sin(i / 9) * 8
        return {
          hitRate: r2(Math.max(50, Math.min(99.5, 93 + wave * 0.4))),
          qps: Math.round(1_450 + wave * 90),
          keys: Math.round(18_200 + i * 6),
          usedMemoryMb: r2(128 + wave * 3),
          connections: Math.round(42 + wave),
        }
      }),
  },
])
