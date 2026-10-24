export interface CacheSnapshot {
  ts: number
  driver: 'memory' | 'valkey'
  version: string
  uptimeSec: number
  usedMemoryMb: number
  maxMemoryMb: number
  keys: number
  hitRate: number
  qps: number
  connections: number
  commands: { name: string; count: number }[]
  prefixes: CachePrefix[]
}

export interface CachePrefix {
  prefix: string
  keys: number
  ttl: string
  desc: string
}
