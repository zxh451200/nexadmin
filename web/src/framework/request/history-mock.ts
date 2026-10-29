/**
 * 监控历史 mock 的公共部分：桶宽算法与后端 modules/internal/monitorsnap.Downsample 保持一致
 * （桶宽 = 窗口/300 向下取到采样周期的整数倍，再加一个采样周期；下限是一个采样周期）。
 * 两份实现必须同口径，否则开发态看到的曲线密度和生产不一样。
 */
const SAMPLE_MS = 30_000
const MAX_POINTS = 300

export interface HistoryPoint {
  ts: number
  metrics: Record<string, number>
}

export interface HistoryView {
  from: string
  to: string
  stepSeconds: number
  points: HistoryPoint[]
}

export function historyStepMs(hours: number): number {
  const raw = (hours * 3_600_000) / MAX_POINTS
  const aligned = Math.floor(raw / SAMPLE_MS) * SAMPLE_MS
  return Math.max(SAMPLE_MS, aligned + SAMPLE_MS)
}

export function buildHistory(hours: number, gen: (index: number, at: number) => Record<string, number>): HistoryView {
  const step = historyStepMs(hours)
  const to = Date.now()
  const from = to - hours * 3_600_000
  const points: HistoryPoint[] = []
  for (let ts = Math.floor(from / step) * step; ts < to; ts += step) {
    points.push({ ts, metrics: gen(points.length, ts) })
  }
  return { from: new Date(from).toISOString(), to: new Date(to).toISOString(), stepSeconds: Math.round(step / 1000), points }
}

/** 解析 ?hours=，与后端一样默认 6 小时并夹到 1..168。 */
export function historyHours(query: Record<string, unknown>): number {
  const n = Number(query.hours ?? 6)
  if (!Number.isFinite(n) || n < 1) return 6
  return Math.min(168, Math.floor(n))
}
