import { lazy, Suspense, type ReactNode } from 'react'
import { Route, Routes } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { api } from '@/framework/request/http'

// 向导与提示只在未初始化时才加载与渲染
const NotInitialized = lazy(() => import('./NotInitialized'))
const SetupWizard = lazy(() => import('./SetupWizard'))

function FullPageSpinner() {
  return (
    <div className="flex h-dvh items-center justify-center bg-background" role="status" aria-live="polite">
      <Loader2 className="size-6 animate-spin text-primary motion-reduce:animate-none" aria-hidden="true" />
    </div>
  )
}

export function SetupGate({ children }: { children: ReactNode }) {
  const { data, isLoading } = useQuery({
    queryKey: ['system', 'status'],
    queryFn: () => api.get<{ initialized: boolean }>('/system/status', undefined, { silent: true }),
    retry: 1,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  })

  if (isLoading) return <FullPageSpinner />

  // 读不到状态时按已初始化处理（宁可错判为已初始化）
  if (!data || data.initialized) return <>{children}</>

  return (
    <Suspense fallback={<FullPageSpinner />}>
      <Routes>
        <Route path="/setup" element={<SetupWizard />} />
        <Route path="*" element={<NotInitialized />} />
      </Routes>
    </Suspense>
  )
}
