import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Alert, AlertDescription } from '@/framework/ui/alert'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Switch } from '@/framework/ui/switch'
import { useDeleteRule, useRules, useSetRuleEnabled, type IpRule } from './api'
import { RuleFormDialog } from './RuleFormDialog'

export function IpRulesTab() {
  const { t } = useTranslation()
  const confirm = useConfirm()
  const { data, isLoading } = useRules()
  const setEnabled = useSetRuleEnabled()
  const remove = useDeleteRule()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<IpRule | null>(null)

  const attempt = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }
  const openForm = (r: IpRule | null) => {
    setEditing(r)
    setFormOpen(true)
  }
  const onDelete = async (r: IpRule) => {
    if (await confirm({ destructive: true, description: t('plugin_security.ip.deleteConfirm', { cidr: r.cidr }) })) {
      await attempt(() => remove.mutateAsync(r.id))
    }
  }

  const columns: Column<IpRule>[] = [
    {
      key: 'action',
      title: t('plugin_security.ip.action'),
      render: (r) => <Badge variant={r.action === 'deny' ? 'destructive' : 'success'}>{t(`plugin_security.ip.actions.${r.action}`)}</Badge>,
    },
    { key: 'cidr', title: t('plugin_security.ip.cidr'), mobile: 'title', render: (r) => <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{r.cidr}</code> },
    { key: 'scope', title: t('plugin_security.ip.scope'), mobile: 'sub', render: (r) => t(`plugin_security.ip.scopes.${r.scope}`) },
    { key: 'remark', title: t('plugin_security.ip.remark'), render: (r) => r.remark || <span className="text-muted-foreground">-</span> },
    {
      key: 'enabled',
      title: t('plugin_security.ip.enabled'),
      render: (r) => (
        <Can perm="security:ip:edit" fallback={<span>{r.enabled ? t('plugin_security.ip.on') : t('plugin_security.ip.off')}</span>}>
          <Switch checked={r.enabled} aria-label={t('plugin_security.ip.enabled')} onCheckedChange={(v) => void attempt(() => setEnabled.mutateAsync({ id: r.id, enabled: v }))} />
        </Can>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <Alert>
        <AlertDescription>
          {t('plugin_security.ip.hint')} {data?.clientIp ? t('plugin_security.ip.yourIp', { ip: data.clientIp }) : null}
        </AlertDescription>
      </Alert>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-security-ip"
        emptyText={t('plugin_security.ip.empty')}
        toolbar={
          <Can perm="security:ip:edit">
            <Button onClick={() => openForm(null)}>
              <Plus aria-hidden="true" />
              {t('plugin_security.ip.create')}
            </Button>
          </Can>
        }
        rowActions={(r) => (
          <Can perm="security:ip:edit">
            <Button variant="ghost" size="icon" aria-label={t('common.edit')} title={t('common.edit')} onClick={() => openForm(r)}>
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} title={t('common.delete')} onClick={() => void onDelete(r)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      <RuleFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} />
    </div>
  )
}
