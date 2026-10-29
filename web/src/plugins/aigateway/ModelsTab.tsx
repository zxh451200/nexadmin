import { useState } from 'react'
import { Pencil, Plus, Trash2, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { useConfirm } from '@/framework/components/ConfirmDialog'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { FilterBar } from '@/framework/components/FilterBar'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Switch } from '@/framework/ui/switch'
import {
  AI_KINDS,
  useDeleteModel,
  useModels,
  usePingModel,
  useSaveModel,
  type AiKind,
  type AiModel,
  type AiModelForm,
  type ModelQuery,
} from './api'
import { formatTime } from './format'

const ALL = '__all'
const MASK = '********'

function ModelFormDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  editing: AiModel | null
}) {
  const { t } = useTranslation()
  const save = useSaveModel()
  const isNew = !editing
  const [draft, setDraft] = useState<Partial<AiModelForm>>({})

  const value: Partial<AiModelForm> = editing
    ? {
        kind: editing.kind,
        name: editing.name,
        provider: editing.provider,
        baseUrl: editing.baseUrl,
        model: editing.model,
        apiKey: editing.apiKey,
        dimension: editing.dimension,
        enabled: editing.enabled,
        isDefault: editing.isDefault,
      }
    : draft

  const set = (patch: Partial<AiModelForm>) => {
    if (editing) return
    setDraft((d) => ({ ...d, ...patch }))
  }

  const submit = async () => {
    const data = { ...value, name: (value.name ?? '').trim() } as AiModelForm
    if (!data.name) {
      toast.error(t('plugin_aigateway.nameRequired'))
      return
    }
    // 打码值或空串表示保持既有密钥——不回传，后端按空处理。
    if (!isNew && data.apiKey === MASK) {
      data.apiKey = ''
    }
    try {
      await save.mutateAsync({ id: editing?.id, data })
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
      title={isNew ? t('plugin_aigateway.model.create') : t('plugin_aigateway.model.edit')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_aigateway.fields.kind')}
            <Select value={value.kind ?? 'llm'} onValueChange={(v) => set({ kind: v as AiKind })} disabled={!isNew}>
              <SelectTrigger aria-label={t('plugin_aigateway.fields.kind')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {AI_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`plugin_aigateway.kind.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_aigateway.fields.name')}
            <Input value={value.name ?? ''} onChange={(e) => set({ name: e.target.value })} />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1">
            {t('plugin_aigateway.fields.provider')}
            <Input value={value.provider ?? ''} onChange={(e) => set({ provider: e.target.value })} placeholder="openai / qwen / ..." />
          </label>
          <label className="flex flex-col gap-1">
            {t('plugin_aigateway.fields.model')}
            <Input value={value.model ?? ''} onChange={(e) => set({ model: e.target.value })} placeholder="gpt-4o / bge-m3" />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          {t('plugin_aigateway.fields.baseUrl')}
          <Input value={value.baseUrl ?? ''} onChange={(e) => set({ baseUrl: e.target.value })} placeholder="https://api.openai.com/v1" />
        </label>
        <label className="flex flex-col gap-1">
          {t('plugin_aigateway.fields.apiKey')}
          <Input
            value={value.apiKey ?? ''}
            onChange={(e) => set({ apiKey: e.target.value })}
            placeholder={isNew ? 'sk-...' : t('plugin_aigateway.model.keyKeep')}
          />
        </label>
        {value.kind === 'embedding' && (
          <label className="flex flex-col gap-1">
            {t('plugin_aigateway.fields.dimension')}
            <Input
              type="number"
              value={value.dimension ?? 0}
              onChange={(e) => set({ dimension: Number(e.target.value) || 0 })}
            />
          </label>
        )}
        <div className="flex items-center justify-between">
          <label className="flex items-center justify-between gap-2 text-sm">
            {t('plugin_aigateway.fields.enabled')}
            <Switch checked={value.enabled ?? true} onCheckedChange={(v) => set({ enabled: v })} />
          </label>
          <label className="flex items-center justify-between gap-2 text-sm">
            {t('plugin_aigateway.fields.isDefault')}
            <Switch checked={value.isDefault ?? false} onCheckedChange={(v) => set({ isDefault: v })} />
          </label>
        </div>
      </div>
    </ResponsiveDialog>
  )
}

export function ModelsTab() {
  const { t, i18n } = useTranslation()
  const confirm = useConfirm()
  const [draft, setDraft] = useState<ModelQuery>({})
  const [filters, setFilters] = useState<ModelQuery>({})
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<AiModel | null>(null)

  const { data, isLoading } = useModels({ ...filters, page, pageSize })
  const remove = useDeleteModel()
  const ping = usePingModel()

  const onDelete = async (m: AiModel) => {
    if (await confirm({ destructive: true, description: t('plugin_aigateway.model.deleteConfirm', { name: m.name }) })) {
      try {
        await remove.mutateAsync(m.id)
        toast.success(t('common.operateSuccess'))
      } catch {
        toast.error(t('common.operateFailed'))
      }
    }
  }

  const onPing = async (m: AiModel) => {
    try {
      const res = await ping.mutateAsync(m.id)
      if (res.ok) {
        toast.success(t('plugin_aigateway.model.pingOk', { ms: res.latencyMs ?? 0 }))
      } else {
        toast.error(t('plugin_aigateway.model.pingFail', { detail: res.detail ?? '' }))
      }
    } catch {
      toast.error(t('common.operateFailed'))
    }
  }

  const columns: Column<AiModel>[] = [
    {
      key: 'name',
      title: t('plugin_aigateway.fields.name'),
      mobile: 'title',
      render: (m) => (
        <div>
          <span className="font-medium text-foreground">{m.name}</span>
          <div className="text-xs text-muted-foreground">{m.model}</div>
        </div>
      ),
    },
    { key: 'kind', title: t('plugin_aigateway.fields.kind'), mobile: 'sub', render: (m) => <Badge variant="outline">{t(`plugin_aigateway.kind.${m.kind}`)}</Badge> },
    { key: 'provider', title: t('plugin_aigateway.fields.provider'), render: (m) => m.provider || '-' },
    {
      key: 'default',
      title: t('plugin_aigateway.fields.isDefault'),
      render: (m) => (m.isDefault ? <Badge variant="success">{t('plugin_aigateway.model.default')}</Badge> : <span className="text-muted-foreground">-</span>),
    },
    {
      key: 'enabled',
      title: t('plugin_aigateway.fields.enabled'),
      render: (m) => (m.enabled ? <Badge variant="success">{t('plugin_aigateway.enabledOn')}</Badge> : <Badge variant="outline">{t('plugin_aigateway.enabledOff')}</Badge>),
    },
    {
      key: 'updatedAt',
      title: t('plugin_aigateway.fields.updatedAt'),
      render: (m) => <span className="num text-xs text-muted-foreground">{formatTime(m.updatedAt, i18n.language)}</span>,
    },
  ]

  return (
    <div className="flex flex-col gap-4">
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
        <Input
          className="md:w-56"
          value={draft.keyword ?? ''}
          onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
          placeholder={t('plugin_aigateway.searchPlaceholder')}
          aria-label={t('common.keyword')}
        />
        <Select value={draft.kind ?? ALL} onValueChange={(v) => setDraft((d) => ({ ...d, kind: v === ALL ? undefined : v }))}>
          <SelectTrigger className="md:w-44" aria-label={t('plugin_aigateway.fields.kind')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>{t('plugin_aigateway.all')}</SelectItem>
            {AI_KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`plugin_aigateway.kind.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FilterBar>
      <DataTable
        columns={columns}
        data={data?.list ?? []}
        rowKey="id"
        loading={isLoading}
        columnSettingsKey="plugin-aigateway-models"
        pagination={{
          page,
          pageSize,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p)
            setPageSize(s)
          },
        }}
        emptyText={t('plugin_aigateway.empty')}
        toolbar={
          <Can perm="aigateway:edit">
            <Button
              onClick={() => {
                setEditing(null)
                setFormOpen(true)
              }}
            >
              <Plus aria-hidden="true" />
              {t('plugin_aigateway.model.create')}
            </Button>
          </Can>
        }
        rowActions={(m) => (
          <Can perm="aigateway:edit">
            <div className="flex items-center">
              <Button variant="ghost" size="icon" aria-label={t('plugin_aigateway.model.ping')} onClick={() => void onPing(m)}>
                <Zap aria-hidden="true" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('common.edit')}
                onClick={() => {
                  setEditing(m)
                  setFormOpen(true)
                }}
              >
                <Pencil aria-hidden="true" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="text-destructive"
                aria-label={t('common.delete')}
                onClick={() => void onDelete(m)}
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </div>
          </Can>
        )}
      />
      <ModelFormDialog key={editing?.id ?? 'new'} open={formOpen} onOpenChange={setFormOpen} editing={editing} />
    </div>
  )
}
