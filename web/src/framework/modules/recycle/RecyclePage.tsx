import { useState } from 'react'
import { RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { DatePicker } from '@/framework/components/DatePicker'
import { toDate } from '@/framework/components/pickers/dates'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import {
  RECYCLE_TYPES,
  usePurge,
  usePurgeBatch,
  useRecycleList,
  useRestore,
  useRestoreBatch,
  type RecycleBatchResult,
  type RecycleItem,
  type RecycleType,
} from './api'

const ALL = 'all'

export default function RecyclePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [type, setType] = useState<string>(ALL)
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [appliedRange, setAppliedRange] = useState<{ from?: string; to?: string }>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [selected, setSelected] = useState<string[]>([])

  const { data, isLoading } = useRecycleList({
    page,
    pageSize,
    keyword: keyword || undefined,
    type: type === ALL ? undefined : (type as RecycleType),
    from: appliedRange.from || undefined,
    to: appliedRange.to || undefined,
  })
  const restore = useRestore()
  const restoreBatch = useRestoreBatch()
  const purge = usePurge()
  const purgeBatch = usePurgeBatch()

  const run = async (fn: () => Promise<unknown>, okKey: string) => {
    try {
      await fn()
      toast.success(t(okKey))
      setSelected([])
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  // 批量回执：冲突与源行消失按条计数，让用户知道有多少条没成、为什么列表少了行。
  const report = (res: RecycleBatchResult | undefined, okKey: string) => {
    const parts: string[] = []
    if (res?.restored) parts.push(t('recycle.batch.restored', { count: res.restored }))
    if (res?.purged) parts.push(t('recycle.batch.purged', { count: res.purged }))
    if (res?.conflict) parts.push(t('recycle.batch.conflict', { count: res.conflict }))
    if (res?.gone) parts.push(t('recycle.batch.gone', { count: res.gone }))
    if (parts.length) toast.success(parts.join('，'))
    else toast.success(t(okKey))
    setSelected([])
  }

  const onRestore = async (row: RecycleItem) => {
    if (!(await confirm({ description: t('recycle.confirm.restore', { name: row.name }) }))) return
    await run(() => restore.mutateAsync(row.id), 'recycle.restored')
  }

  const onPurge = async (row: RecycleItem) => {
    if (!(await confirm({ destructive: true, description: t('recycle.confirm.purge', { name: row.name }) }))) return
    await run(() => purge.mutateAsync(row.id), 'recycle.purged')
  }

  const onRestoreBatch = async (rows: RecycleItem[]) => {
    if (!(await confirm({ description: t('recycle.confirm.restoreBatch', { count: rows.length }) }))) return
    try {
      report(await restoreBatch.mutateAsync(rows.map((r) => r.id)), 'recycle.restored')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onPurgeBatch = async (rows: RecycleItem[]) => {
    if (!(await confirm({ destructive: true, description: t('recycle.confirm.purgeBatch', { count: rows.length }) }))) return
    try {
      report(await purgeBatch.mutateAsync(rows.map((r) => r.id)), 'recycle.purged')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const selectedRows = (data?.list ?? []).filter((r) => selected.includes(r.id))

  const columns: Column<RecycleItem>[] = [
    { key: 'name', title: t('recycle.col.name'), mobile: 'title' },
    {
      key: 'type',
      title: t('recycle.col.type'),
      mobile: 'sub',
      render: (r) => <Badge variant="secondary">{t(`recycle.type.${r.type}`)}</Badge>,
    },
    { key: 'deletedBy', title: t('recycle.col.deletedBy') },
    {
      key: 'deletedAt',
      title: t('recycle.col.deletedAt'),
      render: (r) => <span className="num">{fmtDateTime(r.deletedAt, i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('recycle.menu')} description={t('recycle.description')} />
      <FilterBar
        onSearch={() => {
          setKeyword(draft.trim())
          setAppliedRange({ from: from || undefined, to: to || undefined })
          setPage(1)
        }}
        onReset={() => {
          setDraft('')
          setKeyword('')
          setType(ALL)
          setFrom('')
          setTo('')
          setAppliedRange({})
          setPage(1)
        }}
      >
        <Input aria-label={t('recycle.filter.keyword')} className="md:w-64"
          id="rc-keyword"
          value={draft}
          placeholder={t('recycle.filter.keywordPlaceholder')}
          onChange={(e) => setDraft(e.target.value)}
        />
        <Select
          value={type}
          onValueChange={(v) => {
            setType(v)
            setPage(1)
          }}
        >
          <SelectTrigger id="rc-type" className="md:w-48" aria-label={t('recycle.filter.type')}>
            <SelectValue placeholder={t('recycle.filter.type')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('recycle.type.all')}</SelectItem>
            {RECYCLE_TYPES.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`recycle.type.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {/* 删除时间范围：用共享 DatePicker，不用原生 type=date——原生弹窗是浏览器自己的
            控件，跟主题令牌、语言包、其它筛选栏都不是一套长相（这条是全仓规矩）。 */}
        <DatePicker
          ariaLabel={t('recycle.filter.from')}
          className="md:w-48"
          value={from}
          toDate={toDate(to)}
          onChange={setFrom}
        />
        <DatePicker
          ariaLabel={t('recycle.filter.to')}
          className="md:w-48"
          value={to}
          fromDate={toDate(from)}
          onChange={setTo}
        />
      </FilterBar>
      <DataTable<RecycleItem>
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="recycle"
        selection={{ selected, onChange: setSelected }}
        batchActions={(_ids) => (
          <>
            <Can perm="recycle:restore">
              <Button type="button" variant="outline" size="sm" onClick={() => void onRestoreBatch(selectedRows)}>
                <RotateCcw className="me-2 size-4" aria-hidden="true" />
                {t('recycle.action.restore')}
              </Button>
            </Can>
            <Can perm="recycle:delete">
              <Button type="button" variant="destructive" size="sm" onClick={() => void onPurgeBatch(selectedRows)}>
                <Trash2 className="me-2 size-4" aria-hidden="true" />
                {t('recycle.action.purge')}
              </Button>
            </Can>
          </>
        )}
        rowActions={(row) => (
          <div className="flex items-center">
            <Can perm="recycle:restore">
              <Button type="button" variant="ghost" size="icon" aria-label={t('recycle.action.restore')} onClick={() => onRestore(row)}>
                <RotateCcw className="size-4" aria-hidden="true" />
              </Button>
            </Can>
            <Can perm="recycle:delete">
              <Button type="button" variant="ghost" size="icon" aria-label={t('recycle.action.purge')} onClick={() => onPurge(row)}>
                <Trash2 className="size-4 text-destructive" aria-hidden="true" />
              </Button>
            </Can>
          </div>
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
