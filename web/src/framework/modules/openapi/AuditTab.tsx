import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { DetailAction } from '@/framework/components/DetailAction'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { hasActiveFilterValues } from '@/framework/components/table/use-client-filter'
import { Badge, type BadgeProps } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import { useAudits, type Audit } from './api'

const ANY = '__any'
const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']
const STATUS_CODES = ['200', '201', '400', '401', '403', '404', '429', '500', '502', '503']

interface Filters {
  keyword: string
  principalType: string
  method: string
  authOutcome: string
  status: string
  start: string
  end: string
}

const emptyFilters: Filters = { keyword: '', principalType: ANY, method: ANY, authOutcome: ANY, status: ANY, start: '', end: '' }
const statusVariant = (status: number): BadgeProps['variant'] => (status >= 500 ? 'destructive' : status >= 400 ? 'warning' : 'success')

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm break-all text-foreground">{children}</dd>
    </div>
  )
}

export function AuditTab() {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<Filters>(emptyFilters)
  const [applied, setApplied] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { sort, desc, sortProp } = useHeaderSort('requestedAt', true, () => setPage(1))
  const [detail, setDetail] = useState<Audit | null>(null)

  const { data, isLoading, isError, isFetching, refetch } = useAudits({
    page,
    pageSize,
    keyword: applied.keyword.trim() || undefined,
    principalType: applied.principalType === ANY ? undefined : (applied.principalType as 'pat' | 'app'),
    method: applied.method === ANY ? undefined : applied.method,
    authOutcome: applied.authOutcome === ANY ? undefined : (applied.authOutcome as 'authenticated' | 'rejected'),
    status: applied.status === ANY ? undefined : applied.status,
    start: applied.start ? new Date(applied.start).toISOString() : undefined,
    end: applied.end ? new Date(applied.end).toISOString() : undefined,
    sort,
    desc,
  })

  const search = () => {
    setApplied(draft)
    setPage(1)
  }
  const reset = () => {
    setDraft(emptyFilters)
    setApplied(emptyFilters)
    setPage(1)
  }
  const time = (iso: string) => <span className="num">{fmtDateTime(iso, i18n.language)}</span>
  const anyLabels = {
    principalType: t('openapi.audit.filter.allTypes'),
    method: t('openapi.audit.filter.allMethods'),
    authOutcome: t('openapi.audit.filter.allStatuses'),
    status: t('openapi.audit.filter.allStatuses'),
  }
  const select = (key: 'principalType' | 'method' | 'authOutcome' | 'status', label: string, options: { value: string; label: string }[], width: string) => (
    <Select value={draft[key]} onValueChange={(value) => setDraft({ ...draft, [key]: value })}>
      <SelectTrigger className={width} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={ANY}>{anyLabels[key]}</SelectItem>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )

  const columns: Column<Audit>[] = [
    { key: 'requestedAt', title: t('openapi.audit.fields.requestedAt'), sortKey: 'requestedAt', mobile: 'title', render: (a) => time(a.requestedAt) },
    {
      key: 'credential',
      title: t('openapi.audit.fields.credential'),
      mobile: 'sub',
      render: (a) => (
        <div className="flex min-w-0 flex-col gap-1">
          <span className="truncate font-medium text-foreground" title={a.credentialName}>{a.credentialName}</span>
          <span className="text-xs text-muted-foreground">{t(`openapi.audit.type.${a.principalType}`)}</span>
        </div>
      ),
    },
    { key: 'method', title: t('openapi.audit.fields.method'), render: (a) => <code className="font-mono text-xs">{a.method}</code> },
    {
      key: 'path',
      title: t('openapi.audit.fields.path'),
      render: (a) => <code className="block max-w-80 truncate font-mono text-xs" title={a.path}>{a.path}</code>,
    },
    {
      key: 'statusCode',
      title: t('openapi.audit.fields.status'),
      sortKey: 'statusCode',
      render: (a) => <Badge variant={statusVariant(a.statusCode)}>{a.statusCode}</Badge>,
    },
    { key: 'durationMs', title: t('openapi.audit.fields.duration'), sortKey: 'durationMs', align: 'end', render: (a) => <span className="num">{a.durationMs} ms</span> },
    { key: 'clientIp', title: t('openapi.audit.fields.ip'), render: (a) => <span className="font-mono text-xs">{a.clientIp}</span> },
    {
      key: 'requestId',
      title: t('openapi.audit.fields.requestId'),
      render: (a) => <code className="block max-w-40 truncate font-mono text-xs" title={a.requestId}>{a.requestId || '-'}</code>,
    },
  ]

  const filterValues = {
    keyword: applied.keyword.trim() || undefined,
    principalType: applied.principalType === ANY ? undefined : applied.principalType,
    method: applied.method === ANY ? undefined : applied.method,
    authOutcome: applied.authOutcome === ANY ? undefined : applied.authOutcome,
    status: applied.status === ANY ? undefined : applied.status,
    start: applied.start || undefined,
    end: applied.end || undefined,
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">{t('openapi.audit.retention')}</p>
      <FilterBar onSearch={search} onReset={reset}>
        <Input
          className="md:w-72"
          value={draft.keyword}
          onChange={(e) => setDraft({ ...draft, keyword: e.target.value })}
          placeholder={t('openapi.audit.filter.keyword')}
          aria-label={t('openapi.audit.filter.keyword')}
        />
        {select('principalType', t('openapi.audit.filter.type'), [
          { value: 'pat', label: t('openapi.audit.type.pat') },
          { value: 'app', label: t('openapi.audit.type.app') },
        ], 'md:w-40')}
        {select('method', t('openapi.audit.filter.method'), METHODS.map((method) => ({ value: method, label: method })), 'md:w-36')}
        {select('authOutcome', t('openapi.audit.filter.authOutcome'), [
          { value: 'authenticated', label: t('openapi.audit.outcome.authenticated') },
          { value: 'rejected', label: t('openapi.audit.outcome.rejected') },
        ], 'md:w-40')}
        {select('status', t('openapi.audit.filter.status'), STATUS_CODES.map((status) => ({ value: status, label: status })), 'md:w-36')}
        <Input type="datetime-local" className="md:w-56" value={draft.start} onChange={(e) => setDraft({ ...draft, start: e.target.value })} aria-label={t('openapi.audit.filter.start')} />
        <Input type="datetime-local" className="md:w-56" value={draft.end} onChange={(e) => setDraft({ ...draft, end: e.target.value })} aria-label={t('openapi.audit.filter.end')} />
      </FilterBar>

      <DataTable
        columns={columns}
        columnSettingsKey="openapiAudits"
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        isError={isError}
        onRetry={() => refetch()}
        retrying={isFetching}
        hasFilters={hasActiveFilterValues(filterValues)}
        sort={sortProp}
        emptyText={t('openapi.audit.empty')}
        pagination={{ page, pageSize, total: data?.total ?? 0, onChange: (p, s) => { setPage(p); setPageSize(s) } }}
        rowActions={(a) => <DetailAction onClick={() => setDetail(a)} />}
      />

      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(open) => !open && setDetail(null)}
        size="lg"
        title={t('openapi.audit.detail')}
        description={detail ? `${detail.method} ${detail.path}` : undefined}
        footer={<Button type="button" onClick={() => setDetail(null)}>{t('common.close')}</Button>}
      >
        {detail ? (
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <DetailRow label={t('openapi.audit.fields.requestedAt')}>{time(detail.requestedAt)}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.status')}><Badge variant={statusVariant(detail.statusCode)}>{detail.statusCode}</Badge></DetailRow>
            <DetailRow label={t('openapi.audit.fields.principal')}>{t(`openapi.audit.type.${detail.principalType}`)}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.authOutcome')}>
              <Badge variant={detail.authOutcome === 'rejected' ? 'destructive' : 'success'}>{t(`openapi.audit.outcome.${detail.authOutcome}`)}</Badge>
            </DetailRow>
            <DetailRow label={t('openapi.audit.fields.credential')}>{detail.credentialName}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.credentialLabel')}>{detail.credentialLabel || '-'}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.credentialId')}>{detail.credentialId}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.requestId')}>{detail.requestId || '-'}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.userId')}>{detail.userId}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.username')}>{detail.username || '-'}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.failureKey')}>{detail.failureKey || '-'}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.duration')}>{detail.durationMs} ms</DetailRow>
            <DetailRow label={t('openapi.audit.fields.ip')}>{detail.clientIp}</DetailRow>
            <DetailRow label={t('openapi.audit.fields.createdAt')}>{time(detail.createdAt)}</DetailRow>
            <div className="sm:col-span-2"><DetailRow label={t('openapi.audit.fields.method')}>{detail.method}</DetailRow></div>
            <div className="sm:col-span-2"><DetailRow label={t('openapi.audit.fields.path')}><code className="font-mono text-xs">{detail.path}</code></DetailRow></div>
            <div className="sm:col-span-2"><DetailRow label={t('openapi.audit.fields.userAgent')}>{detail.userAgent || '-'}</DetailRow></div>
          </dl>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}
