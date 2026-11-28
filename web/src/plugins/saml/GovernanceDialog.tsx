import { useState } from 'react'
import { Activity, History, Loader2, MonitorSmartphone, RotateCcw, ShieldX } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/framework/ui/tabs'
import {
  startConnectionTest,
  useRevokeSession,
  useRollbackSource,
  useSourceEvents,
  useSourceSessions,
  useSourceVersions,
  type SamlSource,
  type SourceForm,
  type SourceVersion,
} from './api'

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

const versionFields: (keyof SourceForm)[] = [
  'name', 'enabled', 'externalBaseUrl', 'idpMetadataUrl', 'idpEntityId', 'spEntityIdOverride',
  'attrUsername', 'attrEmail', 'attrNickname', 'attrPhone', 'attrGroups', 'attrExternalId',
  'autoCreate', 'matchExisting', 'matchProvisionedExternalId', 'defaultRole', 'groupRoleMappings',
  'requireMappedGroup', 'syncRolesOnLogin', 'revokeRolesOnEmptyGroups', 'allowIdpInitiated',
  'signAuthnRequest', 'nameIdFormat', 'requireEncryptedAssertion', 'signMetadata', 'allowSha1Signatures',
  'requireSignedResponseForCBC', 'rejectUnknownConditions', 'forceAuthn', 'authnContextClassRef',
  'requireAuthnContext', 'idpMetadata', 'metadataCaCert', 'metadataSignCert',
]

function versionChanges(current: SourceForm, previous?: SourceForm) {
  if (!previous) return []
  return versionFields.flatMap((key) => {
    if (JSON.stringify(current[key]) === JSON.stringify(previous[key])) return []
    return [{ key, before: previous[key], after: current[key] }]
  })
}

function formatVersionValue(key: keyof SourceForm, value: SourceForm[keyof SourceForm]) {
  if (key === 'idpMetadata' || key === 'metadataCaCert' || key === 'metadataSignCert') return value ? 'updated' : ''
  if (typeof value === 'boolean') return value ? 'on' : 'off'
  if (Array.isArray(value)) return value.length ? JSON.stringify(value) : ''
  return String(value ?? '')
}

function VersionDiff({ current, previous }: { current: SourceVersion; previous?: SourceVersion }) {
  const { t } = useTranslation()
  const changes = versionChanges(current.config, previous?.config)
  if (!previous) return <p className="text-xs text-muted-foreground">{t('plugin_saml.governance.firstVersion')}</p>
  if (!changes.length) return <p className="text-xs text-muted-foreground">{t('plugin_saml.governance.noChanges')}</p>
  const displayValue = (key: keyof SourceForm, value: SourceForm[keyof SourceForm]) => {
    const formatted = formatVersionValue(key, value)
    if (formatted === 'updated' || formatted === 'on' || formatted === 'off') {
      return t(`plugin_saml.governance.${formatted}`)
    }
    return formatted || '—'
  }
  return (
    <details className="rounded-md bg-muted/40 p-2 text-xs">
      <summary className="cursor-pointer text-muted-foreground">{t('plugin_saml.governance.changes', { count: changes.length })}</summary>
      <div className="mt-2 space-y-2">
        {changes.map(({ key, before, after }) => (
          <div key={key} className="grid gap-1 sm:grid-cols-[140px_1fr]">
            <span className="font-medium">{t(`plugin_saml.fields.${key}`)}</span>
            <div className="min-w-0 space-y-1">
              <p className="break-all text-muted-foreground line-through">{displayValue(key, before)}</p>
              <p className="break-all">{displayValue(key, after)}</p>
            </div>
          </div>
        ))}
      </div>
    </details>
  )
}

