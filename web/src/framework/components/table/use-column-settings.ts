import { useCallback, useMemo, useState } from 'react'
import type { Column } from './types'

const PREFIX = 'nex-table-cols:'

function load(key?: string): string[] {
  if (!key || typeof localStorage === 'undefined') return []
  try {
    const raw = localStorage.getItem(PREFIX + key)
    const parsed = raw ? JSON.parse(raw) : []
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

/** 列显示/隐藏设置，按 settingsKey 存 localStorage（保存的是被隐藏的列 key） */
export function useColumnSettings<T>(columns: Column<T>[], settingsKey?: string) {
  const [hidden, setHidden] = useState<string[]>(() => load(settingsKey))

  const persist = useCallback(
    (next: string[]) => {
      setHidden(next)
      if (!settingsKey || typeof localStorage === 'undefined') return
      try {
        if (next.length) localStorage.setItem(PREFIX + settingsKey, JSON.stringify(next))
        else localStorage.removeItem(PREFIX + settingsKey)
      } catch {
        /* 存储不可用时忽略 */
      }
    },
    [settingsKey],
  )

  const toggle = useCallback(
    (key: string) => persist(hidden.includes(key) ? hidden.filter((k) => k !== key) : [...hidden, key]),
    [hidden, persist],
  )
  const reset = useCallback(() => persist([]), [persist])

  const visibleColumns = useMemo(
    () => columns.filter((c) => c.hideable === false || !hidden.includes(c.key)),
    [columns, hidden],
  )

  return { hidden, toggle, reset, visibleColumns }
}
