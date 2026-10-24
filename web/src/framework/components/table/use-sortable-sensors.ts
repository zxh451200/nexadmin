import { KeyboardSensor, MouseSensor, TouchSensor, useSensor, useSensors, type Modifier } from '@dnd-kit/core'
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
