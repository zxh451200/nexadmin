import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface ChartBoxProps {
  height: number
  className?: string
  children: (size: { width: number; height: number }) => ReactNode
}

/**
 * 图表容器：自己量宽度，宽度大于 0 才渲染图表，并把尺寸直接传给 recharts。
 * 比 ResponsiveContainer 稳：标签页缓存隐藏（display:none）、折叠面板里宽度为 0 时不渲染，
 * 也就不会刷 "width(0) and height(0) of chart" 警告，还省掉隐藏页面上的无效重绘。
 */
export function ChartBox({ height, className, children }: ChartBoxProps) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const update = () => setWidth(el.clientWidth)
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  return (
    <div ref={ref} className={cn('w-full', className)} style={{ height }}>
      {width > 0 ? children({ width, height }) : null}
    </div>
  )
}
