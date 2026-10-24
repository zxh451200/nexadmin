import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Info, KeyRound, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { api } from '@/framework/request/http'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Switch } from '@/framework/ui/switch'
import { fmtDateTime } from '../oper-log/format'
import { CopyButton } from '../sso/CopyButton'
import { AppFormDialog } from './AppFormDialog'
import { AppSecretDialog, type AppSecretTarget } from './AppSecretDialog'
import { useDeleteOpenApp, useOpenApps, useResetOpenAppSecret, useSetOpenAppEnabled, type OpenApp } from './api'

export function AppsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { has } = usePermission()
  const canEdit = has('openapi:edit')
  const { data, isLoading } = useOpenApps()
  const setEnabled = useSetOpenAppEnabled()
  const remove = useDeleteOpenApp()
  const reset = useResetOpenAppSecret()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<OpenApp | null>(null)
  const [secret, setSecret] = useState<AppSecretTarget | null>(null)

  const { data: roles } = useQuery({
    queryKey: ['openapi', 'role-options'],
    queryFn: () => api.get<{ id: string; name: string }[]>('/roles/options', undefined, { silent: true }),
  })
  const roleName = useMemo(() => new Map((roles ?? []).map((r) => [r.id, r.name])), [roles])

  const openForm = (a: OpenApp | null) => {
    setEditing(a)
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

  const onDelete = async (a: OpenApp) => {
    if (await confirm({ destructive: true, title: t('common.delete'), description: t('openapi.apps.deleteConfirm', { name: a.name }) })) {
      await run(() => remove.mutateAsync(a.id))
    }
  }

  const onReset = async (a: OpenApp) => {
    if (!(await confirm({ destructive: true, title: t('openapi.apps.resetSecret'), description: t('openapi.apps.resetConfirm', { name: a.name }) }))) return
    try {
      const res = await reset.mutateAsync(a.id)
      setSecret({ app: a, appSecret: res.appSecret })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<OpenApp>[] = [
    {
      key: 'name',
      title: t('openapi.apps.fields.name'),
      mobile: 'title',
      render: (a) => <span className="font-medium text-foreground">{a.name}</span>,
    },
    {
      key: 'appKey',
      title: t('openapi.apps.fields.appKey'),
      mobile: 'sub',
      render: (a) => (
        <span className="inline-flex items-center gap-1">
          <code className="font-mono text-xs">{a.appKey}</code>
          <CopyButton text={a.appKey} label={`${t('sso.copy')} ${t('openapi.apps.fields.appKey')}`} />
        </span>
      ),
    },
    {
      key: 'enabled',
      title: t('openapi.apps.fields.enabled'),
      render: (a) => (
        <span className="inline-flex min-h-11 items-center md:min-h-0">
          <Switch
            checked={a.enabled}
            disabled={!canEdit || setEnabled.isPending}
            aria-label={`${t('openapi.apps.fields.enabled')}: ${a.name}`}
            onCheckedChange={(on) => run(() => setEnabled.mutateAsync({ id: a.id, enabled: on }))}
          />
        </span>
      ),
    },
    {
      key: 'roles',
      title: t('openapi.apps.fields.roles'),
      render: (a) =>
        a.roleIds.length === 0 ? (
          <span className="text-muted-foreground">-</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {a.roleIds.map((id) => (
              <Badge key={id} variant="secondary">
                {roleName.get(id) ?? id}
              </Badge>
            ))}
          </div>
        ),
    },
    {
      key: 'ipAllow',
      title: t('openapi.apps.fields.ipAllow'),
      render: (a) =>
        a.ipAllow.length === 0 ? (
          <Badge variant="outline">{t('openapi.apps.ipUnlimited')}</Badge>
        ) : (
          <span className="num">{t('openapi.apps.ipCount', { count: a.ipAllow.length })}</span>
        ),
    },
    {
      key: 'lastUsedAt',
      title: t('openapi.apps.fields.lastUsedAt'),
      render: (a) => (a.lastUsedAt ? <span className="num">{fmtDateTime(a.lastUsedAt, i18n.language)}</span> : <span className="text-muted-foreground">{t('openapi.neverUsed')}</span>),
    },
    {
      key: 'createdAt',
      title: t('openapi.apps.fields.createdAt'),
      render: (a) => <span className="num">{fmtDateTime(a.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <>
      <DataTable
        columns={columns}
        columnSettingsKey="openapiApps"
        data={data ?? []}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        emptyText={t('openapi.apps.empty')}
        toolbar={
          <Can perm="openapi:edit">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('openapi.apps.create')}
            </Button>
          </Can>
        }
        rowActions={(a) => (
          <>
            <Button variant="ghost" size="icon" aria-label={t('openapi.apps.guide.action')} onClick={() => setSecret({ app: a })}>
              <Info aria-hidden="true" />
            </Button>
            <Can perm="openapi:edit">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(a)}>
                <Pencil aria-hidden="true" />
              </Button>
              <Button variant="ghost" size="icon" aria-label={t('openapi.apps.resetSecret')} onClick={() => onReset(a)}>
                <KeyRound aria-hidden="true" />
              </Button>
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => onDelete(a)}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />
      <AppFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
        onCreated={(app) => setSecret({ app, appSecret: app.appSecret })}
      />
      <AppSecretDialog target={secret} onOpenChange={(o) => !o && setSecret(null)} />
    </>
  )
}
