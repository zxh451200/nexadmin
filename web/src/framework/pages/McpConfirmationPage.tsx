import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { LanguageSwitch } from '@/framework/i18n/LanguageSwitch'
import { api, ApiError } from '@/framework/request/http'
import { ThemeToggle } from '@/framework/theme/ThemeToggle'
import { Button } from '@/framework/ui/button'
import { Card, CardContent } from '@/framework/ui/card'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'

interface Confirmation {
  confirmationId: string
  action: {
    tool: string
    method: string
    route: string
    path: Record<string, string> | null
    query: string
    body: unknown
    target: string
  }
  digest: string
  status: string
  expiresAt: string
  credentialType: string
}

type Approval = Pick<Confirmation, 'confirmationId' | 'digest' | 'status' | 'expiresAt'>

function errorKey(error: unknown, approving = false) {
  if (error instanceof ApiError) {
    if (approving && (error.status === 401 || error.code === 401)) return 'passwordFailed'
    if (error.status === 423 || error.code === 423) return 'locked'
    if ([403, 404, 410].includes(error.status ?? error.code)) return 'invalid'
    if ([400, 409].includes(error.status ?? error.code)) return 'changed'
  }
  return approving ? 'approveFailed' : 'failed'
}

export default function McpConfirmationPage() {
  const { id = '' } = useParams()
  const user = useAuth((s) => s.user)
  const token = useAuth((s) => s.token)
  // Remount when identity or session changes, so no action/credentials cross accounts.
  return <ConfirmationContent key={`${id}:${user?.id}:${user?.tenantId}:${token}`} id={id} />
}

