import { startTransition, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface ChartBoxProps {
  height: number
  className?: string
  children: (size: { width: number; height: number }) => ReactNode
}

/** 连续缩放停稳多久再重绘（侧栏折叠动画约 200ms，期间每次 ResizeObserver 回调都会把计时重新推后） */
const RESIZE_SETTLE_MS = 100

/**
 * 图表容器：自己量宽度，宽度大于 0 才渲染图表，并把尺寸直接传给 recharts。
 * 比 ResponsiveContainer 稳：标签页缓存隐藏（display:none）、折叠面板里宽度为 0 时不渲染，
 * 也就不会刷 "width(0) and height(0) of chart" 警告，还省掉隐藏页面上的无效重绘。
 *
 * 尺寸变化分两种对待（对齐 ResponsiveContainer 的 debounce 口径）：
 * - 显隐切换（0 ↔ 非 0）立刻生效：切回缓存标签、展开折叠面板时图表不能空一帧；
 * - 连续缩放（侧栏折叠动画、窗口拖拽）等停稳再重绘：否则一屏 13 个 recharts 会在过渡的
 *   每一帧重算路径，把 200ms 的动画拖成几百毫秒的卡顿。过渡期间的图表由容器裁掉。
 */
export function ChartBox({ height, className, children }: ChartBoxProps) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    let timer: number | undefined
    let shown = -1
    const apply = (w: number) => {
      shown = w
      setWidth(w)
    }
    apply(el.clientWidth)
    const ro = new ResizeObserver(() => {
      const w = el.clientWidth
      if (w === shown) return
      window.clearTimeout(timer)
      if (w === 0 || shown === 0) {
        apply(w)
        return
      }
      timer = window.setTimeout(() => {
        // 重绘不是紧急更新：走 transition 让 React 在 13 张图之间分帧让出主线程，
        // 否则一次同步重算 13 个 recharts 会卡住一二百毫秒。
        shown = w
        startTransition(() => setWidth(w))
      }, RESIZE_SETTLE_MS)
    })
    ro.observe(el)
    return () => {
      ro.disconnect()
      window.clearTimeout(timer)
    }
  }, [])

  return (
    <div ref={ref} className={cn('w-full overflow-hidden', className)} style={{ height }}>
      {width > 0 ? children({ width, height }) : null}
    </div>
  )
}
