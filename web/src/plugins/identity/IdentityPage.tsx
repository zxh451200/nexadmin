import { useState } from 'react'
import { Copy, KeyRound, Pencil, Plug, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Switch } from '@/framework/ui/switch'
import {
  useDeleteSource,
  useSetEnabled,
  useSources,
  useTestConnection,
  type CasConfig,
  type IdentitySource,
  type LdapConfig,
} from './api'
import { SourceFormDialog } from './SourceFormDialog'
import { TestLoginDialog } from './TestLoginDialog'
import { DirectorySync } from './DirectorySync'

function address(s: IdentitySource) {
  if (!s.config) return '-'
  return s.type === 'ldap' ? (s.config as LdapConfig).url : (s.config as CasConfig).serverUrl
}

export default function IdentityPage() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data = [], isLoading } = useSources()
  const setEnabled = useSetEnabled()
  const remove = useDeleteSource()
  const testConn = useTestConnection()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<IdentitySource | null>(null)
  const [loginTest, setLoginTest] = useState<IdentitySource | null>(null)

  const attempt = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const openForm = (s: IdentitySource | null) => {
    setEditing(s)
    setFormOpen(true)
  }
  const onDelete = async (s: IdentitySource) => {
    if (await confirm({ destructive: true, description: t('plugin_identity.deleteConfirm', { name: s.name }) })) {
      await attempt(() => remove.mutateAsync(s.id))
    }
  }
  const onTestConnection = async (s: IdentitySource) => {
    try {
      const r = await testConn.mutateAsync(s.id)
      if (r.ok) toast.success(t('plugin_identity.connectionOk'))
      else toast.error(`${t(`plugin_identity.stage.${r.stage ?? 'connect'}`)}: ${r.message ?? ''}`)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(t('plugin_identity.copied'))
    } catch {
      toast.error(t('plugin_identity.copyFailed'))
    }
  }

  const columns: Column<IdentitySource>[] = [
    { key: 'name', title: t('plugin_identity.fields.name'), mobile: 'title', render: (s) => <span className="font-medium text-foreground">{s.name}</span> },
    { key: 'type', title: t('plugin_identity.fields.type'), mobile: 'sub', render: (s) => <Badge variant="outline">{t(`plugin_identity.type.${s.type}`)}</Badge> },
    { key: 'address', title: t('plugin_identity.fields.address'), render: (s) => <code className="break-all rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{address(s)}</code> },
    {
      key: 'directorySync',
      title: t('plugin_identity.sync.title'),
      render: (s) => s.type === 'ldap' ? <Can perm="identity:sync"><DirectorySync source={s} /></Can> : '—',
    },
    {
      key: 'enabled',
      title: t('plugin_identity.fields.enabled'),
      render: (s) => (
        <Can perm="identity:edit" fallback={<span>{s.enabled ? t('plugin_identity.on') : t('plugin_identity.off')}</span>}>
          <Switch
            checked={s.enabled}
            aria-label={t('plugin_identity.fields.enabled')}
            onCheckedChange={(v) => void attempt(() => setEnabled.mutateAsync({ id: s.id, enabled: v }))}
          />
        </Can>
      ),
    },
    {
      key: 'entry',
      title: t('plugin_identity.fields.entry'),
      render: (s) =>
        s.type === 'cas' && s.entryUrl ? (
          <div className="flex min-w-0 items-center gap-1">
            <code className="min-w-0 break-all rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{s.entryUrl}</code>
            <Button variant="ghost" size="icon" aria-label={t('plugin_identity.copyEntry')} title={t('plugin_identity.copyEntry')} onClick={() => void copy(s.entryUrl ?? '')}>
              <Copy aria-hidden="true" />
            </Button>
          </div>
        ) : (
          <span className="text-muted-foreground">{s.type === 'ldap' ? t('plugin_identity.entryLdap') : '-'}</span>
        ),
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_identity.title')} description={t('plugin_identity.description')} />
      <DataTable
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-identity-sources"
        pagination={false}
        emptyText={t('plugin_identity.empty')}
        toolbar={
          <Can perm="identity:edit">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_identity.create')}
            </Button>
          </Can>
        }
        rowActions={(s) => (
          <>
            <Can perm="identity:test">
              <Button variant="ghost" size="icon" aria-label={t('plugin_identity.testConnection')} title={t('plugin_identity.testConnection')} onClick={() => void onTestConnection(s)}>
                <Plug aria-hidden="true" />
              </Button>
              {s.type === 'ldap' ? (
                <Button variant="ghost" size="icon" aria-label={t('plugin_identity.testLogin')} title={t('plugin_identity.testLogin')} onClick={() => setLoginTest(s)}>
                  <KeyRound aria-hidden="true" />
                </Button>
              ) : null}
            </Can>
            <Can perm="identity:edit">
              <Button variant="ghost" size="icon" aria-label={t('common.edit')} onClick={() => openForm(s)}>
                <Pencil aria-hidden="true" />
              </Button>
              <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(s)}>
                <Trash2 aria-hidden="true" />
              </Button>
            </Can>
          </>
        )}
      />
      <SourceFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} />
      <TestLoginDialog source={loginTest} onClose={() => setLoginTest(null)} />
    </PageContainer>
  )
}
