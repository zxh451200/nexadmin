import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'
import type { UploadConfig, UploadKind } from './types'

export function useUploadConfig() {
  return useQuery({
    queryKey: ['upload-config'],
    queryFn: () => api.get<UploadConfig>('/config/upload'),
    staleTime: 5 * 60 * 1000,
  })
}

export type PrecheckError = { code: 'size'; limitMb: number } | { code: 'type' } | { code: 'kind' }

const KIND_PREFIX: Record<UploadKind, string | null> = { image: 'image/', video: 'video/', file: null }

function typeAllowed(file: File, allowed: string[]) {
  if (!allowed.length) return true
  const name = file.name.toLowerCase()
  return allowed.some((a) => {
    const p = a.trim().toLowerCase()
    if (!p) return false
    if (p.endsWith('/*')) return file.type.toLowerCase().startsWith(p.slice(0, -1))
    if (p.startsWith('.')) return name.endsWith(p)
    return file.type.toLowerCase() === p
  })
}

/** 选文件后的前端预检（大小 0 = 不限制；类型白名单为空 = 不限制） */
export function precheckFile(file: File, kind: UploadKind, cfg: UploadConfig | undefined): PrecheckError | null {
  const prefix = KIND_PREFIX[kind]
  if (prefix && !file.type.startsWith(prefix)) return { code: 'kind' }
  if (!cfg) return null
  if (!typeAllowed(file, cfg.allowedTypes ?? [])) return { code: 'type' }
  const limitMb = kind === 'image' ? cfg.maxImageMb : kind === 'video' ? cfg.maxVideoMb : cfg.maxFileMb
  if (limitMb > 0 && file.size > limitMb * 1024 * 1024) return { code: 'size', limitMb }
  return null
}

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}
