import { useLayoutEffect, useRef, useState } from 'react'
import { useIsMobile } from '@/framework/hooks/use-mobile'

/**
 * 表格该不该用卡片布局：手机，或者表格所在的内容区（扣掉侧边栏之后）不足 breakpoint 宽。
 * 只看浏览器窗口宽度不够：窗口 900px、侧边栏 256px 时内容区只有约 600px，多列表格会被挤得换行错乱。
 */
export function useCompactLayout(breakpoint = 640) {
  const isMobile = useIsMobile()
  const ref = useRef<HTMLDivElement>(null)
  const [narrow, setNarrow] = useState(false)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const update = () => setNarrow(el.clientWidth > 0 && el.clientWidth < breakpoint)
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [breakpoint])

  // phone=真手机用完整卡片；compact 但非 phone 是桌面上的窄栏（如左右分栏的左栏），用紧凑列表行
  return { ref, compact: isMobile || narrow, phone: isMobile }
}
