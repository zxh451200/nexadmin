import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { Button } from '@/framework/ui/button'
import { getDirectorySync, startDirectorySync, type DirectorySyncSummary, type IdentitySource, type LdapConfig } from './api'

const active = (status?: string) => ['queued', 'pending', 'running', 'in_progress'].includes(status?.toLowerCase() ?? '')
const identity = () => {
  const { user, token } = useAuth.getState()
  return JSON.stringify([user?.tenantId, user?.id, token])
}
const count = (value?: number) => typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : '—'

export function DirectorySync({ source }: { source: IdentitySource }) {
  const scope = useAuth((s) => JSON.stringify([s.user?.tenantId, s.user?.id, s.token]))
  return <SyncControls key={`${scope}:${source.id}`} source={source} scope={scope} />
}

function SyncControls({ source, scope }: { source: IdentitySource; scope: string }) {
  const { t } = useTranslation()
  const enabled = source.enabled && !!(source.config as LdapConfig | null)?.directorySync?.enabled
  const [summary, setSummary] = useState<DirectorySyncSummary | null>(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<'loadError' | 'startError' | 'pollLimit' | null>(null)
  const action = useRef<(start: boolean) => void>(() => {})

  useEffect(() => {
    let disposed = false
    let inFlight = false
    let controller: AbortController | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    const current = (request: AbortController) => !disposed && !request.signal.aborted && identity() === scope
    const cancel = () => {
      controller?.abort()
      clearTimeout(timer)
    }
    // Abort immediately on a tenant/session change, including before React rerenders.
    const unsubscribe = useAuth.subscribe(() => {
      if (identity() !== scope) cancel()
    })
    const run = (start: boolean) => {
      if (disposed || inFlight || identity() !== scope || (start && !enabled)) return
      cancel()
      inFlight = true
      const request = new AbortController()
      controller = request
      let attempts = 0
      let expectedJob: string | undefined
      setNotice(null)
      setBusy(true)
      const read = async () => {
        if (!current(request)) return
        inFlight = true
        setBusy(true)
        try {
          const latest = await getDirectorySync(source.id, request.signal)
          if (!current(request)) return
          // A replica may briefly return the preceding job after POST.
          const waiting = !!expectedJob && latest?.jobId !== expectedJob
          if (!waiting) setSummary(latest ?? null)
          setBusy(false)
          if (enabled && (waiting || active(latest?.status))) {
            if (++attempts >= 60) setNotice('pollLimit')
            else timer = setTimeout(() => {
              if (current(request)) void read()
            }, 3000)
          }
        } catch {
          if (current(request)) {
            setNotice('loadError')
            setBusy(false)
          }
        } finally {
          inFlight = false
        }
      }
      void (async () => {
        if (start) {
          try {
            const result = await startDirectorySync(source.id, request.signal)
            if (!current(request)) return
            expectedJob = result.jobId
            setSummary({ jobId: result.jobId, status: 'queued' })
          } catch {
            inFlight = false
            if (current(request)) {
              setNotice('startError')
              setBusy(false)
            }
            return
          }
        }
        if (current(request)) await read()
      })()
    }
    action.current = run
    run(false)
    return () => {
      disposed = true
      action.current = () => {}
      cancel()
      unsubscribe()
    }
  }, [source.id, enabled, scope])

  return (
    <div className="min-w-0 space-y-2 text-sm">
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={!enabled || busy || active(summary?.status)} onClick={() => action.current(true)}>
          {t('plugin_identity.sync.now')}
        </Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => action.current(false)}>
          {t('plugin_identity.sync.refresh')}
        </Button>
      </div>
      {!enabled ? <p className="text-muted-foreground">{t('plugin_identity.sync.disabled')}</p> : null}
      <div role="status" aria-live="polite" className="space-y-1 break-all">
        <p>{busy ? t('common.loading') : summary?.status || t('plugin_identity.sync.idle')}</p>
        <p>{t('plugin_identity.sync.counts', {
          scanned: count(summary?.scanned),
          created: count(summary?.created),
          updated: count(summary?.updated),
          deactivated: count(summary?.deactivated),
        })}</p>
        {summary?.jobId ? <p>{t('plugin_identity.sync.jobId')}: {summary.jobId}</p> : null}
        {summary?.startedAt ? <p>{t('plugin_identity.sync.startedAt')}: {summary.startedAt}</p> : null}
        {summary?.finishedAt ? <p>{t('plugin_identity.sync.finishedAt')}: {summary.finishedAt}</p> : null}
        {summary?.error ? <p className="text-destructive">{summary.error}</p> : null}
        {notice ? <p className="text-destructive">{t(`plugin_identity.sync.${notice}`)}</p> : null}
      </div>
    </div>
  )
}