function ConfirmationContent({ id }: { id: string }) {
  const { t } = useTranslation()
  const user = useAuth((s) => s.user)
  const password = useRef<HTMLInputElement>(null)
  const request = useRef<AbortController | null>(null)
  const inFlight = useRef(false)
  const active = useRef(false)
  const [busy, setBusy] = useState(false)
  const [approved, setApproved] = useState(false)
  const [failure, setFailure] = useState('')
  const [now, setNow] = useState(Date.now)
  const validId = /^[a-f0-9]{64}$/i.test(id)
  const confirmation = useQuery({
    queryKey: ['mcp-confirmation', id, user?.id, user?.tenantId],
    queryFn: ({ signal }) => api.get<Confirmation>(`/mcp/confirmations/${id}`, undefined, {
      signal, silent: true, _retry: true, headers: { 'Cache-Control': 'no-store' },
    }),
    enabled: validId,
    retry: false,
    gcTime: 0,
    staleTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchOnMount: 'always',
  })
  const info = confirmation.data
  const deadline = info ? Date.parse(info.expiresAt) : NaN
  const expired = !!info && (!Number.isFinite(deadline) || now >= deadline)
  const accepted = approved || info?.status === 'approved'
  const usable = !!info && info.confirmationId === id && /^[a-f0-9]{64}$/i.test(info.digest)
  const blocked = !usable || expired || accepted || info?.status !== 'pending' ||
    info?.credentialType !== 'password' || !!failure || confirmation.isFetching

  useEffect(() => {
    active.current = true
    const input = password.current
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => {
      active.current = false
      window.clearInterval(timer)
      request.current?.abort()
      if (input) input.value = ''
    }
  }, [])

  useEffect(() => {
    if (blocked && password.current) password.current.value = ''
  }, [blocked])

  const approve = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (blocked || inFlight.current || !info || !password.current?.value) return
    if (Date.now() >= deadline) {
      password.current.value = ''
      setNow(Date.now())
      return
    }
    inFlight.current = true
    setBusy(true)
    // Keep credentials out of query/mutation state, storage and error messages.
    const body = { digest: info.digest, password: password.current.value }
    password.current.value = ''
    const controller = new AbortController()
    request.current = controller
    try {
      const result = await api.post<Approval>(`/mcp/confirmations/${id}/approve`, body, {
        signal: controller.signal, silent: true, _retry: true, preserveSessionOnUnauthorized: true,
        // axios serializes synchronously before dispatch; release the password object then.
        transformRequest: [(data: typeof body) => {
          const json = JSON.stringify(data)
          data.password = ''
          return json
        }],
        headers: { 'Content-Type': 'application/json' },
      })
      if (!active.current) return
      if (result.confirmationId !== id || result.digest !== info.digest || result.status !== 'approved') {
        setFailure('changed')
        return
      }
      setApproved(true)
    } catch (error) {
      if (active.current) setFailure(errorKey(error, true))
    } finally {
      body.password = ''
      inFlight.current = false
      if (active.current) {
        if (password.current) password.current.value = ''
        setBusy(false)
      }
    }
  }

  const notice = !validId ? 'invalid' : confirmation.isError ? errorKey(confirmation.error) :
    expired ? 'expired' : accepted ? 'approved' : failure ||
    (info && !usable ? 'invalid' : info && info.status !== 'pending' ? 'changed' :
      info && info.credentialType !== 'password' ? 'unsupported' : '')

  return (
    <div className="min-h-dvh bg-background text-foreground">
      <header className="flex justify-end gap-1 p-4"><LanguageSwitch /><ThemeToggle /></header>
      <main className="mx-auto max-w-2xl px-4 pb-12">
        <Card>
          <CardContent className="space-y-6 p-6 md:p-8">
            <h1 className="text-xl font-semibold">{t('mcpConfirmation.title')}</h1>
            <p className="text-sm text-muted-foreground">{t('mcpConfirmation.intro')}</p>
            <p className="break-all text-sm">{t('mcpConfirmation.account')}: {user?.nickname || user?.username} ({user?.username})</p>
            {confirmation.isFetching && <p role="status">{t('mcpConfirmation.loading')}</p>}
            {notice && <p role={accepted && !expired ? 'status' : 'alert'} className="rounded-md border p-4 text-sm">{t(`mcpConfirmation.${notice}`)}</p>}
            {usable && !confirmation.isError && (
              <>
                <dl className="space-y-3 text-sm">
                  {([
                    ['tool', info.action.tool],
                    ['method', info.action.method],
                    ['route', info.action.route],
                    ['target', info.action.target],
                    ['expires', Number.isFinite(deadline) ? new Date(deadline).toLocaleString() : info.expiresAt],
                  ] as const).map(([label, value]) => (
                    <div key={label}>
                      <dt className="font-medium">{t(`mcpConfirmation.${label}`)}</dt>
                      <dd className="whitespace-pre-wrap break-all font-mono" dir="ltr">{value}</dd>
                    </div>
                  ))}
                </dl>
                <p className="text-sm text-muted-foreground">{t('mcpConfirmation.privacy')}</p>
                <details>
                  <summary className="cursor-pointer text-sm font-medium">{t('mcpConfirmation.payload')}</summary>
                  <pre className="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-4 text-xs" dir="ltr">
                    {JSON.stringify(info.action, null, 2)}
                  </pre>
                </details>
              </>
            )}
            {!accepted && !expired && usable && !confirmation.isError && (
              <form onSubmit={(event) => { void approve(event) }} className="space-y-3" autoComplete="off">
                <Label htmlFor="confirmation-password">{t('mcpConfirmation.password')}</Label>
                <Input ref={password} id="confirmation-password" type="password" autoComplete="off"
                  required disabled={blocked || busy} aria-describedby="confirmation-password-hint" />
                <p id="confirmation-password-hint" className="text-xs text-muted-foreground">{t('mcpConfirmation.passwordHint')}</p>
                <Button type="submit" disabled={blocked || busy}>{t('mcpConfirmation.approve')}</Button>
              </form>
            )}
            {validId && !expired && !accepted && (failure || confirmation.isError) && (
              <Button type="button" variant="outline" disabled={busy || confirmation.isFetching} onClick={() => {
                setFailure('')
                if (password.current) password.current.value = ''
                void confirmation.refetch()
              }}>{t('mcpConfirmation.reload')}</Button>
            )}
          </CardContent>
        </Card>
      </main>
    </div>
  )
}
