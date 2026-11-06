/** 一次拖动里的真实采样：相对 pointerdown 的毫秒与指针横坐标 */
export interface DragSample {
  t: number
  cx: number
}

/** 后端 sliderTrailMin 的同款下限：采样比这还稀就退回库的轨迹补形状 */
export const TRAIL_MIN_SAMPLES = 6

/**
 * 把「真实时序」和「库给的拼图行程」接成后端要的轨迹。
 * 时间戳取采样原值——人的微停顿、先慢后快就记在这里；横坐标按落点线性归一到行程口径
 * （库给的 trail 是绝对 clientX，后端比的是拼图行程 x、容差与推进量同一口径），末点强制等于落点。
 * 采样过稀（触屏快滑、掉帧）时退回库的轨迹补形状，时间仍在真实首末之间铺开，不硬造时长。
 */
export function buildTrail(samples: DragSample[], libTrail: [number, number][], landingX: number, durationMs: number) {
  const pts =
    samples.length >= TRAIL_MIN_SAMPLES
      ? samples
      : libTrail.map((p, i) => ({ t: (i / Math.max(1, libTrail.length - 1)) * durationMs, cx: p[0] }))
  if (pts.length < 2) pts.push({ t: Math.max(1, durationMs), cx: pts[0]?.cx ?? 0 })
  const x0 = pts[0].cx
  const span = pts[pts.length - 1].cx - x0
  const scale = span === 0 ? 0 : landingX / span
  const trail = pts.map((p) => ({ t: Math.round(p.t), x: Math.round((p.cx - x0) * scale) }))
  const last = trail[trail.length - 1]
  trail[trail.length - 1] = { t: Math.max(last.t, 1, Math.round(durationMs)), x: Math.round(landingX) }
  return trail
}
