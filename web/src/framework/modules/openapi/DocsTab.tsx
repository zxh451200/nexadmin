import { useMemo, useState } from 'react'
import { Download, Lock, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Skeleton } from '@/framework/ui/skeleton'
import { buildSignCurl, buildTokenCurl, downloadOpenApiJson, useApiRoutes, type ApiRoute } from './api'
import { CodeBlock } from './RevealDialog'

const METHOD_VARIANT: Record<string, BadgeProps['variant']> = {
  GET: 'info',
  POST: 'success',
  PUT: 'warning',
  PATCH: 'warning',
  DELETE: 'destructive',
}

export function MethodBadge({ method }: { method: string }) {
  return (
    <Badge variant={METHOD_VARIANT[method] ?? 'secondary'} className="w-16 justify-center font-mono">
      {method}
    </Badge>
  )
}

function saveBlob(filename: string, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm break-all text-foreground">{children}</dd>
    </div>
  )
}

const authKey = (r: ApiRoute) => (!r.auth ? 'public' : r.superOnly ? 'super' : 'login')

export function DocsTab() {
  const { t } = useTranslation()
  const { data, isLoading } = useApiRoutes()
  const [kw, setKw] = useState('')
  const [detail, setDetail] = useState<ApiRoute | null>(null)
  const [downloading, setDownloading] = useState(false)

  const groups = useMemo(() => {
    const q = kw.trim().toLowerCase()
    const rows = (data ?? []).filter((r) => !q || r.path.toLowerCase().includes(q) || r.perm.toLowerCase().includes(q))
    const map = new Map<string, ApiRoute[]>()
    for (const r of rows) map.set(r.module, [...(map.get(r.module) ?? []), r])
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [data, kw])

  const onDownload = async () => {
    setDownloading(true)
    try {
      saveBlob('openapi.json', await downloadOpenApiJson())
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    } finally {
      setDownloading(false)
    }
  }

  const origin = window.location.origin
  const yes = t('openapi.docs.yes')
  const no = t('openapi.docs.no')

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="relative w-full md:w-80">
          <Search className="pointer-events-none absolute start-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <Input
            value={kw}
            onChange={(e) => setKw(e.target.value)}
            placeholder={t('openapi.docs.searchPlaceholder')}
            aria-label={t('openapi.docs.searchPlaceholder')}
            className="ps-9"
          />
        </div>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">{t('openapi.docs.count', { count: data?.length ?? 0 })}</span>
          <Button type="button" variant="outline" disabled={downloading} onClick={() => void onDownload()}>
            <Download aria-hidden="true" />
            {t('openapi.docs.download')}
          </Button>
        </div>
      </div>

      {isLoading ? (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : groups.length === 0 ? (
        <EmptyState title={t('openapi.docs.empty')} />
      ) : (
        groups.map(([module, rows]) => (
          <section key={module} className="overflow-hidden rounded-lg border border-border">
            <h3 className="flex items-center gap-2 border-b border-border bg-muted/40 px-4 py-2 text-sm font-semibold text-foreground">
              {module}
              <Badge variant="secondary">{rows.length}</Badge>
            </h3>
            <ul className="divide-y divide-border">
              {rows.map((r) => (
                <li key={`${r.method} ${r.path}`}>
                  <button
                    type="button"
                    onClick={() => setDetail(r)}
                    className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-start hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:outline-none"
                  >
                    <MethodBadge method={r.method} />
                    <code className="min-w-0 flex-1 font-mono text-xs break-all text-foreground">{r.path}</code>
                    {r.perm ? <code className="font-mono text-xs text-muted-foreground">{r.perm}</code> : null}
                    {r.auth ? (
                      <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                        <Lock className="h-3.5 w-3.5" aria-hidden="true" />
                        {t(`openapi.docs.auth.${authKey(r)}`)}
                      </span>
                    ) : null}
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))
      )}

      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(o) => !o && setDetail(null)}
        size="lg"
        title={t('openapi.docs.detail')}
        description={detail ? `${detail.method} ${detail.path}` : undefined}
        footer={
          <Button type="button" onClick={() => setDetail(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {detail ? (
          <div className="flex flex-col gap-4">
            <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <DetailRow label={t('openapi.docs.fields.module')}>{detail.module}</DetailRow>
              <DetailRow label={t('openapi.docs.fields.perm')}>
                {detail.perm ? <code className="font-mono text-xs">{detail.perm}</code> : <span className="text-muted-foreground">{t('openapi.docs.noPerm')}</span>}
              </DetailRow>
              <DetailRow label={t('openapi.docs.fields.auth')}>{t(`openapi.docs.auth.${authKey(detail)}`)}</DetailRow>
              <DetailRow label={t('openapi.docs.fields.token')}>{detail.auth && detail.tokenCallable ? yes : no}</DetailRow>
              <DetailRow label={t('openapi.docs.fields.app')}>{detail.auth && detail.appCallable ? yes : no}</DetailRow>
              {detail.summary ? <DetailRow label={t('openapi.docs.fields.summary')}>{detail.summary}</DetailRow> : null}
            </dl>
            <div className="flex flex-col gap-2">
              <h4 className="text-sm font-medium text-foreground">{t('openapi.docs.curlToken')}</h4>
              {detail.auth && detail.tokenCallable ? (
                <CodeBlock code={buildTokenCurl(origin, detail)} label={t('openapi.docs.copyCurl')} />
              ) : (
                <p className="text-sm text-muted-foreground">{detail.auth ? t('openapi.docs.notCallable') : t('openapi.docs.publicNote')}</p>
              )}
            </div>
            {detail.auth ? (
              <div className="flex flex-col gap-2">
                <h4 className="text-sm font-medium text-foreground">{t('openapi.docs.curlSign')}</h4>
                {detail.appCallable ? (
                  <CodeBlock code={buildSignCurl(origin, detail)} label={t('openapi.docs.copyCurl')} />
                ) : (
                  <p className="text-sm text-muted-foreground">{t('openapi.docs.notCallable')}</p>
                )}
              </div>
            ) : null}
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
