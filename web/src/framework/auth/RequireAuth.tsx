import { useEffect, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { useAuth } from './useAuth'

export function RequireAuth({ children }: { children: ReactNode }) {
  const { token, user, fetchMe, reset } = useAuth()
  const location = useLocation()
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (token && !user && !failed) {
      fetchMe().catch(() => {
        setFailed(true)
        reset()
      })
    }
  }, [token, user, failed, fetchMe, reset])

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
