import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/framework/request/http'

interface SystemStatus {
  demo?: boolean
  account?: string
  password?: string
}

// 演示站信息来自 /system/status（config demo.enabled）。SetupGate 启动时用同一个
// queryKey 拉过状态，这里共享缓存不产生额外请求；读不到（未初始化 / 离线）按未开启处理。
function useSystemStatus() {
  return useQuery({
    queryKey: ['system', 'status'],
    queryFn: () => api.get<SystemStatus>('/system/status', undefined, { silent: true }).catch(() => undefined),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  })
}

// 是否处于演示站模式（登录页 / 顶栏徽标）。
export function useDemoMode(): boolean {
  const { data } = useSystemStatus()
  return data?.demo === true
}

// 演示账号凭据：后端仅在 demo.enabled 且配置了 account / password 时下发。
// 返回值用 useMemo 保持稳定，供表单做预填 effect 的依赖；null = 不展示。
export function useDemoAccount(): { account: string; password: string } | null {
  const { data } = useSystemStatus()
  const demo = data?.demo
  const account = data?.account
  const password = data?.password
  return useMemo(
    () => (demo === true && account && password ? { account, password } : null),
    [demo, account, password],
  )
}
