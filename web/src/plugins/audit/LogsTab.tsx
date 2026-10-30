import { useState } from 'react'
import { Eraser } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DetailAction } from '@/framework/components/DetailAction'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useCleanLogs, useLogDetail, useLogs, useLogTables, type AuditAction, type AuditLog, type LogQuery } from './api'
import { DiffView } from './DiffView'
import { actionVariant, formatTime } from './format'

const ALL = '__all'
const ACTIONS: AuditAction[] = ['create', 'update', 'delete']

interface Draft {
  table?: string
  operator?: string
  action?: string
  range: DateRangeValue
}

function useOperatorLabel() {
  const { t } = useTranslation()
  return (l: Pick<AuditLog, 'operatorId' | 'operatorName'>) => (l.operatorId === '0' ? t('plugin_audit.system') : l.operatorName)
}

function DetailDialog({ id, onClose }: { id: string | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const operatorLabel = useOperatorLabel()
  const { data } = useLogDetail(id ?? undefined)
  return (
    <ResponsiveDialog
      open={!!id}
      onOpenChange={(o) => !o && onClose()}
      size="lg"
      title={t('plugin_audit.detail')}
      description={data ? `${data.table} · ${data.rowPk}` : undefined}
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {data ? (
        <div className="flex flex-col gap-4 text-sm">
          <dl className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_audit.fields.action')}</dt>
              <dd>
                <Badge variant={actionVariant(data.action)}>{t(`plugin_audit.action.${data.action}`)}</Badge>
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_audit.fields.operator')}</dt>
              <dd>{operatorLabel(data)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('plugin_audit.fields.time')}</dt>
              <dd className="num">{formatTime(data.createdAt, i18n.language)}</dd>
            </div>
          </dl>
          <DiffView changes={data.changes} />
        </div>
      ) : null}
    </ResponsiveDialog>
  )
}

function CleanDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  const clean = useCleanLogs()
  const [days, setDays] = useState('90')
  const submit = async () => {
    try {
      const r = await clean.mutateAsync(Number(days))
      toast.success(t('plugin_audit.clean.done', { count: r.deleted }))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title={t('plugin_audit.clean.title')}
      description={t('plugin_audit.clean.description')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button type="button" variant="destructive" disabled={clean.isPending} onClick={() => void submit()}>
            {clean.isPending ? t('common.loading') : t('plugin_audit.btn.clean')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-2">
        <label className="text-sm font-medium text-foreground" id="audit-clean-days">
          {t('plugin_audit.clean.keep')}
        </label>
        <Select value={days} onValueChange={setDays}>
          <SelectTrigger aria-labelledby="audit-clean-days">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {['30', '90', '180', '365', '730'].map((d) => (
              <SelectItem key={d} value={d}>
                {t('plugin_audit.clean.days', { count: Number(d) })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </ResponsiveDialog>
  )
}

export function LogsTab() {
  const { t, i18n } = useTranslation()
  const operatorLabel = useOperatorLabel()
  const [draft, setDraft] = useState<Draft>({ range: {} })
  const [applied, setApplied] = useState<Draft>({ range: {} })
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [detail, setDetail] = useState<string | null>(null)
  const [cleanOpen, setCleanOpen] = useState(false)

  const query: LogQuery = {
    table: applied.table,
    operator: applied.operator?.trim() || undefined,
    action: applied.action,
    from: applied.range.from,
    to: applied.range.to,
    page,
    pageSize,
  }
  const { data, isLoading } = useLogs(query)
  const { data: tables } = useLogTables()

  const columns: Column<AuditLog>[] = [
    { key: 'createdAt', title: t('plugin_audit.fields.time'), mobile: 'sub', render: (l) => <span className="num">{formatTime(l.createdAt, i18n.language)}</span> },
    { key: 'table', title: t('plugin_audit.fields.table'), mobile: 'title', render: (l) => <span className="font-mono text-xs font-medium text-foreground">{l.table}</span> },
    { key: 'rowPk', title: t('plugin_audit.fields.rowPk'), render: (l) => <span className="num font-mono text-xs">{l.rowPk}</span> },
    { key: 'action', title: t('plugin_audit.fields.action'), render: (l) => <Badge variant={actionVariant(l.action)}>{t(`plugin_audit.action.${l.action}`)}</Badge> },
    { key: 'operator', title: t('plugin_audit.fields.operator'), render: (l) => operatorLabel(l) },
    {
      key: 'fields',
      title: t('plugin_audit.fields.changedFields'),
      render: (l) => (
        <span className="block max-w-[18rem] truncate font-mono text-xs text-muted-foreground" title={l.fields}>
          {l.fields || '-'}
        </span>
      ),
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      <FilterBar
        onSearch={() => {
          setApplied(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft({ range: {} })
          setApplied({ range: {} })
          setPage(1)
        }}
      >
        <Select value={draft.table ?? ALL} onValueChange={(v) => setDraft({ ...draft, table: v === ALL ? undefined : v })}>
          <SelectTrigger className="md:w-52" aria-label={t('plugin_audit.fields.table')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_audit.all')}</SelectItem>
            {(tables ?? []).map((n) => (
              <SelectItem key={n} value={n}>
                {n}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="md:w-44"
          value={draft.operator ?? ''}
          maxLength={100}
          aria-label={t('plugin_audit.fields.operator')}
          placeholder={t('plugin_audit.operatorPlaceholder')}
          onChange={(e) => setDraft({ ...draft, operator: e.target.value })}
        />
        <Select value={draft.action ?? ALL} onValueChange={(v) => setDraft({ ...draft, action: v === ALL ? undefined : v })}>
          <SelectTrigger className="md:w-40" aria-label={t('plugin_audit.fields.action')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_audit.all')}</SelectItem>
            {ACTIONS.map((a) => (
              <SelectItem key={a} value={a}>
                {t(`plugin_audit.action.${a}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <DateRangePicker value={draft.range} onChange={(range) => setDraft({ ...draft, range })} placeholder={t('plugin_audit.fields.timeRange')} className="md:w-64" />
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-audit-logs"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_audit.empty')}
        toolbar={
          <Can perm="audit:clean">
            <Button variant="outline" onClick={() => setCleanOpen(true)}>
              <Eraser aria-hidden="true" />
              {t('plugin_audit.btn.clean')}
            </Button>
          </Can>
        }
        rowActions={(l) => <DetailAction onClick={() => setDetail(l.id)} />}
      />

      <DetailDialog id={detail} onClose={() => setDetail(null)} />
      <CleanDialog open={cleanOpen} onOpenChange={setCleanOpen} />
    </div>
  )
}
