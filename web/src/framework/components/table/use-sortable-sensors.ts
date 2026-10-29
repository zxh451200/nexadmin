import { useEffect, useRef, useState } from 'react'
import { closestCenter, KeyboardSensor, MouseSensor, pointerWithin, TouchSensor, useDndMonitor, useSensor, useSensors, type CollisionDetection, type Modifier } from '@dnd-kit/core'
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable'

/** 鼠标移动 4px 起拖；触屏长按 200ms 起拖（不与滚动冲突）；键盘空格抬起 */
export function useSortableSensors() {
  return useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 4 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 200, tolerance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
}

export const restrictToVertical: Modifier = ({ transform }) => ({ ...transform, x: 0 })

/**
 * 命中以「指针落在哪一行」为准，而不是「被拖元素中心离谁近」：
 * closestCenter 下被拖元素只跟到指针的一半，行高 50px 要拖 100+px 才换位，手感就是「拖不动」。
 * 指针落在行间空隙时退回 closestCenter，避免松手没反应。
 */
export const pointerFirstCollision: CollisionDetection = (args) => {
  const hits = pointerWithin(args)
  return hits.length > 0 ? hits : closestCenter(args)
}

/**
 * 拖动全程保持「抓取中」光标：指针一旦离开手柄，:active 就失效，光标会跳回箭头，
 * 这是「手感不顺」最常见的来源之一，所以在 body 上统一加。
 */
export function useDragCursor(dragging: boolean) {
  useEffect(() => {
    if (!dragging) return
    const prev = document.body.style.cursor
    document.body.style.cursor = 'grabbing'
    return () => {
      document.body.style.cursor = prev
    }
  }, [dragging])
}

/** 渲染在任意 DndContext 内部一次即可：不需要自己维护 activeId 也能全程保持 grabbing 光标。 */
export function DragCursorHost() {
  const [dragging, setDragging] = useState(false)
  useDndMonitor({
    onDragStart: () => setDragging(true),
    onDragEnd: () => setDragging(false),
    onDragCancel: () => setDragging(false),
  })
  useDragCursor(dragging)
  return null
}

/**
 * 拖动时的边缘自动滚动：指针靠近视口上/下边缘就持续滚动，越靠边越快。
 * dnd-kit 自带的 autoScroll 在这种「表体不滚、页面滚」的结构下不生效（实测 1.8s 仍 scrollTop=0），
 * 所以自己用 rAF 做：先滚表格自己的横向滚动壳（若它纵向也可滚），滚不动再滚页面。
 */
export function useEdgeAutoScroll(enabled: boolean, container?: () => HTMLElement | null) {
  const pos = useRef<{ x: number; y: number } | null>(null)
  const getContainer = useRef(container)
  getContainer.current = container
  useEffect(() => {
    if (!enabled) {
      pos.current = null
      return
    }
    const onMove = (e: PointerEvent) => {
      pos.current = { x: e.clientX, y: e.clientY }
    }
    window.addEventListener('pointermove', onMove, { passive: true })
    const EDGE = 90
    const MAX = 22
    let raf = 0
    const tick = () => {
      const pt = pos.current
      if (pt) {
        const vh = window.innerHeight
        const delta =
          pt.y < EDGE
            ? -Math.ceil(((EDGE - pt.y) / EDGE) * MAX)
            : pt.y > vh - EDGE
              ? Math.ceil(((pt.y - (vh - EDGE)) / EDGE) * MAX)
              : 0
        if (delta !== 0) {
          // 后台的滚动容器是 <main class="overflow-y-auto">，不是 window：
          // 从表格（或指针下的元素）往上找第一个「纵向可滚且还能动」的祖先，都不行才滚页面。
          let node: HTMLElement | null = getContainer.current?.() ?? (document.elementFromPoint(pt.x, pt.y) as HTMLElement | null)
          let moved = false
          while (node) {
            const cs = getComputedStyle(node)
            if (/(auto|scroll)/.test(cs.overflowY) && node.scrollHeight > node.clientHeight + 1) {
              const before = node.scrollTop
              node.scrollTop = before + delta
              if (node.scrollTop !== before) {
                moved = true
                break
              }
            }
            node = node.parentElement
          }
          if (!moved) window.scrollBy(0, delta)
        }
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(raf)
      window.removeEventListener('pointermove', onMove)
    }
  }, [enabled])
}
