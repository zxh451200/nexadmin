import { useEffect, useState, type ReactNode } from 'react'
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  Copy,
  Download,
  KeyRound,
  Loader2,
  Pencil,
  Plug,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card } from '@/framework/ui/card'
import { Switch } from '@/framework/ui/switch'
import {
  getSPCert,
  promoteSPCert,
  refreshMetadata,
  rotateSPCert,
  useDeleteSource,
  useSetEnabled,
  useSources,
  useTestSource,
  type SamlSource,
  type SPCert,
  type TestResult,
} from './api'
import SourceFormDialog from './SourceFormDialog'

function CopyButton({ text, label }: { text: string; label: string }) {
  const { t } = useTranslation()
  const [done, setDone] = useState(false)
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      onClick={() => {
        void (async () => {
          try {
            await navigator.clipboard.writeText(text)
            setDone(true)
            toast.success(t('plugin_saml.btn.copied'))
            window.setTimeout(() => setDone(false), 1500)
          } catch {
            toast.error(t('plugin_saml.btn.copyFailed'))
          }
        })()
      }}
    >
      {done ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
    </Button>
  )
}

function ValueRow({ label, value, children }: { label: string; value?: string; children?: ReactNode }) {
  return (
    <div className="flex min-w-0 items-start gap-1.5">
      <span className="shrink-0 text-muted-foreground">{label}:</span>
      <code className="min-w-0 break-all rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{value || '—'}</code>
      {children}
    </div>
  )
}

function CertStatus({ idp }: { idp: SamlSource['idp'] }) {
  const { t } = useTranslation()
  if (!idp?.certNotAfter || idp.certCount === 0) return null
  const date = new Date(idp.certNotAfter).toLocaleDateString()
  if (idp.certExpired) return <Badge variant="destructive">{t('plugin_saml.cert.expired', { date })}</Badge>
  if (idp.certExpiringSoon) return <Badge variant="warning">{t('plugin_saml.cert.expiring', { date })}</Badge>
  return <Badge variant="success">{t('plugin_saml.cert.ok', { date })}</Badge>
}

