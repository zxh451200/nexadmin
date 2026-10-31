import { LogOut } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { DictTag } from '@/framework/components/DictTag'
import type { Column } from '@/framework/components/table/types'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { fmtDateTime } from '../oper-log/format'
import { useKickSession, useSessions, type LoginSession } from './api'

export function SessionsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { data = [], isLoading } = useSessions()
  const kick = useKickSession()

  const onKick = async (row: LoginSession) => {
    if (!(await confirm({ destructive: true, description: t('profile.sessions.confirmKick') }))) return
    try {
      await kick.mutateAsync(row.id)
      toast.success(t('profile.sessions.kicked'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  // 后端目前不返回归属地；没有数据时不要展示一列空白。
  const hasLocation = data.some((r) => !!r.location)
  const allColumns: Column<LoginSession>[] = [
    {
      key: 'device',
      title: t('profile.sessions.device'),
      mobile: 'title',
      render: (r) => (
        <div className="flex flex-wrap items-center gap-2">
          <DictTag dict="online_device" value={r.device} />
          <span className="text-sm text-foreground">{[r.browser, r.os].filter(Boolean).join(' · ')}</span>
          {r.current ? <Badge variant="success">{t('profile.sessions.current')}</Badge> : null}
        </div>
      ),
    },
    { key: 'ip', title: t('profile.sessions.ip'), render: (r) => <span className="num">{r.ip}</span> },
    { key: 'location', title: t('profile.sessions.location') },
    { key: 'loginAt', title: t('profile.sessions.loginAt'), render: (r) => <span className="num">{fmtDateTime(r.loginAt, i18n.language)}</span> },
    { key: 'lastActiveAt', title: t('profile.sessions.lastActive'), render: (r) => <span className="num">{fmtDateTime(r.lastActiveAt, i18n.language)}</span> },
  ]
  const columns = allColumns.filter((c) => c.key !== 'location' || hasLocation)

  return (
    <div className="flex flex-col gap-3">
      <div>
        <h3 className="text-sm font-medium text-foreground">{t('profile.sessions.title')}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{t('profile.sessions.description')}</p>
      </div>
      <DataTable<LoginSession>
        columns={columns}
        data={data}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        rowActions={(row) =>
          row.current ? null : (
            <Button type="button" variant="ghost" size="icon" aria-label={t('profile.sessions.kick')} onClick={() => onKick(row)}>
              <LogOut className="size-4 text-destructive" aria-hidden="true" />
            </Button>
          )
        }
      />
    </div>
  )
}
