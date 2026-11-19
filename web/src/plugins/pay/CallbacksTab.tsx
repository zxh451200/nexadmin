import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ArrowUpRight, Copy, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { useHeaderSort } from '@/framework/hooks/use-header-sort'
import { DateRangePicker, type DateRangeValue } from '@/framework/components/DateRangePicker'
import { DetailAction } from '@/framework/components/DetailAction'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { callbackReplayable, useCallbacks, useChannels, useReplayCallback, type CallbackQuery, type PayCallback } from './api'
import { formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

const ALL = '__all'

/** 已知结果码 → 文案键；未知码原样显示（后端先于前端发新码时也不至于空白）。 */
const RESULT_KEYS: Record<string, string> = {
  accepted: 'accepted',
  duplicate: 'duplicate',
  sign_invalid: 'signInvalid',
  stale_notify: 'staleNotify',
  order_not_found: 'orderNotFound',
  amount_mismatch: 'amountMismatch',
}

/**
 * 结果码 → 本地化文案。ignored_state_<status>（后端 ResultIgnoredStatePrefix，
 * 下划线）后拼的是资金单状态，复用支付单状态词典；翻译不到时回退原始码。
 */
function callbackResultLabel(result: string, t: TFunction): string {
  if (!result) return ''
  if (result.startsWith('ignored_state_')) {
    const state = result.slice('ignored_state_'.length)
    return t('plugin_pay.callbackResult.ignoredState', { state: t(`plugin_pay.status.${state}`, { defaultValue: state }) })
  }
  const key = RESULT_KEYS[result]
  return key ? t(`plugin_pay.callbackResult.${key}`) : result
}

export function CallbacksTab() {
  const { t, i18n } = useTranslation()
  const [params, setParams] = useSearchParams()
  const [draft, setDraft] = useState<CallbackQuery>({})
  const [filters, setFilters] = useState<CallbackQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  // 表头排序：与筛选分开存；默认最新在前（与后端回退一致）
  const { sort, desc, sortProp } = useHeaderSort('created_at', true, () => setPage(1))
  const [detail, setDetail] = useState<PayCallback | null>(null)
  const replay = useReplayCallback()

  const { data, isLoading, isError, refetch } = useCallbacks({ ...filters, page, pageSize, sort, desc })
  const { data: channels = [] } = useChannels()
  // 渠道列显示实例名（表头「渠道实例」），code 只在找不到实例时兜底——运营看得懂名称看不懂 code
  const channelName = useMemo(() => new Map(channels.map((c) => [c.code, c.name])), [channels])

  // 重放：后端幂等（重复回调得 duplicate 不发事件），前端只做一次确认
  const onReplay = async (cb: PayCallback) => {
    try {
      const fresh = await replay.mutateAsync(cb.id)
      toast.success(t('plugin_pay.callbackReplay.done', { result: callbackResultLabel(fresh.result, t) }))
    } catch {
      // 失败原因由 http 拦截器统一弹出
    }
  }

  const copyText = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(t('plugin_pay.copy.done'))
    } catch {
      toast.error(t('plugin_pay.copy.failed'))
    }
  }

  // 从回调审计跳到支付单：切回「支付单」页签并带关键词深链，OrdersTab 挂载时套用
  const jumpToOrder = (paymentNo: string) => {
    const next = new URLSearchParams(params)
    next.delete('tab')
    next.delete('view')
    next.set('kw', paymentNo)
    setParams(next)
  }

  const columns: Column<PayCallback>[] = [
    {
      key: 'paymentNo',
      title: t('plugin_pay.fields.paymentNo'),
      mobile: 'title',
      render: (r) =>
        r.paymentNo ? (
          <button
            type="button"
            onClick={() => jumpToOrder(r.paymentNo)}
            title={t('plugin_pay.jumpToOrder')}
            className="inline-flex items-center gap-0.5 font-medium text-foreground underline-offset-2 hover:text-primary hover:underline"
          >
            <span className="num">{r.paymentNo}</span>
            <ArrowUpRight aria-hidden="true" className="size-3 shrink-0 text-muted-foreground" />
          </button>
        ) : (
          // 验签失败的行定位不到支付单，没有单号可跳
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'channel',
      title: t('plugin_pay.fields.channel'),
      sortKey: 'channel',
      mobile: 'sub',
      render: (r) => <Badge variant="outline">{channelName.get(r.channelCode) ?? r.channelCode}</Badge>,
    },
    {
      key: 'ok',
      title: t('plugin_pay.fields.verify'),
      render: (r) =>
        r.ok ? <Badge variant="success">{t('plugin_pay.verifyOk')}</Badge> : <Badge variant="destructive">{t('plugin_pay.verifyFail')}</Badge>,
    },
    // 网关返回的消息长度不限：截两行 + 悬停看原文（详情弹窗里已有完整报文）
    {
      key: 'result',
      title: t('plugin_pay.fields.result'),
      render: (r) =>
        r.result ? (
          <span className="line-clamp-2 max-w-72 text-xs" title={r.result}>
            {callbackResultLabel(r.result, t)}
          </span>
        ) : (
          <span className="text-muted-foreground">-</span>
        ),
    },
    {
      key: 'createdAt',
      title: t('plugin_pay.fields.createdAt'),
      sortKey: 'created_at',
      render: (r) => <span className="num text-xs text-muted-foreground">{formatTime(r.createdAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
      <FilterBar
        onSearch={() => {
          setFilters(draft)
          setPage(1)
        }}
        onReset={() => {
          setDraft({})
          setFilters({})
          setPage(1)
        }}
      >
        {/* 后端 keyword 只搜支付单号（回调表没有业务单号）：占位符照实写，别许诺搜不到的东西。
            不复用 plugin_pay.searchPlaceholder——那个键被订单/退款共用，它们的关键词含业务单号 */}
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_pay.callbacks.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select
          value={draft.channelCode ?? ALL}
          onValueChange={(v) => setDraft((d) => ({ ...d, channelCode: v === ALL ? undefined : v }))}
        >
          <SelectTrigger className="md:w-40" aria-label={t('plugin_pay.fields.channel')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.filter.allChannels')}</SelectItem>
            {channels.map((c) => (
              <SelectItem key={c.code} value={c.code}>
                {c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={draft.ok === true ? 'ok' : draft.ok === false ? 'fail' : ALL} onValueChange={(v) => setDraft((d) => ({ ...d, ok: v === 'ok' ? true : v === 'fail' ? false : undefined }))}>
          <SelectTrigger className="md:w-36" aria-label={t('plugin_pay.fields.verify')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_pay.filter.allVerifies')}</SelectItem>
            <SelectItem value="ok">{t('plugin_pay.verifyOk')}</SelectItem>
            <SelectItem value="fail">{t('plugin_pay.verifyFail')}</SelectItem>
          </SelectContent>
        </Select>
        {/* 排障高频：「只看某天的通知」。参数名与支付单列表的 createdFrom/createdTo 对齐 */}
        <DateRangePicker
          value={{ from: draft.createdFrom, to: draft.createdTo }}
          onChange={(v: DateRangeValue) => setDraft((d) => ({ ...d, createdFrom: v.from, createdTo: v.to }))}
          ariaLabel={t('plugin_pay.fields.createdAt')}
          className="md:w-64"
          placeholder={t('plugin_pay.fields.createdAt')}
        />
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        sort={sortProp}
        columnSettingsKey="plugin-pay-callbacks"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_pay.empty')}
        rowActions={(r) => (
          <>
            <DetailAction onClick={() => setDetail(r)} />
            {callbackReplayable(r) ? (
              <Can perm="pay:edit">
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t('plugin_pay.callbackReplay.action')}
                  title={t('plugin_pay.callbackReplay.action')}
                  disabled={replay.isPending}
                  onClick={() => void onReplay(r)}
                >
                  <RotateCcw aria-hidden="true" />
                </Button>
              </Can>
            ) : null}
          </>
        )}
      />
      <ResponsiveDialog
        open={!!detail}
        onOpenChange={(v) => !v && setDetail(null)}
        title={`${t('common.detail')} · ${detail?.paymentNo || detail?.channelCode || ''}`}
        size="lg"
        footer={
          <Button variant="outline" onClick={() => setDetail(null)}>
            {t('common.close')}
          </Button>
        }
      >
        {detail ? (
          <div className="flex flex-col gap-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="outline">{channelName.get(detail.channelCode) ?? detail.channelCode}</Badge>
              {detail.ok ? <Badge variant="success">{t('plugin_pay.verifyOk')}</Badge> : <Badge variant="destructive">{t('plugin_pay.verifyFail')}</Badge>}
              <span className="text-muted-foreground">{callbackResultLabel(detail.result, t)}</span>
            </div>
            <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                  {t('plugin_pay.fields.paymentNo')}
                  {detail.paymentNo ? (
                    <button
                      type="button"
                      aria-label={`${t('plugin_pay.copy.action')} ${t('plugin_pay.fields.paymentNo')}`}
                      className="inline-flex"
                      onClick={() => void copyText(detail.paymentNo)}
                    >
                      <Copy aria-hidden="true" className="size-3" />
                    </button>
                  ) : null}
                </dt>
                <dd className="num mt-0.5 break-all">{detail.paymentNo || '-'}</dd>
              </div>
              {detail.notifyId ? (
                <div>
                  <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                    {t('plugin_pay.fields.notifyId')}
                    <button
                      type="button"
                      aria-label={`${t('plugin_pay.copy.action')} ${t('plugin_pay.fields.notifyId')}`}
                      className="inline-flex"
                      onClick={() => void copyText(detail.notifyId ?? '')}
                    >
                      <Copy aria-hidden="true" className="size-3" />
                    </button>
                  </dt>
                  <dd className="num mt-0.5 break-all">{detail.notifyId}</dd>
                </div>
              ) : null}
              <div>
                <dt className="text-xs text-muted-foreground">{t('plugin_pay.fields.createdAt')}</dt>
                <dd className="num mt-0.5">{formatTime(detail.createdAt, i18n.language)}</dd>
              </div>
            </dl>
            <div>
              <div className="flex items-center justify-between gap-2">
                <p className="text-xs text-muted-foreground">{t('plugin_pay.fields.payload')}</p>
                {detail.payload ? (
                  <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={() => void copyText(prettyPayload(detail.payload))}>
                    <Copy aria-hidden="true" />
                    {t('plugin_pay.copy.action')}
                  </Button>
                ) : null}
              </div>
              <pre className="mt-1 max-h-80 overflow-auto rounded-md bg-muted p-2 text-xs">{prettyPayload(detail.payload)}</pre>
            </div>
          </div>
        ) : null}
      </ResponsiveDialog>
    </div>
  )
}

function prettyPayload(payload?: string): string {
  if (!payload) return '-'
  try {
    return JSON.stringify(JSON.parse(payload), null, 2)
  } catch {
    return payload
  }
}
