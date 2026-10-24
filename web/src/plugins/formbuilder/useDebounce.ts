import { useEffect, useState } from 'react'

/** 输入防抖：value 稳定 delay 毫秒后才更新返回值 */
export function useDebounce<T>(value: T, delay = 300): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setV(value), delay)
    return () => clearTimeout(timer)
  }, [value, delay])
  return v
}
