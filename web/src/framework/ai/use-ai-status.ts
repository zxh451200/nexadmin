import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

export interface AiStatus {
  enabled: boolean
  model: string
}

/** 未配置 AI 时所有入口自动隐藏；加载中与出错均视为未启用，不报错 */
export function useAiStatus() {
  const { data, isLoading } = useQuery({
    queryKey: ['ai', 'status'],
    queryFn: () => api.get<AiStatus>('/ai/status'),
    staleTime: 10_000,
    retry: false,
  })
  return { enabled: data?.enabled === true, model: data?.model ?? '', isLoading }
}
