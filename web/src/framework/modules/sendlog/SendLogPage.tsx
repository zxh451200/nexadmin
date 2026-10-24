import { useState } from 'react'
import { Eraser, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import {
  SEND_CHANNELS,
  SEND_STATUSES,
  useDeleteSendLogs,
  useSendLogList,
  type SendLog,
  type SendLogQuery,
} from './api'
import { SendLogCleanDialog } from './SendLogCleanDialog'
import { SendLogDetailDrawer } from './SendLogDetailDrawer'
import { CHANNEL_VARIANT, STATUS_VARIANT } from './status'

interface Filters {
  recipient: string
  channel: string
  status: string
  range: DateRangeValue
}

const emptyFilters: Filters = { recipient: '', channel: 'all', status: 'all', range: {} }

function toQuery(f: Filters): Omit<SendLogQuery, 'page' | 'pageSize'> {
  return {
    recipient: f.recipient.trim() || undefined,
    channel: f.channel === 'all' ? undefined : f.channel,
    status: f.status === 'all' ? undefined : f.status,
    startDate: f.range.from,
    endDate: f.range.to,
  }
}

export default function SendLogPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])
  const [detail, setDetail] = useState<SendLog | null>(null)
  const [cleanOpen, setCleanOpen] = useState(false)

  const { data, isLoading } = useSendLogList({ page, pageSize, ...toQuery(applied) })
  const del = useDeleteSendLogs()

  const search = () => {
    setApplied(draft)
    setPage(1)
    setSelected([])
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
    setSelected([])
  }

  const onDelete = async (ids: string[]) => {
    if (!(await confirm({ destructive: true, description: t('sendlog.confirm.delete', { count: ids.length }) }))) return
    try {
      await del.mutateAsync(ids)
      setSelected([])
      toast.success(t('common.operateSuccess'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const columns: Column<SendLog>[] = [
    {
      key: 'channel',
      title: t('sendlog.col.channel'),
      mobile: 'title',
      render: (r) => <Badge variant={CHANNEL_VARIANT[r.channel]}>{t(`sendlog.channel.${r.channel}`)}</Badge>,
    },
    {
      key: 'scene',
      title: t('sendlog.col.scene'),
      mobile: 'sub',
      render: (r) => t(`sendlog.scene.${r.scene}`, { defaultValue: r.scene }),
    },
    { key: 'recipient', title: t('sendlog.col.recipient'), render: (r) => <span className="num">{r.recipient || '-'}</span> },
    { key: 'subject', title: t('sendlog.col.subject'), render: (r) => r.subject || '-' },
    {
      key: 'status',
      title: t('sendlog.col.status'),
      render: (r) => <Badge variant={STATUS_VARIANT[r.status]}>{t(`sendlog.status.${r.status}`)}</Badge>,
    },
    {
      key: 'error',
      title: t('sendlog.col.error'),
      render: (r) =>
        r.error ? (
          <span className="block max-w-xs truncate text-muted-foreground" title={r.error}>
            {r.error}
          </span>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    { key: 'durationMs', title: t('sendlog.col.duration'), align: 'end', render: (r) => <span className="num">{r.durationMs} ms</span> },
    { key: 'createdAt', title: t('sendlog.col.time'), render: (r) => <span className="num">{fmtDateTime(r.createdAt, i18n.language)}</span> },
  ]

  return (
    <PageContainer>
      <PageHeader
        title={t('sendlog.menu')}
        description={t('sendlog.description')}
        actions={
          <Can perm="sendlog:delete">
            <Button type="button" variant="outline" onClick={() => setCleanOpen(true)}>
              <Eraser className="me-2 size-4" aria-hidden="true" />
              {t('sendlog.action.clean')}
            </Button>
          </Can>
        }
      />
      <FilterBar onSearch={search} onReset={reset}>
        <div className="flex w-full flex-col gap-1.5 md:w-64">
          <Label htmlFor="sl-recipient">{t('sendlog.filter.recipient')}</Label>
          <Input
            id="sl-recipient"
            value={draft.recipient}
            placeholder={t('sendlog.filter.recipientPlaceholder')}
            onChange={(e) => setDraft({ ...draft, recipient: e.target.value })}
          />
        </div>
        <div className="flex w-full flex-col gap-1.5 md:w-40">
          <Label>{t('sendlog.filter.channel')}</Label>
          <Select value={draft.channel} onValueChange={(v) => setDraft({ ...draft, channel: v })}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('sendlog.filter.allChannels')}</SelectItem>
              {SEND_CHANNELS.map((k) => (
                <SelectItem key={k} value={k}>
                  {t(`sendlog.channel.${k}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex w-full flex-col gap-1.5 md:w-40">
          <Label>{t('sendlog.filter.status')}</Label>
          <Select value={draft.status} onValueChange={(v) => setDraft({ ...draft, status: v })}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('sendlog.filter.allStatuses')}</SelectItem>
              {SEND_STATUSES.map((k) => (
                <SelectItem key={k} value={k}>
                  {t(`sendlog.status.${k}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex w-full flex-col gap-1.5 md:w-64">
          <Label>{t('sendlog.filter.time')}</Label>
          <DateRangePicker value={draft.range} onChange={(v) => setDraft({ ...draft, range: v })} />
        </div>
      </FilterBar>
      <DataTable<SendLog>
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="sendlog"
        onRowClick={setDetail}
        selection={{ selected, onChange: setSelected }}
        batchActions={(ids) => (
          <Can perm="sendlog:delete">
            <Button type="button" variant="destructive" size="sm" className="h-10 md:h-8" onClick={() => onDelete(ids)}>
              <Trash2 className="me-2 size-4" aria-hidden="true" />
              {t('sendlog.action.deleteSelected')}
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
      <SendLogDetailDrawer log={detail} onClose={() => setDetail(null)} />
      <SendLogCleanDialog
        open={cleanOpen}
        onOpenChange={setCleanOpen}
        onDone={() => {
          setSelected([])
          setPage(1)
        }}
      />
    </PageContainer>
  )
}
