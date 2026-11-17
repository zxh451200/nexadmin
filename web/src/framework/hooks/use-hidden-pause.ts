import { useEffect, useState } from 'react'

/** refetchInterval 的离页暂停封装：标签页不可见时停轮询，回前台立即恢复。
 *  用法：refetchInterval: useHiddenPause(5000) —— 间隔值不变，只是隐藏时不再空转。 */
export function useHiddenPause(intervalMs: number) {
  const [hidden, setHidden] = useState(() => typeof document !== 'undefined' && document.hidden)
  useEffect(() => {
    const on = () => setHidden(document.hidden)
    document.addEventListener('visibilitychange', on)
    return () => document.removeEventListener('visibilitychange', on)
  }, [])
  return () => (hidden ? false : intervalMs)
}
