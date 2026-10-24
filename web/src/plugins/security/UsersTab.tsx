import { useState } from 'react'
import { RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { useResetUser, useUsers, type TwoFactorUser, type UserStatus } from './api'
import { formatTime } from './format'

const VARIANT: Record<UserStatus, 'success' | 'info' | 'secondary' | 'destructive'> = {
  enabled: 'success',
  pending: 'info',
  grace: 'secondary',
  expired: 'destructive',
}

export function UsersTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { data, isLoading } = useUsers({ keyword, page, pageSize })
  const reset = useResetUser()

  const onReset = async (u: TwoFactorUser) => {
    if (!(await confirm({ destructive: true, title: t('plugin_security.users.reset'), description: t('plugin_security.users.resetConfirm', { name: u.username }) }))) return
    try {
      await reset.mutateAsync(u.userId)
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<TwoFactorUser>[] = [
    { key: 'username', title: t('plugin_security.users.username'), mobile: 'title', render: (u) => <span className="font-medium text-foreground">{u.username}</span> },
    { key: 'status', title: t('plugin_security.users.status'), mobile: 'sub', render: (u) => <Badge variant={VARIANT[u.status]}>{t(`plugin_security.users.state.${u.status}`)}</Badge> },
    { key: 'enabledAt', title: t('plugin_security.users.enabledAt'), render: (u) => <span className="num">{formatTime(u.enabledAt, i18n.language) || '-'}</span> },
    { key: 'graceUntil', title: t('plugin_security.users.graceUntil'), render: (u) => <span className="num">{u.status === 'enabled' ? '-' : formatTime(u.graceUntil, i18n.language) || '-'}</span> },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar
        onSearch={() => {
          setKeyword(draft.trim())
          setPage(1)
        }}
        onReset={() => {
          setDraft('')
          setKeyword('')
          setPage(1)
        }}
      >
        <Input className="md:w-56" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder={t('plugin_security.users.search')} aria-label={t('common.keyword')} />
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="userId"
        loading={isLoading}
        columnSettingsKey="plugin-security-users"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_security.users.empty')}
        rowActions={(u) => (
          <Can perm="security:reset">
            <Button variant="ghost" size="icon" aria-label={t('plugin_security.users.reset')} title={t('plugin_security.users.reset')} onClick={() => void onReset(u)}>
              <RotateCcw aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
    </div>
  )
}
