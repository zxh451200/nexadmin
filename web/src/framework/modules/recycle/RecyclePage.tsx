import { useState } from 'react'
import { RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import type { Column } from '@/framework/components/table/types'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { fmtDateTime } from '../oper-log/format'
import { RECYCLE_TYPES, usePurge, useRecycleList, useRestore, type RecycleItem, type RecycleType } from './api'

const ALL = 'all'

export default function RecyclePage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState('')
  const [keyword, setKeyword] = useState('')
  const [type, setType] = useState<string>(ALL)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const { data, isLoading } = useRecycleList({
    page,
    pageSize,
    keyword: keyword || undefined,
    type: type === ALL ? undefined : (type as RecycleType),
  })
  const restore = useRestore()
  const purge = usePurge()

  const run = async (fn: () => Promise<unknown>, okKey: string) => {
    try {
      await fn()
      toast.success(t(okKey))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'))
    }
  }

  const onRestore = async (row: RecycleItem) => {
    if (!(await confirm({ description: t('recycle.confirm.restore', { name: row.name }) }))) return
    await run(() => restore.mutateAsync(row.id), 'recycle.restored')
  }

  const onPurge = async (row: RecycleItem) => {
    if (!(await confirm({ destructive: true, description: t('recycle.confirm.purge', { name: row.name }) }))) return
    await run(() => purge.mutateAsync(row.id), 'recycle.purged')
  }

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
          setPage(1)
        }}
        onReset={() => {
          setDraft('')
          setKeyword('')
          setType(ALL)
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
      </FilterBar>
      <DataTable<RecycleItem>
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="recycle"
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
