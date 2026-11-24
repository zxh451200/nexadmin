import { useState } from 'react'
import { Download, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useAsyncJob } from '@/framework/hooks/use-async-job'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { FilterBar } from '@/framework/components/FilterBar'
import { OptionPicker } from '@/framework/components/OptionPicker'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import {
  createSiteOrderExportJob,
  useDeleteSiteOrders,
  useSiteOrders,
  type SiteOrder,
} from './api'

function formatTime(v: string, lang: string) {
  return new Intl.DateTimeFormat(lang, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(v))
}

const ORDER_STATUSES: SiteOrder['status'][] = ['pending', 'paid', 'failed', 'closed']

/** 站点订单：官网商品页在线购买（PurchaseEnabled 开关）的成交记录。
 * 资金状态以 pay 中台为准（业务单号 SO… 在「支付中台」按 biz 查询对账），这里管业务侧列表。 */
export default function SiteOrdersPage() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { start: startExportJob } = useAsyncJob()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const { sort, desc, sortProp } = useHeaderSort('created_at', true, () => setPage(1))
  const [draft, setDraft] = useState<{ keyword?: string; status?: string[] }>({})
  const [filters, setFilters] = useState<{ keyword?: string; status?: string }>({})
  const [selected, setSelected] = useState<string[]>([])
  const { data, isLoading } = useSiteOrders({
    page,
    pageSize,
    keyword: filters.keyword || undefined,
    status: filters.status,
    sort,
    desc,
  })
  const remove = useDeleteSiteOrders()

  const statusLabel = (s: SiteOrder['status']) => {
    switch (s) {
      case 'paid': return t('plugin_website.orders.status.paid')
      case 'pending': return t('plugin_website.orders.status.pending')
      case 'failed': return t('plugin_website.orders.status.failed')
      default: return t('plugin_website.orders.status.closed')
    }
  }

  const statusBadge = (s: SiteOrder['status']) => {
    switch (s) {
      case 'paid': return <Badge variant="success">{statusLabel(s)}</Badge>
      case 'pending': return <Badge variant="warning">{statusLabel(s)}</Badge>
      case 'failed': return <Badge variant="destructive">{statusLabel(s)}</Badge>
      default: return <Badge variant="outline">{statusLabel(s)}</Badge>
    }
  }

  const [exporting, setExporting] = useState(false)
  // 导出沿用当前筛选（不是当前页）：统一走异步任务，提交后由 useAsyncJob
  // 提示「已提交到导出中心」，进度与下载都在导出中心看。
  const onExport = async () => {
    setExporting(true)
    try {
      await startExportJob(() => createSiteOrderExportJob({ keyword: filters.keyword, status: filters.status }))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setExporting(false)
    }
  }

  // 按 id 删：勾选可以跨页，用当前页的行反查只会删掉本页那几条。
  const onDelete = async (ids: string[]) => {
    const orderNo = ids.length === 1 ? (data?.list.find((r) => r.id === ids[0])?.orderNo ?? '') : ''
    const ok = await confirm({
      destructive: true,
      description: orderNo
        ? t('plugin_website.orders.deleteConfirm', { orderNo })
        : t('plugin_website.orders.deleteBatchConfirm', { count: ids.length }),
    })
    if (!ok) return
    try {
      await remove.mutateAsync(ids)
      setSelected((s) => s.filter((id) => !ids.includes(id)))
      toast.success(t('common.operateSuccess'))
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<SiteOrder>[] = [
    {
      key: 'orderNo',
      title: t('plugin_website.orders.fields.orderNo'),
      mobile: 'title',
      sortKey: 'order_no',
      render: (o) => <span className="num font-medium">{o.orderNo}</span>,
    },
    {
      key: 'productTitle',
      title: t('plugin_website.orders.fields.product'),
      sortKey: 'product_title',
      render: (o) => (
        <div className="min-w-0">
          <span className="block max-w-[20rem] truncate font-medium text-foreground" title={o.productTitle}>
            {o.productTitle}
          </span>
          <div className="text-xs text-muted-foreground">{t('plugin_website.orders.qty', { count: o.qty })}</div>
        </div>
      ),
    },
    {
      key: 'buyer',
      title: t('plugin_website.orders.fields.buyer'),
      render: (o) => (
        <div className="min-w-0">
          <span className="block truncate">{o.buyerEmail}</span>
          {o.buyerPhone ? <div className="text-xs text-muted-foreground">{o.buyerPhone}</div> : null}
        </div>
      ),
    },
    {
      key: 'amount',
      title: t('plugin_website.orders.fields.amount'),
      align: 'end',
      sortKey: 'amount_minor',
      render: (o) => (
        <div className="flex min-w-32 flex-col items-end gap-1">
          {/* 收款币种与金额：与 pay 中台支付单一致；外币单（CNY 换算）在定价币种上用小字回显 */}
          <span className="num font-medium">
            {o.payCurrency}
            {(o.payAmountMinor / 100).toFixed(2)}
          </span>
          {o.payCurrency !== o.currency ? (
            <span className="num text-xs text-muted-foreground">
              {t('plugin_website.orders.origAmount', { currency: o.currency, amount: (o.amountMinor / 100).toFixed(2) })}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      key: 'status',
      title: t('plugin_website.orders.fields.status'),
      render: (o) => statusBadge(o.status),
    },
    {
      key: 'createdAt',
      title: t('plugin_website.orders.fields.createdAt'),
      sortKey: 'created_at',
      render: (o) => <span className="num text-xs text-muted-foreground">{formatTime(o.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <PageContainer>
      <PageHeader title={t('plugin_website.orders.title')} description={t('plugin_website.orders.description')} />
      <FilterBar
        onSearch={() => {
          setFilters({ keyword: draft.keyword, status: draft.status?.length ? draft.status.join(',') : undefined })
          setPage(1)
          setSelected([])
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
          setSelected([])
        }}
      >
        <Input
          className="md:w-64"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_website.orders.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <OptionPicker
          title={t('plugin_website.orders.filterStatus')}
          value={draft.status ?? []}
          onChange={(v) => setDraft((d) => ({ ...d, status: v.length ? v : undefined }))}
          options={ORDER_STATUSES.map((s) => ({ value: s, label: statusLabel(s) }))}
          className="md:w-44"
          placeholder={t('plugin_website.orders.filterStatus')}
        />
      </FilterBar>
      <DataTable
        sort={sortProp}
        toolbar={
          <Can perm="website:order">
            <Button type="button" variant="outline" onClick={() => void onExport()} disabled={exporting}>
              <Download aria-hidden="true" />
              {t('common.export')}
            </Button>
          </Can>
        }
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="website-site-orders"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => { setPage(p); setPageSize(s) },
        }}
        emptyText={t('plugin_website.orders.empty')}
        selection={{ selected, onChange: setSelected }}
        batchActions={() => (
          <Can perm="website:order">
            <Button type="button" variant="destructive" size="sm" onClick={() => void onDelete(selected)}>
              <Trash2 aria-hidden="true" />
              {t('common.delete')}
            </Button>
          </Can>
        )}
        rowActions={(row) => (
          <Can perm="website:order">
            <Button variant="ghost" size="icon" aria-label={t('common.delete')} onClick={() => void onDelete([row.id])}>
              <Trash2 className="size-4" aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
    </PageContainer>
  )
}
