import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Info, KeyRound, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { usePermission } from '@/framework/auth/usePermission'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { ExportCsvButton } from '@/framework/components/export/ExportCsvButton'
import { api } from '@/framework/request/http'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Switch } from '@/framework/ui/switch'
import { fetchSsoAppsForExport, useDeleteSsoApp, useDeleteSsoApps, useResetSsoSecret, useSetSsoAppEnabled, useSsoApps, type SsoApp } from './api'
import { CopyButton } from './CopyButton'
import { KeyRotationDialog } from './KeyRotationDialog'
import { SecretDialog, type SecretTarget } from './SecretDialog'
import { SsoAppFormDialog } from './SsoAppFormDialog'

/** 单点登录应用：作为「系统设置 → 安全与登录」的一个分区渲染，旧地址 /system/sso-apps 重定向到 ?tab=sso。 */
export function SsoSection() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { has } = usePermission()
  const canEdit = has('sso:edit')
  const { data, isLoading } = useSsoApps()
  const setEnabled = useSetSsoAppEnabled()
  const remove = useDeleteSsoApp()
  const removeBatch = useDeleteSsoApps()
  const reset = useResetSsoSecret()
  const [selected, setSelected] = useState<string[]>([])
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<SsoApp | null>(null)
  const [secret, setSecret] = useState<SecretTarget | null>(null)
  const [keysOpen, setKeysOpen] = useState(false)

  const { data: roles } = useQuery({
    queryKey: ['sso', 'role-options'],
    queryFn: () => api.get<{ id: string; name: string }[]>('/roles/options', undefined, { silent: true }),
  })
  const roleName = useMemo(() => new Map((roles ?? []).map((r) => [r.id, r.name])), [roles])

  const openForm = (a: SsoApp | null) => {
    setEditing(a)
    setFormOpen(true)
  }

  const run = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onDelete = async (a: SsoApp) => {
    if (await confirm({ destructive: true, title: t('common.delete'), description: t('sso.deleteConfirm', { name: a.name }) })) {
      await run(() => remove.mutateAsync(a.id))
      setSelected((s) => s.filter((x) => x !== a.id))
    }
  }

  // 批量删除：一次提交选中的 id，后端在同一事务里连各应用的 refresh_token 一起清掉
  const onDeleteBatch = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, title: t('common.delete'), description: t('sso.deleteBatchConfirm', { count: ids.length }) }))) return
    try {
      await removeBatch.mutateAsync(ids)
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const onReset = async (a: SsoApp) => {
    if (!(await confirm({ destructive: true, title: t('sso.resetSecret'), description: t('sso.resetConfirm', { name: a.name }) }))) return
    try {
      const res = await reset.mutateAsync(a.id)
      setSecret({ name: a.name, clientId: a.clientId, clientSecret: res.clientSecret, orgRead: a.allowOrgRead })
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<SsoApp>[] = [
    {
      key: 'name',
      title: t('sso.fields.name'),
      mobile: 'title',
      render: (a) => <span className="font-medium text-foreground">{a.name}</span>,
    },
    {
      key: 'clientId',
      title: t('sso.fields.clientId'),
      mobile: 'sub',
      render: (a) => (
        <span className="inline-flex items-center gap-1">
          <code className="font-mono text-xs">{a.clientId}</code>
          <CopyButton text={a.clientId} label={`${t('sso.copy')} ${t('sso.fields.clientId')}`} />
        </span>
      ),
    },
    {
      key: 'enabled',
      title: t('sso.fields.enabled'),
      render: (a) => (
        <span className="inline-flex min-h-11 items-center md:min-h-0">
          <Switch
            checked={a.enabled}
            disabled={!canEdit || setEnabled.isPending}
            aria-label={`${t('sso.fields.enabled')}: ${a.name}`}
            onCheckedChange={(on) => run(() => setEnabled.mutateAsync({ id: a.id, enabled: on }))}
          />
        </span>
      ),
    },
    {
      key: 'roles',
      title: t('sso.fields.roles'),
      render: (a) =>
        a.roleIds.length === 0 ? (
          <Badge variant="outline">{t('sso.allUsers')}</Badge>
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
      key: 'redirectCount',
      title: t('sso.fields.redirectCount'),
      render: (a) => <span className="num">{a.redirectUris.length}</span>,
    },
  ]

  return (
    <>
      <DataTable
        toolbar={
          <>
            <Can perm="sso:edit">
              <Button onClick={() => openForm(null)}>
                <Plus aria-hidden="true" />
                {t('sso.create')}
              </Button>
            </Can>
            <Button variant="outline" onClick={() => setKeysOpen(true)}>
              <KeyRound aria-hidden="true" />
              {t('sso.keys.entry')}
            </Button>
            <ExportCsvButton
              filename={t('sso.exportFilename')}
              perm="sso:list"
              getRows={() => fetchSsoAppsForExport()}
              columns={[
                { header: t('sso.fields.name'), value: (a) => a.name },
                { header: t('sso.fields.clientId'), value: (a) => a.clientId },
                { header: t('sso.fields.enabled'), value: (a) => (a.enabled ? t('common.enable') : t('common.disable')) },
                {
                  header: t('sso.fields.roles'),
                  value: (a) => (a.roleIds.length === 0 ? t('sso.allUsers') : a.roleIds.map((id) => roleName.get(id) ?? id).join(' | ')),
                },
                { header: t('sso.fields.redirectUris'), value: (a) => a.redirectUris.join(' | ') },
                { header: t('sso.fields.homeUrl'), value: (a) => a.homeUrl },
                { header: t('sso.fields.allowOrgRead'), value: (a) => (a.allowOrgRead ? t('common.yes') : t('common.no')) },
                { header: t('sso.fields.description'), value: (a) => a.description },
                { header: t('common.createdAt'), value: (a) => (a.createdAt ? new Date(a.createdAt).toLocaleString(i18n.language) : '') },
              ]}
            />
          </>
        }
        columns={columns}
        columnSettingsKey="ssoApps"
        data={data ?? []}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        emptyText={t('sso.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="sso:edit">
            <Button variant="destructive" size="sm" onClick={() => void onDeleteBatch(ids)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(a) => (
          <>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('sso.access.action')}
              onClick={() => setSecret({ name: a.name, clientId: a.clientId, orgRead: a.allowOrgRead })}
            >
              <Info aria-hidden="true" />
            </Button>
            <Can perm="sso:edit">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(a)}>
                <Pencil aria-hidden="true" />
              </Button>
              <Button variant="ghost" size="icon" aria-label={t('sso.resetSecret')} onClick={() => onReset(a)}>
                <KeyRound aria-hidden="true" />
              </Button>
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => onDelete(a)}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />

      <SsoAppFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        editing={editing}
        onCreated={(app) => setSecret({ name: app.name, clientId: app.clientId, clientSecret: app.clientSecret, orgRead: app.allowOrgRead })}
      />
      <SecretDialog target={secret} onOpenChange={(o) => !o && setSecret(null)} />
      <KeyRotationDialog open={keysOpen} onOpenChange={setKeysOpen} />
    </>
  )
}
