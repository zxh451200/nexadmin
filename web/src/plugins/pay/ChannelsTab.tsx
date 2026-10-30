import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
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
import { useChannels, useDeleteChannel, useProviders, useSaveChannel, type PayChannel, type PayChannelForm, type PayProvider } from './api'
import { formatBp, formatTime } from './format'
import { ListErrorBanner } from './ListErrorBanner'

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
  const [notifyUrl, setNotifyUrl] = useState(editing?.notifyUrl ?? '')
  const [notifyTouched, setNotifyTouched] = useState(false)
  const [configValues, setConfigValues] = useState<Record<string, string>>({})
  const [configJson, setConfigJson] = useState('{}')

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
  const remove = useDeleteChannel()
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<PayChannel | null>(null)

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
      {/* 每次打开重新挂载：表单状态按当前编辑对象初始化，避免沿用上一次的草稿。 */}
      {formOpen && <ChannelFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} providers={providers} />}
    </div>
  )
}