function TestDialog({ result, onClose }: { result: TestResult | null; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <ResponsiveDialog
      open={!!result}
      onOpenChange={(o) => !o && onClose()}
      title={t('plugin_saml.test.title')}
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {result ? (
        <div className="space-y-3">
          <div>
            {result.ok ? (
              <Badge variant="success">{t('plugin_saml.test.passed')}</Badge>
            ) : (
              <Badge variant="destructive">{t('plugin_saml.test.failed')}</Badge>
            )}
          </div>
          <ul className="space-y-3">
            {result.checks.map((check) => (
              <li key={check.key} className="flex items-start gap-2 text-sm">
                {check.ok ? (
                  check.warning ? (
                    <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
                  ) : (
                    <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" aria-hidden="true" />
                  )
                ) : (
                  <XCircle className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
                )}
                <div className="min-w-0">
                  <div className="font-medium">{t(`plugin_saml.test.check.${check.key}`)}</div>
                  {check.detail ? <div className="break-all text-xs text-muted-foreground">{check.detail}</div> : null}
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}

function CertDialog({ source, onClose }: { source: SamlSource | null; onClose: () => void }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const qc = useQueryClient()
  const [data, setData] = useState<SPCert | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [promoting, setPromoting] = useState(false)

  useEffect(() => {
    if (!source) return
    setData(null)
    setErr(null)
    setLoading(true)
    getSPCert(source.id)
      .then(setData)
      .catch((e: Error) => setErr(e.message))
      .finally(() => setLoading(false))
  }, [source])

  const rotate = async () => {
    if (!source) return
    if (!(await confirm({ description: t('plugin_saml.cert.rotateConfirm', { name: source.name }) }))) return
    setRotating(true)
    try {
      setData(await rotateSPCert(source.id))
      toast.success(t('plugin_saml.cert.published'))
      void qc.invalidateQueries({ queryKey: ['plugin_saml'] })
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setRotating(false)
    }
  }

  // 两段式轮换第二步：确认 IdP 已导入新证书后再切换签名证书，旧证书自动保留 7 天做解密兜底。
  const promote = async () => {
    if (!source) return
    if (!(await confirm({ description: t('plugin_saml.cert.promoteConfirm', { name: source.name }) }))) return
    setPromoting(true)
    try {
      setData(await promoteSPCert(source.id))
      toast.success(t('plugin_saml.cert.promoted'))
      void qc.invalidateQueries({ queryKey: ['plugin_saml'] })
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setPromoting(false)
    }
  }

  const download = () => {
    if (!data || !source) return
    const url = URL.createObjectURL(new Blob([data.cert], { type: 'application/x-pem-file' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `${source.name}.crt`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <ResponsiveDialog
      open={!!source}
      onOpenChange={(o) => !o && onClose()}
      title={t('plugin_saml.cert.title')}
      size="lg"
      footer={
        <>
          <Button type="button" variant="outline" onClick={onClose}>
            {t('common.close')}
          </Button>
          <Can perm="saml:edit">
            <Button type="button" variant="outline" onClick={() => void rotate()} disabled={rotating || !data || !!data.nextCert}>
              {rotating ? <Loader2 className="animate-spin" aria-hidden="true" /> : <RefreshCw aria-hidden="true" />}
              {t('plugin_saml.cert.publish')}
            </Button>
            {data?.nextCert ? (
              <Button type="button" onClick={() => void promote()} disabled={promoting}>
                {promoting ? <Loader2 className="animate-spin" aria-hidden="true" /> : <ShieldCheck aria-hidden="true" />}
                {t('plugin_saml.cert.promote')}
              </Button>
            ) : null}
          </Can>
          <Button type="button" onClick={download} disabled={!data}>
            <Download aria-hidden="true" />
            {t('plugin_saml.cert.download')}
          </Button>
        </>
      }
    >
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="animate-spin" aria-hidden="true" />
          {t('common.loading')}
        </div>
      ) : err ? (
        <p className="text-sm text-destructive">{err}</p>
      ) : data ? (
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-sm text-muted-foreground">
              {t('plugin_saml.cert.expires', { date: new Date(data.notAfter).toLocaleString() })}
            </span>
            <CopyButton text={data.cert} label={t('plugin_saml.btn.copy')} />
          </div>
          <p className="rounded-md border border-warning/40 bg-warning/10 p-2 text-xs text-muted-foreground">
            {t('plugin_saml.cert.rotateHint')}
          </p>
          {data.nextCert ? (
            <div className="space-y-2 rounded-md border border-info/40 bg-info/10 p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-sm font-medium">{t('plugin_saml.cert.nextTitle')}</span>
                <CopyButton text={data.nextCert} label={t('plugin_saml.btn.copy')} />
              </div>
              <p className="text-xs text-muted-foreground">
                {t('plugin_saml.cert.nextHint', {
                  date: data.nextNotAfter ? new Date(data.nextNotAfter).toLocaleString() : '—',
                })}
              </p>
            </div>
          ) : null}
          <pre className="max-h-80 overflow-auto rounded-md bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap break-all">
            {data.cert}
          </pre>
          {data.prevCert ? (
            <p className="rounded-md border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
              {t('plugin_saml.cert.prevHint', {
                date: data.prevUntil ? new Date(data.prevUntil).toLocaleString() : '—',
              })}
            </p>
          ) : null}
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}

export default function SamlPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const qc = useQueryClient()
  const { data = [], isLoading } = useSources()
  const setEnabled = useSetEnabled()
  const remove = useDeleteSource()
  const test = useTestSource()
  const [editing, setEditing] = useState<SamlSource | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)
  const [certSource, setCertSource] = useState<SamlSource | null>(null)
  const [refreshing, setRefreshing] = useState<string | null>(null)

  const openForm = (s: SamlSource | null) => {
    setEditing(s)
    setFormOpen(true)
  }
  const onDelete = async (s: SamlSource) => {
    if (!(await confirm({ destructive: true, description: t('plugin_saml.deleteConfirm', { name: s.name }) }))) return
    try {
      await remove.mutateAsync(s.id)
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const onToggle = async (s: SamlSource, enabled: boolean) => {
    try {
      await setEnabled.mutateAsync({ id: s.id, enabled })
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const onRefresh = async (s: SamlSource) => {
    setRefreshing(s.id)
    try {
      const r = await refreshMetadata(s.id)
      toast.success(r.changed ? t('plugin_saml.refresh.changed') : t('plugin_saml.refresh.unchanged'))
      void qc.invalidateQueries({ queryKey: ['plugin_saml'] })
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setRefreshing(null)
    }
  }
  const onTest = async (s: SamlSource) => {
    try {
      setTestResult(await test.mutateAsync(s.id))
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const newButton = (
    <Can perm="saml:edit">
      <Button onClick={() => openForm(null)}>
        <Plus aria-hidden="true" />
        {t('plugin_saml.add')}
      </Button>
    </Can>
  )

  return (
    <PageContainer>
      <PageHeader
        title={t('plugin_saml.title')}
        description={t('plugin_saml.description')}
        actions={data.length > 0 ? newButton : undefined}
      />

      {isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="animate-spin" aria-hidden="true" />
          {t('common.loading')}
        </div>
      ) : data.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck className="size-6" aria-hidden="true" />}
          title={t('plugin_saml.empty')}
          action={newButton}
          description={
            <div className="space-y-2">
              <p>{t('plugin_saml.emptyHint')}</p>
              <ol className="mx-auto max-w-sm list-decimal space-y-1 ps-5 text-start text-xs">
                <li>{t('plugin_saml.emptySteps.get')}</li>
                <li>{t('plugin_saml.emptySteps.create')}</li>
                <li>{t('plugin_saml.emptySteps.enable')}</li>
              </ol>
            </div>
          }
        />
      ) : (
        <div className="space-y-3">
          {data.map((s) => (
            <Card key={s.id} className="space-y-3 p-4">
              <div className="flex flex-wrap items-center gap-2">
                <KeyRound className="size-4 text-muted-foreground" aria-hidden="true" />
                <span className="font-medium">{s.name}</span>
                <Badge variant={s.enabled ? 'success' : 'secondary'}>
                  {s.enabled ? t('plugin_saml.on') : t('plugin_saml.off')}
                </Badge>
                <CertStatus idp={s.idp} />
                <div className="ms-auto flex flex-wrap items-center gap-1">
                  <Can
                    perm="saml:edit"
                    fallback={
                      <span className="px-2 text-xs text-muted-foreground">
                        {s.enabled ? t('plugin_saml.on') : t('plugin_saml.off')}
                      </span>
                    }
                  >
                    <Switch
                      checked={s.enabled}
                      aria-label={t('plugin_saml.fields.enabled')}
                      onCheckedChange={(v) => void onToggle(s, v)}
                    />
                  </Can>
                  <Can perm="saml:edit">
                    <Button variant="outline" size="sm" disabled={test.isPending} onClick={() => void onTest(s)}>
                      <Plug className="size-4" aria-hidden="true" />
                      {t('plugin_saml.btn.test')}
                    </Button>
                  </Can>
                  <Can perm="saml:edit">
                    <Button variant="ghost" size="icon" aria-label={t('common.edit')} title={t('common.edit')} onClick={() => openForm(s)}>
                      <Pencil aria-hidden="true" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="text-destructive"
                      aria-label={t('common.delete')}
                      title={t('common.delete')}
                      onClick={() => void onDelete(s)}
                    >
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </Can>
                </div>
              </div>

              {s.idp?.error ? (
                <p className="text-sm text-destructive">{s.idp.error}</p>
              ) : (
                <div className="grid gap-1.5 md:grid-cols-2">
                  <ValueRow label={t('plugin_saml.fields.idpEntityId')} value={s.idp?.entityId} />
                  <ValueRow
                    label={t('plugin_saml.fields.idpSso')}
                    value={
                      s.idp?.ssoUrl
                        ? `${s.idp.ssoUrl}${s.idp.ssoBinding ? ` (${t(`plugin_saml.binding.${s.idp.ssoBinding}`)})` : ''}`
                        : ''
                    }
                  />
                  <ValueRow label={t('plugin_saml.fields.idpSlo')} value={s.idp?.sloUrl} />
                  <ValueRow label={t('plugin_saml.fields.metadataSource')} value={s.idpMetadataUrl || t('plugin_saml.source.pasted')} />
                  {s.metadataRefreshedAt ? (
                    <ValueRow
                      label={t('plugin_saml.fields.lastSync')}
                      value={new Date(s.metadataRefreshedAt).toLocaleString()}
                    />
                  ) : null}
                </div>
              )}

              <div className="space-y-1.5 border-t border-border pt-3">
                <ValueRow label={t('plugin_saml.fields.spEntityId')} value={s.sp?.entityId}>
                  <CopyButton text={s.sp?.entityId ?? ''} label={t('plugin_saml.btn.copy')} />
                </ValueRow>
                <ValueRow label={t('plugin_saml.fields.acs')} value={s.sp?.acsUrl}>
                  <CopyButton text={s.sp?.acsUrl ?? ''} label={t('plugin_saml.btn.copy')} />
                </ValueRow>
                <ValueRow label={t('plugin_saml.fields.metadata')} value={s.sp?.metadataUrl}>
                  <CopyButton text={s.sp?.metadataUrl ?? ''} label={t('plugin_saml.btn.copy')} />
                  <Button
                    variant="ghost"
                    size="icon"
                    asChild
                    aria-label={t('plugin_saml.btn.downloadMetadata')}
                    title={t('plugin_saml.btn.downloadMetadata')}
                  >
                    <a href={`${s.sp?.metadataUrl ?? ''}?download=1`} target="_blank" rel="noreferrer">
                      <Download aria-hidden="true" />
                    </a>
                  </Button>
                </ValueRow>
                <ValueRow label={t('plugin_saml.fields.loginUrl')} value={s.sp?.loginUrl}>
                  <CopyButton text={s.sp?.loginUrl ?? ''} label={t('plugin_saml.btn.copyLogin')} />
                </ValueRow>
                <ValueRow label={t('plugin_saml.fields.slo')} value={s.sp?.sloUrl}>
                  <CopyButton text={s.sp?.sloUrl ?? ''} label={t('plugin_saml.btn.copy')} />
                </ValueRow>
                <div className="flex flex-wrap gap-2 pt-1">
                  <Button variant="outline" size="sm" onClick={() => setCertSource(s)}>
                    <ShieldCheck aria-hidden="true" />
                    {t('plugin_saml.btn.viewCert')}
                  </Button>
                  {s.idpMetadataUrl ? (
                    <Can perm="saml:edit">
                      <Button variant="outline" size="sm" disabled={refreshing === s.id} onClick={() => void onRefresh(s)}>
                        {refreshing === s.id ? (
                          <Loader2 className="animate-spin" aria-hidden="true" />
                        ) : (
                          <RefreshCw aria-hidden="true" />
                        )}
                        {t('plugin_saml.btn.refreshMetadata')}
                      </Button>
                    </Can>
                  ) : null}
                </div>
              </div>

              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                <span>
                  {t('plugin_saml.summary.attrs')}: {[s.attrUsername, s.attrEmail, s.attrNickname, s.attrPhone, s.attrGroups].filter(Boolean).join(' / ')}
                </span>
                <span>
                  {t('plugin_saml.summary.policy')}:{' '}
                  {[
                    s.autoCreate ? t('plugin_saml.summary.autoCreate') : null,
                    s.matchExisting ? t('plugin_saml.summary.matchExisting') : null,
                    s.defaultRole ? t('plugin_saml.summary.defaultRole', { role: s.defaultRole }) : null,
                    s.requireMappedGroup ? t('plugin_saml.summary.requireGroup') : null,
                    s.syncRolesOnLogin ? t('plugin_saml.summary.roleSync') : null,
                    s.forceAuthn ? t('plugin_saml.summary.forceAuthn') : null,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
                {s.allowIdpInitiated ? <span>{t('plugin_saml.summary.idpInitiated')}</span> : null}
                {s.requireAuthnContext && s.authnContextClassRef ? (
                  <span>{t('plugin_saml.summary.authnContext', { value: s.authnContextClassRef })}</span>
                ) : null}
                {s.requireSignedResponseForCBC || s.rejectUnknownConditions ? (
                  <span>
                    {t('plugin_saml.summary.hardening')}:{' '}
                    {[
                      s.requireSignedResponseForCBC ? t('plugin_saml.summary.signedCBC') : null,
                      s.rejectUnknownConditions ? t('plugin_saml.summary.unknownConditions') : null,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                ) : null}
              </div>
            </Card>
          ))}
        </div>
      )}

      <SourceFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} />
      <TestDialog result={testResult} onClose={() => setTestResult(null)} />
      <CertDialog source={certSource} onClose={() => setCertSource(null)} />
    </PageContainer>
  )
}
