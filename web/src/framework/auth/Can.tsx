import type { ReactNode } from 'react'
import { usePermission } from './usePermission'

interface CanProps {
  /** 单个权限点，或多个（满足任一即可） */
  perm: string | string[]
  fallback?: ReactNode
  children: ReactNode
}

export function Can({ perm, fallback = null, children }: CanProps) {
  const { has } = usePermission()
  return <>{has(perm) ? children : fallback}</>
}
