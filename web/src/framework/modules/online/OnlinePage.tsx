import { useState } from 'react'
import { LogOut, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { fmtDateTime } from '../oper-log/format'
import { useForceLogout, useOnlineList, type OnlineUser } from './api'

export default function OnlinePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const { data, isLoading, isFetching, refetch } = useOnlineList({ page, pageSize, keyword: keyword || undefined })
  const force = useForceLogout()

  const onForce = async (row: OnlineUser) => {
    const name = row.nickname || row.username
    if (!(await confirm({ destructive: true, description: t('online.confirm.forceLogout', { name }) }))) return
    try {
      await force.mutateAsync(row.id)
      toast.success(t('online.forced'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const time = (iso: string) => <span className="num">{fmtDateTime(iso, i18n.language)}</span>

  const columns: Column<OnlineUser>[] = [
    { key: 'username', title: t('online.col.username'), mobile: 'title' },
    { key: 'nickname', title: t('online.col.nickname'), mobile: 'sub' },
    { key: 'deptName', title: t('online.col.dept') },
    { key: 'device', title: t('online.col.device'), render: (r) => <DictTag dict="online_device" value={r.device} /> },
    {
      key: 'client',
      title: t('online.col.client'),
      render: (r) => <span className="text-sm text-foreground">{[r.browser, r.os].filter(Boolean).join(' · ')}</span>,
    },
    { key: 'ip', title: t('online.col.ip'), render: (r) => <span className="num">{r.ip}</span> },
    { key: 'loginAt', title: t('online.col.loginAt'), render: (r) => time(r.loginAt) },
    { key: 'lastActiveAt', title: t('online.col.lastActive'), render: (r) => time(r.lastActiveAt) },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('online.menu')}
        description={t('online.description')}
        actions={
          <Button type="button" variant="outline" disabled={isFetching} onClick={() => refetch()}>
            <RefreshCw className="me-2 size-4" aria-hidden="true" />
            {t('common.refresh')}
          </Button>
        }
      />
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
        <div className="flex w-full flex-col gap-1.5 md:w-64">
          <Label htmlFor="ou-keyword">{t('online.filter.keyword')}</Label>
          <Input
            id="ou-keyword"
            value={draft}
            placeholder={t('online.filter.keywordPlaceholder')}
            onChange={(e) => setDraft(e.target.value)}
          />
        </div>
      </FilterBar>
      <DataTable<OnlineUser>
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="online"
        rowActions={(row) => (
          <Can perm="online:forceLogout">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('online.action.forceLogout')}
              onClick={() => onForce(row)}
            >
              <LogOut className="size-4 text-destructive" aria-hidden="true" />
            </Button>
          </Can>
        )}
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
      />
    </PageContainer>
  )
}
