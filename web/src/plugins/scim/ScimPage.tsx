import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { DataTable } from '@/framework/components/DataTable'
import { useCreateToken, useRevokeToken, useTokens } from './api'

export default function ScimPage() {
  const { t } = useTranslation()
  const q = useTokens()
  const create = useCreateToken()
  const revoke = useRevokeToken()
  const [name, setName] = useState('')
  const [newToken, setNewToken] = useState('')
  const rows = q.data?.list ?? []
  return (
    <PageContainer>
      <PageHeader title={t('plugin_scim.title')} description={t('plugin_scim.description')} />
      <div className="flex flex-wrap items-center gap-2">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('plugin_scim.tokenName')} className="max-w-xs" />
        <Button disabled={!name.trim() || create.isPending} onClick={() => create.mutate(name.trim(), { onSuccess: (v) => { setNewToken(v.token); setName('') } })}>
          {t('plugin_scim.createToken')}
        </Button>
        {newToken && <code className="max-w-full break-all rounded bg-muted px-2 py-1 text-sm">{newToken}</code>}
      </div>
      <DataTable
        data={rows}
        rowKey="id"
        columns={[
          { key: 'name', title: t('plugin_scim.columns.name') },
          { key: 'createdAt', title: t('plugin_scim.columns.createdAt') },
          { key: 'lastUsed', title: t('plugin_scim.columns.lastUsed') },
          { key: 'revoked', title: t('plugin_scim.columns.revoked') },
          { key: 'actions', title: t('plugin_scim.columns.actions'), render: (row) => (
            <Button variant="outline" size="sm" disabled={row.revoked} onClick={() => revoke.mutate(row.id)}>
              {t('plugin_scim.revoke')}
            </Button>
          ) },
        ]}
      />
    </PageContainer>
  )
}
