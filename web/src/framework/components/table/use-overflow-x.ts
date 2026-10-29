import { useEffect, useRef, useState } from 'react'

/**
 * 横向滚动容器是否真的溢出。
 * 只有溢出时才把「操作」列吸附到右侧；表格本来就放得下时吸附会留出一块游离的白块。
 */
export function useOverflowX<T extends HTMLElement>() {
  const ref = useRef<T | null>(null)
  const [overflowing, setOverflowing] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    // Table 原语自带一层 overflow-auto 滚动壳，真正的溢出发生在它身上，不是我们这层
    const scroller = (el.firstElementChild as HTMLElement | null) ?? el
    const update = () => setOverflowing(scroller.scrollWidth - scroller.clientWidth > 1)
    update()
    const observer = new ResizeObserver(update)
    observer.observe(scroller)
    if (scroller.firstElementChild) observer.observe(scroller.firstElementChild)
    return () => observer.disconnect()
  }, [])

  return { ref, overflowing }
}
