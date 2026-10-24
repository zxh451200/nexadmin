import { useCallback } from 'react'
import { useAuth } from './useAuth'

export function checkPerm(perms: string[], perm: string | string[] | undefined, mode: 'any' | 'all' = 'any') {
  if (!perm || (Array.isArray(perm) && perm.length === 0)) return true
  if (perms.includes('*')) return true
  const list = Array.isArray(perm) ? perm : [perm]
  return mode === 'all' ? list.every((p) => perms.includes(p)) : list.some((p) => perms.includes(p))
}

export function usePermission() {
  const perms = useAuth((s) => s.perms)
  const has = useCallback((perm: string | string[] | undefined) => checkPerm(perms, perm, 'any'), [perms])
  const hasAll = useCallback((list: string[]) => checkPerm(perms, list, 'all'), [perms])
  return { perms, has, hasAny: has, hasAll, isSuper: perms.includes('*') }
}
