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
  const platform = useAuth((s) => s.user?.platform)
  const has = useCallback((perm: string | string[] | undefined) => checkPerm(perms, perm, 'any'), [perms])
  const hasAll = useCallback((list: string[]) => checkPerm(perms, list, 'all'), [perms])
  // isPlatform 与后端 core.IsPlatformCaller 同一条判据（服务端仍然自己判，这里只决定界面给不给写入口）。
  // 不要用 isSuper 代替：那是 perms.includes('*')，会把平台租户里的非超管坐席误判成无权写。
  return { perms, has, hasAny: has, hasAll, isSuper: perms.includes('*'), isPlatform: platform === true }
}
