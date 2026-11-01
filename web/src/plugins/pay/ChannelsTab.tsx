import { useState } from 'react'
import { Pencil, Plus, Stethoscope, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { JsonInput } from '@/framework/components/JsonInput'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { SwitchRow } from '@/framework/components/SwitchField'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import {
  useChannels,
  useDeleteChannel,
  useProviders,
  useSaveChannel,
  useProbeChannel,
  type PayChannel,
  type PayChannelProbe,
  type PayChannelForm,
  type PayProvider,
  type ProviderCapabilities,
} from './api'
import { CapabilityNote, CapabilitySelect, CapabilitySummary } from './CapabilityControls'
import {
  allMethods,
  capabilitiesOfProvider,
  supportsMethod,
  unsupportedMethod,
  useCapabilityIndex,
} from './capabilities'
import { formatBp, formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

/** 渠道列表里的能力摘要：只列支持的动作，一眼看出哪些入口在该渠道可用。 */
/** 一次自检的结论面板：逐项 pass/fail/skipped + 渠道原文诊断。 */
function ProbeDialog({ code, onClose }: { code: string; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const probe = useProbeChannel()
  const fmt = (v?: string) => (v ? formatTime(v, i18n.language) : '-')

  return (
    <ResponsiveDialog
      open
      onOpenChange={(v) => !v && onClose()}
      title={`${t('plugin_pay.probe.title')} · ${code}`}
      size="md"
      footer={
        <>
          <Button variant="outline" onClick={() => void probe.mutate(code)} disabled={probe.isPending}>
            {probe.isPending ? t('common.loading') : t('plugin_pay.probe.rerun')}
          </Button>
          <Button onClick={onClose}>{t('common.close')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {/* 自检只读：不下单、不退款、不打款，所以可以随便点 */}
        <p className="text-xs text-muted-foreground">{t('plugin_pay.probe.hint')}</p>
        {probe.isError ? <ListErrorBanner onRetry={() => void probe.mutate(code)} /> : null}
        {probe.data ? <ProbeResultView r={probe.data} fmt={fmt} /> : null}
        {!probe.data && !probe.isError && probe.isPending ? (
          <p className="text-xs text-muted-foreground">{t('common.loading')}</p>
        ) : null}
        {!probe.data && !probe.isPending && !probe.isError ? (
          <p className="text-xs text-muted-foreground">{t('plugin_pay.probe.idle')}</p>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}

function ProbeResultView({ r, fmt }: { r: PayChannelProbe; fmt: (v?: string) => string }) {
  const { t } = useTranslation()
  const variant = (v: string) => (v === 'pass' ? 'success' : v === 'fail' ? 'destructive' : 'outline')
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <Badge variant={r.failed > 0 ? 'destructive' : 'success'}>
          {t(r.failed > 0 ? 'plugin_pay.probe.hasFailures' : 'plugin_pay.probe.allPassed', { count: r.failed })}
        </Badge>
        <span>{r.provider}</span>
        <span className="num">{fmt(r.checkedAt)}</span>
      </div>
      <dl className="flex flex-col divide-y divide-border rounded-lg border border-border">
        {r.checks.map((c) => (
          <div key={c.name} className="flex flex-col gap-1 p-2.5">
            <dt className="flex items-center gap-2">
              <Badge variant={variant(c.state)}>{t(`plugin_pay.probeState.${c.state}`)}</Badge>
              <span className="text-sm">{t(`plugin_pay.probeCheck.${c.name}`, { defaultValue: c.name })}</span>
            </dt>
            {/* 诊断原文照 failReason 口径透出：它是渠道给的话，翻译了反而对不上工单 */}
            {c.detail ? <dd className="break-words text-xs text-muted-foreground">{c.detail}</dd> : null}
          </div>
        ))}
      </dl>
      {r.skipped > 0 ? (
        <p className="text-xs text-muted-foreground">{t('plugin_pay.probe.skippedNote', { count: r.skipped })}</p>
      ) : null}
    </div>
  )
}

function CapabilityCell({ caps, provider }: { caps: ProviderCapabilities | null; provider: string }) {
  const { t } = useTranslation()
  if (!caps) {
    return <span className="text-xs text-muted-foreground">{t('plugin_pay.capabilities.unknown', { provider })}</span>
  }
  const on = [
    caps.refundQuery && t('plugin_pay.capabilities.actions.refundQuery'),
    caps.auth && t('plugin_pay.capabilities.actions.auth'),
    caps.bill && t('plugin_pay.capabilities.actions.bill'),
    caps.payout && t('plugin_pay.capabilities.actions.payout'),
  ].filter((v): v is string => !!v)
  if (on.length === 0) {
    return (
      <span className="text-xs text-muted-foreground">
        {t('plugin_pay.capabilities.actionsNone', { provider })}
      </span>
    )
  }
  return (
    <div className="flex flex-wrap gap-1">
      {on.map((label) => (
        <Badge key={label} variant="outline" className="font-normal">
          {label}
        </Badge>
      ))}
    </div>
  )
}

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  editing: PayChannel | null
  providers: PayProvider[]
}

function ChannelFormDialog({ open, onOpenChange, editing, providers }: Props) {
  const { t } = useTranslation()
  const save = useSaveChannel()
  const isNew = !editing
  const [code, setCode] = useState(editing?.code ?? '')
  const [provider, setProvider] = useState(editing?.provider ?? '')
  const [name, setName] = useState(editing?.name ?? '')
  const [feeRateBp, setFeeRateBp] = useState(editing?.feeRateBp ?? 0)
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [sort, setSort] = useState(editing?.sort ?? 0)
  const [method, setMethod] = useState(editing?.method ?? '')
  const [notifyUrl, setNotifyUrl] = useState(editing?.notifyUrl ?? '')
  const [notifyTouched, setNotifyTouched] = useState(false)
  const [configValues, setConfigValues] = useState<Record<string, string>>({})
  const [configJson, setConfigJson] = useState('{}')

  // 能力矩阵按「当前表单选中的渠道类型」取：新建时跟着下拉走，编辑时后端不让改类型
  const index = useCapabilityIndex()
  const activeProvider = editing?.provider ?? provider
  const caps = capabilitiesOfProvider(index, activeProvider)
  const methodOptions = allMethods(index).map((m) => ({ value: m, label: t(`plugin_pay.method.${m}`, { defaultValue: m }) }))

  const schema = providers.find((p) => p.name === (editing?.provider ?? provider))?.fields ?? []
  const fieldLabel = (key: string) => t(`plugin_pay.configFields.${key}`, { defaultValue: key })

  // 新建时按渠道编码带上本站的完整回调地址；真实网关不接受相对路径。
  const onCodeChange = (v: string) => {
    setCode(v)
    if (isNew && !notifyTouched) {
      setNotifyUrl(v.trim() ? `${window.location.origin}/api/v1/plugins/pay/notify/${v.trim()}` : '')
    }
  }

  const submit = async () => {
    const data: PayChannelForm = {
      code: editing?.code ?? code.trim(),
      name,
      provider: editing?.provider ?? provider,
      feeRateBp,
      notifyUrl: notifyUrl.trim(),
      enabled,
      sort,
    }
    if (method.trim()) data.method = method.trim()
    // 能力矩阵已知且该方式不支持时直接拦下：后端同样会拒，别让保存变成一次 400
    if (data.method && !supportsMethod(caps, data.method)) {
      toast.error(unsupportedMethod(t, activeProvider, data.method))
      return
    }
    if (schema.length > 0) {
      const filled = Object.fromEntries(
        Object.entries(configValues)
          .map(([k, v]) => [k, v.trim()])
          .filter(([, v]) => v),
      )
      const missing = schema.filter((f) => f.required && !filled[f.key])
      if (missing.length > 0) {
        toast.error(t('plugin_pay.configRequired', { fields: missing.map((f) => fieldLabel(f.key)).join(', ') }))
        return
      }
      // 编辑时全空＝沿用既有密文；只改部分字段会让其余必填项丢失，必须整表重填。
      if (!editing || Object.keys(filled).length > 0) data.config = filled
    } else {
      let parsed: Record<string, string> = {}
      try {
        parsed = JSON.parse(configJson || '{}')
      } catch {
        toast.error(t('plugin_pay.configJsonError'))
        return
      }
      if (!editing || Object.keys(parsed).length > 0) data.config = parsed
    }
    try {
      await save.mutateAsync(data)
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={isNew ? t('plugin_pay.channel.create') : t('plugin_pay.channel.edit')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_pay.fields.channelCode')}
          <Input value={code} disabled={!isNew} onChange={(e) => onCodeChange(e.target.value)} placeholder="wx01" />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_pay.fields.provider')}
          <Select
            value={provider}
            onValueChange={(v) => {
              setProvider(v)
              setConfigValues({})
              // 换渠道类型后原支付方式可能不再被支持：清空让用户重选，别带着非法值保存
              const next = capabilitiesOfProvider(index, v)
              if (method && !supportsMethod(next, method)) setMethod('')
            }}
            disabled={!isNew}
          >
            <SelectTrigger aria-label={t('plugin_pay.fields.provider')}>
              <SelectValue placeholder={t('plugin_pay.fields.provider')} />
            </SelectTrigger>
            <SelectContent>
              {providers.map((p) => (
                <SelectItem key={p.name} value={p.name}>
                  {t(`plugin_pay.providers.${p.name}`, { defaultValue: p.name })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </label>
        <CapabilitySelect
          label={t('plugin_pay.fields.method')}
          value={method}
          onChange={(v) => setMethod(v)}
          options={methodOptions}
          caps={caps}
          provider={activeProvider}
          kind="method"
          placeholder={t('plugin_pay.channel.methodPlaceholder')}
        />
        <div className="flex flex-col gap-1.5 rounded-lg border border-border p-3">
          <p className="text-xs font-medium text-foreground">{t('plugin_pay.capabilities.title')}</p>
          <CapabilitySummary caps={caps} provider={activeProvider} />
          <CapabilityNote>{t('plugin_pay.capabilities.enforcedByServer')}</CapabilityNote>
        </div>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_pay.fields.channelName')}
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_pay.fields.feeRateBp')}
          <Input
            type="number"
            value={feeRateBp}
            min={0}
            max={9999}
            onChange={(e) => setFeeRateBp(Number(e.target.value) || 0)}
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_pay.fields.sort')}
          <Input type="number" value={sort} onChange={(e) => setSort(Number(e.target.value) || 0)} />
        </label>
        <SwitchRow label={t('plugin_pay.fields.enabled')} checked={enabled} onCheckedChange={setEnabled} />
        <label className="flex flex-col gap-1 text-sm">
          {t('plugin_pay.fields.notifyUrl')}
          <Input
            value={notifyUrl}
            onChange={(e) => {
              setNotifyUrl(e.target.value)
              setNotifyTouched(true)
            }}
            placeholder={t('plugin_pay.notifyUrlHint')}
          />
        </label>
        {schema.length > 0 ? (
          <div className="flex flex-col gap-3">
            {editing && <p className="text-xs text-muted-foreground">{t('plugin_pay.configKeep')}</p>}
            {schema.map((f) => (
              <label key={f.key} className="flex flex-col gap-1 text-sm">
                <span>
                  {fieldLabel(f.key)}
                  {f.required && <span className="text-destructive"> *</span>}
                </span>
                {f.type === 'select' ? (
                  <Select
                    value={configValues[f.key] || undefined}
                    onValueChange={(v) => setConfigValues((s) => ({ ...s, [f.key]: v }))}
                  >
                    <SelectTrigger aria-label={fieldLabel(f.key)}>
                      <SelectValue placeholder={f.placeholder ?? fieldLabel(f.key)} />
                    </SelectTrigger>
                    <SelectContent>
                      {f.options?.map((opt) => (
                        <SelectItem key={opt} value={opt}>
                          {opt}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <Input
                    type={f.secret ? 'password' : 'text'}
                    autoComplete={f.secret ? 'new-password' : 'off'}
                    value={configValues[f.key] ?? ''}
                    onChange={(e) => setConfigValues((s) => ({ ...s, [f.key]: e.target.value }))}
                    placeholder={f.placeholder ?? fieldLabel(f.key)}
                  />
                )}
              </label>
            ))}
          </div>
        ) : (
          <div className="flex flex-col gap-1 text-sm">
            <label htmlFor="pay-channel-config">{t('plugin_pay.fields.config')}</label>
            <JsonInput
              id="pay-channel-config"
              value={configJson}
              onChange={setConfigJson}
              rows={3}
              placeholder={isNew ? '{"secret":"..."}' : t('plugin_pay.configKeep')}
            />
          </div>
        )}
      </div>
    </ResponsiveDialog>
  )
}

export function ChannelsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { data: channels = [], isLoading, isError, refetch } = useChannels()
  const { data: providers = [] } = useProviders()
  const index = useCapabilityIndex()
  const remove = useDeleteChannel()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<PayChannel | null>(null)
  const [probing, setProbing] = useState<string | null>(null)

  const onDelete = async (c: PayChannel) => {
    if (await confirm({ destructive: true, description: t('plugin_pay.channel.deleteConfirm', { name: c.name }) })) {
      try {
        await remove.mutateAsync(c.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const columns: Column<PayChannel>[] = [
    { key: 'code', title: t('plugin_pay.fields.channelCode'), mobile: 'title', render: (c) => <span className="font-medium text-foreground">{c.code}</span> },
    { key: 'name', title: t('plugin_pay.fields.channelName'), mobile: 'sub', render: (c) => c.name },
    { key: 'provider', title: t('plugin_pay.fields.provider'), render: (c) => <Badge variant="outline">{c.provider}</Badge> },
    {
      key: 'capabilities',
      title: t('plugin_pay.capabilities.title'),
      render: (c) => <CapabilityCell provider={c.provider} caps={capabilitiesOfProvider(index, c.provider)} />,
    },
    { key: 'feeRateBp', title: t('plugin_pay.fields.feeRateBp'), render: (c) => (c.feeRateBp > 0 ? <span className="num">{formatBp(c.feeRateBp)}</span> : <span className="text-muted-foreground">-</span>) },
    { key: 'enabled', title: t('plugin_pay.fields.enabled'), render: (c) => (c.enabled ? <Badge variant="success">{t('plugin_pay.enabledOn')}</Badge> : <Badge variant="outline">{t('plugin_pay.enabledOff')}</Badge>) },
    { key: 'hasConfig', title: t('plugin_pay.fields.config'), render: (c) => (c.hasConfig ? t('plugin_pay.configSet') : <span className="text-muted-foreground">-</span>) },
    {
      key: 'updatedAt',
      title: t('plugin_pay.fields.updatedAt'),
      render: (c) => <span className="num text-xs text-muted-foreground">{formatTime(c.updatedAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
      {isError ? <ListErrorBanner onRetry={() => void refetch()} /> : null}
      <DataTable
        columns={columns}
        data={channels}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-pay-channels"
        emptyText={t('plugin_pay.empty')}
        toolbar={
          <Can perm="pay:channel">
            <Button
              onClick={() => {
                setEditing(null)
                setFormOpen(true)
              }}
            >
              <Plus aria-hidden="true" />
              {t('plugin_pay.channel.create')}
            </Button>
          </Can>
        }
        rowActions={(c) => (
          <Can perm="pay:channel">
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('plugin_pay.probe.title')}
              title={t('plugin_pay.probe.title')}
              onClick={() => setProbing(c.code)}
            >
              <Stethoscope aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('common.edit')}
              onClick={() => {
                setEditing(c)
                setFormOpen(true)
              }}
            >
              <Pencil aria-hidden="true" />
            </Button>
            <Button variant="ghost" size="icon" className="text-destructive" aria-label={t('common.delete')} onClick={() => void onDelete(c)}>
              <Trash2 aria-hidden="true" />
            </Button>
          </Can>
        )}
      />
      {probing ? <ProbeDialog code={probing} onClose={() => setProbing(null)} /> : null}
      {/* 每次打开重新挂载：表单状态按当前编辑对象初始化，避免沿用上一次的草稿。 */}
      {formOpen && <ChannelFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} providers={providers} />}
    </div>
  )
}
