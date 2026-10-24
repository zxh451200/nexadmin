export interface ApiStat {
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  qps: number
  /** 平均耗时（毫秒） */
  avgMs: number
  /** 错误率（百分比 0-100） */
  errorRate: number
}

export interface ApiSnapshot {
  ts: number
  qps: number
  avgMs: number
  errorRate: number
  apis: ApiStat[]
}

export const TOP_OPTIONS = ['5', '10'] as const
export const SORT_KEYS = ['qps', 'avgMs', 'errorRate'] as const
export type SortKey = (typeof SORT_KEYS)[number]
