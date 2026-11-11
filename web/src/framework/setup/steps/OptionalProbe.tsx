import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { setupHeaders, useSetupToken } from '../request'

// 探测结果码与后端 core/setup/probe.go 的 probeResult 一一对应；未知码回落 requestFailed。
const FAILURE_CODES = [
  'invalid', 'connection', 'timeout', 'cancelled', 'denied', 'notFound', 'redirect',
  'upstreamLimited', 'upstream', 'invalidResponse', 'modelNotListed', 'modelsUnsupported',
  'rateLimited',
] as const

type FailureCode = (typeof FAILURE_CODES)[number] | 'requestFailed'

interface ProbeResult { ok: boolean; code: string; scope: string }

export function OptionalProbe({ kind, payload }: { kind: 's3' | 'ai'; payload: unknown }) {
  const { t } = useTranslation()
  const token = useSetupToken()
  const [running, setRunning] = useState(false)
  const [result, setResult] = useState<{ ok: true; code: 'ok' } | { ok: false; code: FailureCode } | null>(null)
  const active = useRef<AbortController>(undefined)
  const key = JSON.stringify(payload)

  useEffect(() => {
    setResult(null)
    setRunning(false)
    active.current?.abort()
    active.current = undefined
    return () => {
      active.current?.abort()
      active.current = undefined
    }
  }, [key, token])

  const run = async () => {
    if (active.current || !token) return
    const controller = new AbortController()
    active.current = controller
    setRunning(true)
    setResult(null)
    const timer = window.setTimeout(() => controller.abort(), 10000)
    try {
      const response = await api.post<ProbeResult>(`/setup/test-${kind}`, payload, {
        silent: true, headers: setupHeaders(token), signal: controller.signal,
      })
      if (active.current !== controller) return
      if (!response || response.scope !== kind || typeof response.ok !== 'boolean' || typeof response.code !== 'string') {
        setResult({ ok: false, code: 'requestFailed' })
        return
      }
      if (response.ok === true) {
        setResult({ ok: true, code: 'ok' })
        return
      }
      const code = FAILURE_CODES.includes(response.code as (typeof FAILURE_CODES)[number])
        ? response.code as FailureCode
        : 'requestFailed'
      setResult({ ok: false, code })
    } catch {
      if (active.current === controller) {
        setResult({ ok: false, code: controller.signal.aborted ? 'timeout' : 'requestFailed' })
      }
    } finally {
      window.clearTimeout(timer)
      if (active.current === controller) {
        active.current = undefined
        setRunning(false)
      }
    }
  }

  const cancel = () => {
    active.current?.abort()
    active.current = undefined
    setRunning(false)
    setResult({ ok: false, code: 'cancelled' })
  }

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">{t(`setup.probe.${kind}Scope`)}</p>
      {!token && <p className="text-sm text-muted-foreground">{t('setup.probe.tokenRequired')}</p>}
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={run} disabled={running || !token}>
          {running ? t('setup.probe.running') : t(`setup.probe.${kind}Button`)}
        </Button>
        {running && <Button type="button" variant="outline" onClick={cancel}>{t('setup.probe.cancel')}</Button>}
      </div>
      {result && (
        <p role="status" aria-live="polite" className={`text-sm ${result.ok ? 'text-success' : 'text-destructive'}`}>
          {result.ok
            ? t(`setup.probe.${kind}Ok`)
            : `${t('setup.probe.failed')}: ${t(`setup.probe.${result.code}`)}`}
        </p>
      )}
    </div>
  )
}
