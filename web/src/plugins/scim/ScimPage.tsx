import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { DataTable } from '@/framework/components/DataTable'
import { usePermission } from '@/framework/auth/usePermission'
import { useCreateToken, useRevokeToken, useTokens } from './api'

export default function ScimPage() {
  const { t } = useTranslation()
  // 建发/撤销走 scim:token（与 list 分开授权）：没授权就收掉按钮，而不是点了吃 403
  const { has } = usePermission()
  const confirm = useConfirm()
  const canManage = has('scim:token')
  const q = useTokens()
  const create = useCreateToken()
  const revoke = useRevokeToken()
  const [name, setName] = useState('')
  const [newToken, setNewToken] = useState('')
  const rows = q.data?.list ?? []

  // 撤销是危险操作（现有集成立刻失效）：与全站危险操作同一套 useConfirm 确认
  const revokeToken = async (id: string, tokenName: string) => {
    if (
      !(await confirm({
        destructive: true,
        description: t('plugin_scim.revokeConfirm', { name: tokenName }),
      }))
    ) {
      return
    }
    await revoke.mutateAsync(id)
    toast.success(t('common.operateSuccess'))
  }

  return (
    <PageContainer>
      <PageHeader title={t('plugin_scim.title')} description={t('plugin_scim.description')} />
      {canManage ? (
        <div className="flex flex-wrap items-center gap-2">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('plugin_scim.tokenName')} className="max-w-xs" />
          <Button
            disabled={!name.trim() || create.isPending}
            onClick={() =>
              create.mutate(name.trim(), {
                onSuccess: (v) => {
                  setNewToken(v.token)
                  setName('')
                  toast.success(t('common.operateSuccess'))
                },
              })
            }
          >
            {create.isPending ? t('common.loading') : t('plugin_scim.createToken')}
          </Button>
          {newToken && <code className="max-w-full break-all rounded bg-muted px-2 py-1 text-sm">{newToken}</code>}
        </div>
      ) : null}
      <DataTable
        data={rows}
        loading={q.isLoading}
        isError={q.isError}
        onRetry={() => void q.refetch()}
        retrying={q.isFetching}
        total={q.data?.total}
        columnSettingsKey="plugin-scim-tokens"
        emptyText={t('plugin_scim.empty')}
        rowKey="id"
        columns={[
          { key: 'name', title: t('plugin_scim.columns.name') },
          { key: 'createdAt', title: t('plugin_scim.columns.createdAt') },
          { key: 'lastUsed', title: t('plugin_scim.columns.lastUsed') },
          { key: 'revoked', title: t('plugin_scim.columns.revoked') },
          { key: 'actions', title: t('plugin_scim.columns.actions'), render: (row) => (
            canManage ? (
              <Button
                variant="outline"
                size="sm"
                disabled={row.revoked || revoke.isPending}
                onClick={() => void revokeToken(row.id, row.name)}
              >
                {t('plugin_scim.revoke')}
              </Button>
            ) : null
          ) },
        ]}
      />
    </PageContainer>
  )
}
