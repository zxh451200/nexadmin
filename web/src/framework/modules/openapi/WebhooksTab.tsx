import { useState } from 'react'
import { KeyRound, Pencil, Plus, Send, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Switch } from '@/framework/ui/switch'
import { fmtDateTime } from '../oper-log/format'
import {
  useDeleteWebhook,
  useResetWebhookSecret,
  useSetWebhookEnabled,
  useTestWebhook,
  useWebhooks,
  type Webhook,
  type WebhookTestResult,
} from './api'
import { RevealDialog } from './RevealDialog'
import { WebhookFormDialog } from './WebhookFormDialog'

interface SecretTarget {
  name: string
  secret: string
}

export function WebhooksTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { has } = usePermission()
  const canEdit = has('openapi:edit')
  const { data, isLoading } = useWebhooks()
  const setEnabled = useSetWebhookEnabled()
  const remove = useDeleteWebhook()
  const reset = useResetWebhookSecret()
  const test = useTestWebhook()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Webhook | null>(null)
  const [secret, setSecret] = useState<SecretTarget | null>(null)
  const [testing, setTesting] = useState<{ name: string; result: WebhookTestResult } | null>(null)
  const [testingId, setTestingId] = useState<string | null>(null)

  const openForm = (w: Webhook | null) => {
    setEditing(w)
    setFormOpen(true)
  }

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onDelete = async (w: Webhook) => {
    if (await confirm({ destructive: true, title: t('common.delete'), description: t('openapi.webhooks.deleteConfirm', { name: w.name }) })) {
      await run(() => remove.mutateAsync(w.id))
    }
  }

  const onReset = async (w: Webhook) => {
    if (!(await confirm({ destructive: true, title: t('openapi.webhooks.resetSecret'), description: t('openapi.webhooks.resetConfirm', { name: w.name }) }))) return
    try {
      const res = await reset.mutateAsync(w.id)
      setSecret({ name: w.name, secret: res.secret })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onTest = async (w: Webhook) => {
    setTestingId(w.id)
    try {
      setTesting({ name: w.name, result: await test.mutateAsync(w.id) })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    } finally {
      setTestingId(null)
    }
  }

  const columns: Column<Webhook>[] = [
    {
      key: 'name',
      title: t('openapi.webhooks.fields.name'),
      mobile: 'title',
      render: (w) => <span className="font-medium text-foreground">{w.name}</span>,
    },
    {
      key: 'url',
      title: t('openapi.webhooks.fields.url'),
      mobile: 'sub',
      render: (w) => <code className="break-all font-mono text-xs">{w.url}</code>,
    },
    {
      key: 'events',
      title: t('openapi.webhooks.fields.events'),
      render: (w) => (
        <div className="flex flex-wrap gap-1">
          {w.events.slice(0, 3).map((e) => (
            <Badge key={e} variant="secondary" className="font-mono">
              {e}
            </Badge>
          ))}
          {w.events.length > 3 ? <Badge variant="outline">+{w.events.length - 3}</Badge> : null}
        </div>
      ),
    },
    {
      key: 'enabled',
      title: t('openapi.webhooks.fields.enabled'),
      render: (w) => (
        <span className="inline-flex min-h-11 items-center md:min-h-0">
          <Switch
            checked={w.enabled}
            disabled={!canEdit || setEnabled.isPending}
            aria-label={`${t('openapi.webhooks.fields.enabled')}: ${w.name}`}
            onCheckedChange={(on) => run(() => setEnabled.mutateAsync({ id: w.id, enabled: on }))}
          />
        </span>
      ),
    },
    {
      key: 'createdAt',
      title: t('openapi.webhooks.fields.createdAt'),
      render: (w) => <span className="num">{fmtDateTime(w.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <>
      <DataTable
        columns={columns}
        columnSettingsKey="openapiWebhooks"
        data={data ?? []}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        emptyText={t('openapi.webhooks.empty')}
        toolbar={
          <Can perm="openapi:edit">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('openapi.webhooks.create')}
            </Button>
          </Can>
        }
        rowActions={(w) => (
          <Can perm="openapi:edit">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('openapi.webhooks.test')}
              disabled={testingId === w.id}
              onClick={() => void onTest(w)}
            >
              <Send aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(w)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" aria-label={t('openapi.webhooks.resetSecret')} onClick={() => onReset(w)}>
              <KeyRound aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => onDelete(w)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      <WebhookFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
        onCreated={(w) => setSecret({ name: w.name, secret: w.secret })}
      />
      <RevealDialog
        open={!!secret}
        onOpenChange={(o) => !o && setSecret(null)}
        title={t('openapi.webhooks.secret.title')}
        description={secret?.name}
        warning={t('openapi.webhooks.secret.onceHint')}
        closeText={t('openapi.webhooks.secret.saved')}
        fields={[{ label: t('openapi.webhooks.fields.secret'), value: secret?.secret ?? '' }]}
      >
        <p className="text-xs text-muted-foreground">{t('openapi.webhooks.secret.usage')}</p>
      </RevealDialog>
      <ResponsiveDialog
        open={!!testing}
        onOpenChange={(o) => !o && setTesting(null)}
        size="sm"
        title={t('openapi.webhooks.testResult.title')}
        description={testing?.name}
        footer={
          <Button type="button" onClick={() => setTesting(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {testing ? (
          <dl className="flex flex-col gap-3 text-sm">
            <div className="flex items-center justify-between gap-3">
              <dt className="text-muted-foreground">{t('common.status')}</dt>
              <dd>
                <Badge variant={testing.result.success ? 'success' : 'destructive'}>
                  {testing.result.success ? t('openapi.webhooks.testResult.success') : t('openapi.webhooks.testResult.failed')}
                </Badge>
              </dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="text-muted-foreground">{t('openapi.webhooks.testResult.statusCode')}</dt>
              <dd className="num font-mono">{testing.result.statusCode || '-'}</dd>
            </div>
            <div className="flex items-center justify-between gap-3">
              <dt className="text-muted-foreground">{t('openapi.webhooks.testResult.duration')}</dt>
              <dd className="num font-mono">{t('openapi.webhooks.testResult.ms', { ms: testing.result.durationMs })}</dd>
            </div>
            {testing.result.error ? (
              <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs break-all text-destructive">
                {testing.result.error}
              </div>
            ) : null}
          </dl>
        ) : null}
      </ResponsiveDialog>
    </>
  )
}
