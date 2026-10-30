import { useCallback, useState } from 'react'

function read(key: string): string[] | null {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as string[]) : null
  } catch {
    // 存档损坏或存储不可用：退回默认
    return null
  }
}

function write(key: string, set: ReadonlySet<string>) {
  try {
    localStorage.setItem(key, JSON.stringify([...set]))
  } catch {
    // 存储不可用时仅本次生效
  }
}

/**
 * 带 localStorage 记忆的集合状态，用于「展开 / 收起」这类由用户摆出来的布局。
 * 存的是 id 集合（收起的组、展开的行），默认值只在没有存档时生效；
 * add / remove 对「本来就是这个状态」的情况不写存储，避免无意义的写入。
 */
export function usePersistedSet(storageKey: string, initial: readonly string[] = []) {
  const [value, setValue] = useState<Set<string>>(() => new Set(read(storageKey) ?? initial))

  const add = useCallback(
    (id: string) =>
      setValue((prev) => {
        if (prev.has(id)) return prev
        const next = new Set(prev).add(id)
        write(storageKey, next)
        return next
      }),
    [storageKey],
  )

  const remove = useCallback(
    (id: string) =>
      setValue((prev) => {
        if (!prev.has(id)) return prev
        const next = new Set(prev)
        next.delete(id)
        write(storageKey, next)
        return next
      }),
    [storageKey],
  )

  const toggle = useCallback(
    (id: string) =>
      setValue((prev) => {
        const next = new Set(prev)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        write(storageKey, next)
        return next
      }),
    [storageKey],
  )

  const replace = useCallback(
    (next: Iterable<string>) => {
      const set = new Set(next)
      setValue(set)
      write(storageKey, set)
    },
    [storageKey],
  )

  return { value, has: (id: string) => value.has(id), add, remove, toggle, replace }
}
