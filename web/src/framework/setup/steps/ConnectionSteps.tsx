import { useEffect, useRef, useState } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/framework/request/http'
import { Button } from '@/framework/ui/button'
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/framework/ui/form'
import { Label } from '@/framework/ui/label'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Skeleton } from '@/framework/ui/skeleton'
import { DB_TYPES, DEFAULT_PORT, type SetupValues } from '../schema'
import { SelectField, SwitchField, TextField } from '../Fields'
import { setupHeaders, useSetupToken } from '../request'

interface EnvCheck {
  key: string
  ok: boolean
  detail?: string
}

export function EnvStep() {
  const { t, i18n } = useTranslation()
  const token = useSetupToken()
  const [data, setData] = useState<{ checks: EnvCheck[] }>()
  const [isLoading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setData(undefined)
    setError(undefined)
    // Debounce token typing to avoid exhausting the setup route's rate limit.
    const timer = window.setTimeout(() => {
      api.get<{ checks: EnvCheck[] }>('/setup/env', undefined, {
        silent: true, headers: setupHeaders(token), signal: controller.signal,
      }).then((result) => {
        if (!controller.signal.aborted) setData(result)
      }).catch((e: unknown) => {
        if (!controller.signal.aborted) setError(e instanceof Error ? e.message : t('error.unknown'))
      }).finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    }, 300)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [token, attempt, i18n.language, t])

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <p className="text-sm font-medium">{t('setup.envCheck')}</p>
        {isLoading && <Skeleton className="h-24 w-full" />}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <Button type="button" variant="outline" disabled={isLoading} onClick={() => setAttempt((n) => n + 1)}>{t('setup.retry')}</Button>
        <ul className="divide-y divide-border rounded-lg border border-border">
          {(data?.checks ?? []).map((c) => (
            <li key={c.key} className="flex min-h-11 items-center gap-3 px-3 py-2 text-sm">
              {c.ok ? (
                <CheckCircle2 className="size-4 shrink-0 text-success" aria-hidden="true" />
              ) : (
                <XCircle className="size-4 shrink-0 text-destructive" aria-hidden="true" />
              )}
              <span className="min-w-0 flex-1">{t(`setup.env.${c.key}`)}</span>
              <span className="num min-w-0 max-w-[50%] break-words text-end text-xs text-muted-foreground">{c.detail}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

interface TestResult {
  ok: boolean
  message?: string
  version?: string
}

function TestConnection({ url, payload }: { url: string; payload: unknown }) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<TestResult | null>(null)
  const token = useSetupToken()
  const generation = useRef(0)
  const key = JSON.stringify(payload)

  useEffect(() => {
    generation.current++
    setResult(null)
    return () => { generation.current++ }
  }, [key, token])

  const run = async () => {
    setLoading(true)
    const current = generation.current
    try {
      const response = await api.post<TestResult>(url, payload, { silent: true, headers: setupHeaders(token) })
      if (current === generation.current) setResult(response)
    } catch (e) {
      if (current === generation.current) setResult({ ok: false, message: e instanceof Error ? e.message : undefined })
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
      <Button type="button" variant="outline" onClick={run} disabled={loading}>
        {loading && <Loader2 className="me-2 size-4 animate-spin motion-reduce:animate-none" aria-hidden="true" />}
        {t('setup.testConnection')}
      </Button>
      {result && (
        <p role="status" className={result.ok ? 'flex min-w-0 items-start gap-2 break-words text-sm text-success' : 'flex min-w-0 items-start gap-2 break-words text-sm text-destructive'}>
          {result.ok ? <CheckCircle2 className="mt-0.5 size-4 shrink-0" aria-hidden="true" /> : <XCircle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />}
          <span className="min-w-0">{result.ok ? `${t('setup.testOk')}${result.version ? ` · ${result.version}` : ''}` : `${t('setup.testFailed')}${result.message ? `: ${result.message}` : ''}`}</span>
        </p>
      )}
    </div>
  )
}

export function DbStep() {
  const { t } = useTranslation()
  const { control, setValue } = useFormContext<SetupValues>()
  const db = useWatch({ control, name: 'db' })

  return (
    <div className="space-y-4">
      <SelectField
        name="db.type"
        label={t('setup.db.type')}
        options={[
          { value: 'mysql', label: 'MySQL' },
          { value: 'mariadb', label: 'MariaDB' },
          { value: 'postgresql', label: 'PostgreSQL' },
        ]}
        onValueChange={(v) => setValue('db.port', DEFAULT_PORT[v as (typeof DB_TYPES)[number]])}
      />
      <div className="grid gap-4 md:grid-cols-2">
        <TextField name="db.host" label={t('setup.db.host')} />
        <TextField name="db.port" label={t('setup.db.port')} inputMode="numeric" />
        <TextField name="db.username" label={t('setup.db.username')} autoComplete="off" />
        <TextField name="db.password" label={t('setup.db.password')} type="password" autoComplete="new-password" />
      </div>
      <TextField name="db.database" label={t('setup.db.database')} />
      <SwitchField name="db.autoCreate" label={t('setup.db.autoCreate')} description={t('setup.db.autoCreateHint')} />
      <TestConnection url="/setup/test-db" payload={db} />
    </div>
  )
}

export function CacheStep() {
  const { t } = useTranslation()
  const { control } = useFormContext<SetupValues>()
  const cache = useWatch({ control, name: 'cache' })

  return (
    <div className="space-y-4">
      <FormField
        control={control}
        name="cache.type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('setup.cache.type')}</FormLabel>
            <FormControl>
              <RadioGroup value={field.value} onValueChange={field.onChange} className="grid gap-3 md:grid-cols-2">
                {(['memory', 'valkey'] as const).map((v) => (
                  <Label
                    key={v}
                    htmlFor={`cache-${v}`}
                    className="flex min-h-11 cursor-pointer items-start gap-3 rounded-lg border border-border p-3 has-[:checked]:border-primary has-[:checked]:bg-primary/5"
                  >
                    <RadioGroupItem id={`cache-${v}`} value={v} className="mt-0.5" />
                    <span className="space-y-1">
                      <span className="block text-sm font-medium">{t(`setup.cache.${v}`)}</span>
                      <span className="block text-xs font-normal text-muted-foreground">{t(`setup.cache.${v}Hint`)}</span>
                    </span>
                  </Label>
                ))}
              </RadioGroup>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      {cache.type === 'valkey' && (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <TextField name="cache.addr" label={t('setup.cache.addr')} placeholder="127.0.0.1:6379" />
            <TextField name="cache.password" label={t('setup.cache.password')} type="password" autoComplete="new-password" />
          </div>
          <TestConnection url="/setup/test-cache" payload={cache} />
        </>
      )}
    </div>
  )
}
