import { useEffect, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ApiError } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { useAuth } from './useAuth'

/** 只有服务端明确否认凭证时才清会话；网络抖动/5xx 不能让用户丢掉登录态。 */
function isUnauthorized(err: unknown) {
  // 真实响应看 HTTP 状态；mock 适配器只带业务码，回落看 code。
  return err instanceof ApiError && [401, 403].includes(err.status ?? err.code)
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const { token, user, fetchMe, reset } = useAuth()
  const location = useLocation()
  const [failed, setFailed] = useState(false)
  const [loadFailed, setLoadFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (!token || user || failed || loadFailed) return
    let active = true
    fetchMe().catch((err) => {
      if (!active) return
      if (isUnauthorized(err)) {
        setFailed(true)
        reset()
        return
      }
      setLoadFailed(true)
    })
    return () => {
      active = false
    }
  }, [token, user, failed, loadFailed, attempt, fetchMe, reset])

  if (loadFailed) {
    return (
      <div className="flex h-dvh flex-col items-center justify-center gap-3 bg-background px-6 text-center" role="alert">
        <p className="text-sm text-muted-foreground">{t('common.loadFailed')}</p>
        <Button
          variant="outline"
          onClick={() => {
            setLoadFailed(false)
            setAttempt((n) => n + 1)
          }}
        >
          {t('common.retry')}
        </Button>
      </div>
    )
  }
  if (!token || failed) {
    const returnTo = encodeURIComponent(location.pathname + location.search + location.hash)
    return <Navigate to={`/login?returnTo=${returnTo}`} replace />
  }
  if (!user) {
    return (
      <div className="flex h-dvh items-center justify-center bg-background" role="status" aria-live="polite">
        <Loader2 className="size-6 animate-spin text-primary motion-reduce:animate-none" aria-hidden="true" />
      </div>
    )
  }
  return <>{children}</>
}
