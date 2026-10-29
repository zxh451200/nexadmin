import { useQuery } from '@tanstack/react-query'
import { MOCK_ENABLED } from '@/framework/env'
import { api } from '@/framework/request/http'

const TTL = 60_000
let cache: { at: number; promise: Promise<string[] | null> } | undefined

/**
 * 已启用（有授权并已挂载）的插件 ID。
 * 返回 null 表示「问不到」（mock 模式、旧后端、网络失败）——调用方按未知处理，保持原行为。
 */
export function fetchActivePlugins(): Promise<string[] | null> {
  if (MOCK_ENABLED) return Promise.resolve(null)
  if (!cache || Date.now() - cache.at > TTL) {
    cache = {
      at: Date.now(),
      promise: api
        .get<string[]>('/plugins/active', undefined, { silent: true })
        .then((ids) => (Array.isArray(ids) ? ids.filter((x): x is string => typeof x === 'string') : null))
        .catch(() => null),
    }
  }
  return cache.promise
}

export function clearActivePluginsCache() {
  cache = undefined
}

/** 路由守卫与插件语言包加载共用同一份结果（60s 内只打一次接口） */
export function useActivePlugins() {
  return useQuery({
    queryKey: ['plugins', 'active'],
    queryFn: fetchActivePlugins,
    staleTime: TTL,
    retry: false,
  })
}
