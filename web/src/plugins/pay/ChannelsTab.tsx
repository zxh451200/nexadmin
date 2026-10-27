import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/framework/ui/dialog'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import { useChannels, useDeleteChannel, useProviders, useSaveChannel, type PayChannel, type PayChannelForm } from './api'
import { formatBp, formatTime } from './format'

interface Props {
  open: boolean
  onOpenChange: (v: boolean) => void
  editing: PayChannel | null
  providers: string[]
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
  const [config, setConfig] = useState('{}')

  const submit = async () => {
    let parsed: Record<string, string> = {}
    try {
      parsed = JSON.parse(config || '{}')
    } catch {
      toast.error(t('plugin_pay.configJsonError'))
      return
    }
    const data: PayChannelForm = {
      code: editing?.code ?? code.trim(),
      name,
      provider: editing?.provider ?? provider,
      feeRateBp,
      enabled,
      sort,
    }
    // 编辑时未改配置就不回传，保留既有密文；新建必须给配置（mock 渠道要 secret）。
    if (!editing || Object.keys(parsed).length > 0) {
      data.config = parsed
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{isNew ? t('plugin_pay.channel.create') : t('plugin_pay.channel.edit')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_pay.fields.channelCode')}
            <Input value={code} disabled={!isNew} onChange={(e) => setCode(e.target.value)} placeholder="wx01" />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_pay.fields.provider')}
            <Select value={provider} onValueChange={setProvider} disabled={!isNew}>
              <SelectTrigger aria-label={t('plugin_pay.fields.provider')}>
                <SelectValue placeholder={t('plugin_pay.fields.provider')} />
              </SelectTrigger>
              <SelectContent>
                {providers.map((p) => (
                  <SelectItem key={p} value={p}>
                    {t(`plugin_pay.providers.${p}`, { defaultValue: p })}
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
          <label className="flex items-center justify-between text-sm">
            {t('plugin_pay.fields.enabled')}
            <Switch checked={enabled} onCheckedChange={setEnabled} />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            {t('plugin_pay.fields.config')}
            <Input
              value={config}
              onChange={(e) => setConfig(e.target.value)}
              className="font-mono"
              placeholder={isNew ? '{"secret":"..."}' : t('plugin_pay.configKeep')}
            />
          </label>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ChannelsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const { data: channels = [], isLoading } = useChannels()
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
      <ChannelFormDialog open={formOpen} onOpenChange={setFormOpen} editing={editing} providers={providers} />
    </div>
  )
}