export default function GovernanceDialog({ source, onClose }: { source: SamlSource | null; onClose: () => void }) {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const versions = useSourceVersions(source?.id)
  const events = useSourceEvents(source?.id)
  const sessions = useSourceSessions(source?.id)
  const rollback = useRollbackSource()
  const revoke = useRevokeSession()
  const [mode, setMode] = useState<'sso' | 'slo'>('sso')
  const [nameId, setNameId] = useState('')
  const [testing, setTesting] = useState(false)

  const runTest = async () => {
    if (!source) return
    const testWindow = window.open('', '_blank')
    if (!testWindow) {
      toast.error(t('plugin_saml.governance.testPopupBlocked'))
      return
    }
    testWindow.name = `saml-test-${Date.now()}`
    setTesting(true)
    try {
      const result = await startConnectionTest(source.id, { mode, nameId: mode === 'slo' ? nameId.trim() : undefined })
      if (result.binding === 'redirect') {
        testWindow.location.href = result.url
        return
      }
      const form = document.createElement('form')
      form.method = 'POST'
      form.action = result.url
      form.target = testWindow.name
      for (const [key, value] of [
        ['SAMLRequest', result.samlRequest ?? ''],
        ['RelayState', result.relayState ?? ''],
      ]) {
        const input = document.createElement('input')
        input.type = 'hidden'
        input.name = key
        input.value = value
        form.appendChild(input)
      }
      document.body.appendChild(form)
      form.submit()
      form.remove()
    } catch (error) {
      testWindow.close()
      toast.error((error as Error).message)
    } finally {
      setTesting(false)
    }
  }

  const onRollback = async (version: number) => {
    if (!source) return
    const reason = window.prompt(t('plugin_saml.governance.rollbackReason'))
    if (reason === null) return
    if (!(await confirm({ description: t('plugin_saml.governance.rollbackConfirm', { version }) }))) return
    try {
      await rollback.mutateAsync({ id: source.id, version, reason })
      toast.success(t('common.operateSuccess'))
    } catch (error) {
      toast.error((error as Error).message)
    }
  }

  const onRevoke = async (sessionId: string) => {
    if (!source) return
    if (!(await confirm({ destructive: true, description: t('plugin_saml.governance.revokeConfirm') }))) return
    try {
      await revoke.mutateAsync({ id: source.id, sessionId })
      toast.success(t('common.operateSuccess'))
    } catch (error) {
      toast.error((error as Error).message)
    }
  }

  return (
    <ResponsiveDialog
      open={!!source}
      onOpenChange={(open) => !open && onClose()}
      title={t('plugin_saml.governance.title', { name: source?.name ?? '' })}
      size="lg"
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      <Tabs defaultValue="test">
        <TabsList className="grid w-full grid-cols-2 sm:grid-cols-4">
          <TabsTrigger value="test"><Activity className="size-4" />{t('plugin_saml.governance.test')}</TabsTrigger>
          <TabsTrigger value="versions"><History className="size-4" />{t('plugin_saml.governance.versions')}</TabsTrigger>
          <TabsTrigger value="events"><MonitorSmartphone className="size-4" />{t('plugin_saml.governance.events')}</TabsTrigger>
          <TabsTrigger value="sessions"><ShieldX className="size-4" />{t('plugin_saml.governance.sessions')}</TabsTrigger>
        </TabsList>

        <TabsContent value="test" className="space-y-4 pt-4">
          <p className="text-sm text-muted-foreground">{t('plugin_saml.governance.testHint')}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <label className="text-sm font-medium" htmlFor="saml-test-mode">{t('plugin_saml.governance.testMode')}</label>
              <Select value={mode} onValueChange={(value) => setMode(value as 'sso' | 'slo')}>
                <SelectTrigger id="saml-test-mode"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="sso">{t('plugin_saml.governance.sso')}</SelectItem>
                  <SelectItem value="slo">{t('plugin_saml.governance.slo')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {mode === 'slo' ? (
              <div className="space-y-1.5">
                <label className="text-sm font-medium" htmlFor="saml-test-nameid">{t('plugin_saml.governance.nameId')}</label>
                <Input id="saml-test-nameid" value={nameId} onChange={(event) => setNameId(event.target.value)} />
              </div>
            ) : null}
          </div>
          <Can perm="saml:edit">
            <Button type="button" onClick={() => void runTest()} disabled={testing || (mode === 'slo' && !nameId.trim())}>
              {testing ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Activity aria-hidden="true" />}
              {t('plugin_saml.governance.startTest')}
            </Button>
          </Can>
        </TabsContent>

        <TabsContent value="versions" className="space-y-3 pt-4">
          {versions.isLoading ? <p className="text-sm text-muted-foreground">{t('common.loading')}</p> : null}
          {versions.data?.length === 0 ? <p className="text-sm text-muted-foreground">{t('plugin_saml.governance.noVersions')}</p> : null}
          {versions.data?.map((item, index) => (
            <div key={item.version} className="space-y-2 rounded-md border border-border p-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="secondary">v{item.version}</Badge>
                <span className="text-sm font-medium">{item.config.name}</span>
                <Badge variant={item.config.enabled ? 'success' : 'secondary'}>{item.config.enabled ? t('plugin_saml.on') : t('plugin_saml.off')}</Badge>
                <span className="text-xs text-muted-foreground">{formatTime(item.createdAt)}</span>
                <Can perm="saml:edit">
                  <Button type="button" variant="outline" size="sm" className="ms-auto" onClick={() => void onRollback(item.version)}>
                    <RotateCcw aria-hidden="true" />{t('plugin_saml.governance.rollback')}
                  </Button>
                </Can>
              </div>
              <p className="break-all text-xs text-muted-foreground">{item.operatorName || item.operatorId} · {item.reason || '—'}</p>
              {item.config.externalBaseUrl ? <p className="break-all font-mono text-xs">{item.config.externalBaseUrl}</p> : null}
              <VersionDiff current={item} previous={versions.data?.[index + 1]} />
            </div>
          ))}
        </TabsContent>

        <TabsContent value="events" className="space-y-2 pt-4">
          {events.isLoading ? <p className="text-sm text-muted-foreground">{t('common.loading')}</p> : null}
          {events.data?.length === 0 ? <p className="text-sm text-muted-foreground">{t('plugin_saml.governance.noEvents')}</p> : null}
          {events.data?.map((event) => (
            <div key={event.id} className="space-y-1 rounded-md border border-border p-3 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={event.result === 'failed' ? 'destructive' : event.result === 'success' ? 'success' : 'secondary'}>{event.result}</Badge>
                <span className="font-mono text-xs">{event.type}</span>
                <span className="text-xs text-muted-foreground">{formatTime(event.createdAt)}</span>
              </div>
              {event.detail ? <p className="break-all text-xs text-muted-foreground">{event.detail}</p> : null}
              <p className="break-all text-xs text-muted-foreground">
                {[event.operatorName, event.userId, event.correlationId, event.ip].filter(Boolean).join(' · ') || '—'}
              </p>
            </div>
          ))}
        </TabsContent>

        <TabsContent value="sessions" className="space-y-2 pt-4">
          {sessions.isLoading ? <p className="text-sm text-muted-foreground">{t('common.loading')}</p> : null}
          {sessions.data?.length === 0 ? <p className="text-sm text-muted-foreground">{t('plugin_saml.governance.noSessions')}</p> : null}
          {sessions.data?.map((session) => (
            <div key={session.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border p-3 text-sm">
              <div className="min-w-0 flex-1">
                <p className="font-mono text-xs">{t('plugin_saml.governance.sessionUser', { id: session.userId })}</p>
                <p className="break-all text-xs text-muted-foreground">{session.sessionIndex || '—'} · {formatTime(session.lastSeenAt)}</p>
                <p className="text-xs text-muted-foreground">{t('plugin_saml.governance.expires')}: {formatTime(session.expiresAt)}</p>
              </div>
              {session.revokedAt ? <Badge variant="secondary">{t('plugin_saml.governance.revoked')}</Badge> : (
                <Can perm="saml:edit">
                  <Button type="button" variant="outline" size="sm" onClick={() => void onRevoke(session.id)}>
                    <ShieldX aria-hidden="true" />{t('plugin_saml.governance.revoke')}
                  </Button>
                </Can>
              )}
            </div>
          ))}
        </TabsContent>
      </Tabs>
    </ResponsiveDialog>
  )
}
